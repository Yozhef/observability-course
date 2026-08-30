package obs

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
)

// Settings holds runtime fault-injection switches. Every course incident is a
// combination of these flags, toggled through /admin/chaos without restarts.
type Settings struct {
	ErrorRate         float64 `json:"error_rate"`          // 0..1: share of requests failing with 500
	DelayMs           int     `json:"delay_ms"`            // artificial handler delay
	QueryDelayMs      int     `json:"query_delay_ms"`      // artificial DB "query" time while holding a connection
	PoolMax           int     `json:"pool_max"`            // DB pool size (0 = default)
	ProviderDelayMs   int     `json:"provider_delay_ms"`   // payment provider stub latency
	ProviderErrorRate float64 `json:"provider_error_rate"` // payment provider stub 503 rate
	RetryStorm        bool    `json:"retry_storm"`         // retry payment calls aggressively
	Parallel          bool    `json:"parallel"`            // /api/aggregate runs sub-calls in parallel
	BreakPropagation  bool    `json:"break_propagation"`   // drop traceparent on outgoing HTTP
	Cardinality       bool    `json:"cardinality"`         // emit user_id-labelled metric (antidemo)
	CPUBurn           bool    `json:"cpu_burn"`            // burn CPU in background
	MemMB             int     `json:"mem_mb"`              // hold N MB of memory
}

type Chaos struct {
	mu         sync.RWMutex
	s          Settings
	defaults   Settings
	onPoolMax  func(int)
	cpuCancel  context.CancelFunc
	memBallast [][]byte
}

func NewChaos(defaults Settings) *Chaos {
	return &Chaos{s: defaults, defaults: defaults}
}

// OnPoolMax registers a callback fired when pool_max changes (checkout wires
// this to db.SetMaxOpenConns).
func (c *Chaos) OnPoolMax(f func(int)) { c.mu.Lock(); c.onPoolMax = f; c.mu.Unlock() }

// Snapshot returns a copy safe for reading.
func (c *Chaos) Snapshot() Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.s
}

func (c *Chaos) apply(q map[string][]string) {
	c.mu.Lock()
	getF := func(k string, dst *float64) {
		if v, ok := q[k]; ok && len(v) > 0 {
			if f, err := strconv.ParseFloat(v[0], 64); err == nil {
				*dst = f
			}
		}
	}
	getI := func(k string, dst *int) {
		if v, ok := q[k]; ok && len(v) > 0 {
			if i, err := strconv.Atoi(v[0]); err == nil {
				*dst = i
			}
		}
	}
	getB := func(k string, dst *bool) {
		if v, ok := q[k]; ok && len(v) > 0 {
			*dst = v[0] == "on" || v[0] == "true" || v[0] == "1"
		}
	}
	getF("error_rate", &c.s.ErrorRate)
	getI("delay_ms", &c.s.DelayMs)
	getI("query_delay_ms", &c.s.QueryDelayMs)
	getF("provider_error_rate", &c.s.ProviderErrorRate)
	getI("provider_delay_ms", &c.s.ProviderDelayMs)
	getB("retry_storm", &c.s.RetryStorm)
	getB("parallel", &c.s.Parallel)
	getB("break_propagation", &c.s.BreakPropagation)
	getB("cardinality", &c.s.Cardinality)
	getB("cpu_burn", &c.s.CPUBurn)
	getI("mem_mb", &c.s.MemMB)

	oldPool := c.s.PoolMax
	getI("pool_max", &c.s.PoolMax)
	poolChanged := c.s.PoolMax != oldPool
	newPool := c.s.PoolMax
	cb := c.onPoolMax
	c.applySideEffectsLocked()
	c.mu.Unlock()

	if poolChanged && cb != nil {
		cb(newPool)
	}
}

func (c *Chaos) applySideEffectsLocked() {
	// CPU burn
	if c.s.CPUBurn && c.cpuCancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		c.cpuCancel = cancel
		for i := 0; i < 4; i++ {
			go func() {
				x := 0
				for {
					select {
					case <-ctx.Done():
						return
					default:
						x++
						if x == 1<<24 {
							x = 0
						}
					}
				}
			}()
		}
	}
	if !c.s.CPUBurn && c.cpuCancel != nil {
		c.cpuCancel()
		c.cpuCancel = nil
	}
	// Memory ballast
	c.memBallast = nil
	for i := 0; i < c.s.MemMB; i++ {
		b := make([]byte, 1<<20)
		for j := 0; j < len(b); j += 4096 {
			b[j] = 1
		}
		c.memBallast = append(c.memBallast, b)
	}
}

// Handler exposes GET/POST /admin/chaos.
func (c *Chaos) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Query()) > 0 {
			c.apply(r.URL.Query())
			L(r.Context()).Warn("chaos_updated", "params", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(c.Snapshot())
	}
}

// ResetHandler restores defaults.
func (c *Chaos) ResetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		oldPool := c.s.PoolMax
		c.s = c.defaults
		poolChanged := c.s.PoolMax != oldPool
		newPool := c.s.PoolMax
		cb := c.onPoolMax
		c.applySideEffectsLocked()
		c.mu.Unlock()
		if poolChanged && cb != nil {
			cb(newPool)
		}
		L(r.Context()).Info("chaos_reset")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"reset"}`))
	}
}
