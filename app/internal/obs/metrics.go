package obs

import (
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"method", "path"})

	httpRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Number of HTTP requests currently being served.",
	})

	// Product / funnel metrics (Day 2).
	CheckoutStarted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "checkout_started_total", Help: "Checkouts started."})
	PaymentSucceeded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "payment_succeeded_total", Help: "Payments succeeded."})
	PaymentFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "payment_failed_total", Help: "Payments failed."}, []string{"kind"}) // expected|system
	EntitlementGranted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "entitlement_granted_total", Help: "Entitlements granted."})

	// Event age (Day 3, consumer lag).
	EventAge = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "event_age_seconds",
		Help:    "Age of a message when the consumer picked it up (publish -> process).",
		Buckets: []float64{0.05, 0.25, 1, 5, 15, 60, 300, 900},
	})

	// Deliberately bad metric for the cardinality antidemo (Day 2).
	cardinalityBomb = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "requests_by_user_total",
		Help: "ANTIPATTERN: user_id used as a label. Course demo only.",
	}, []string{"user_id"})
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// MetricsMiddleware records RED metrics with trace exemplars.
func MetricsMiddleware(path string, ch *Chaos, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		dur := time.Since(start).Seconds()
		status := strconv.Itoa(rec.status)

		var exLabels prometheus.Labels
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() && sc.IsSampled() {
			exLabels = prometheus.Labels{"trace_id": sc.TraceID().String()}
		}

		ctr := httpRequestsTotal.WithLabelValues(r.Method, path, status)
		if ea, ok := ctr.(prometheus.ExemplarAdder); ok && exLabels != nil {
			ea.AddWithExemplar(1, exLabels)
		} else {
			ctr.Inc()
		}

		obs := httpRequestDuration.WithLabelValues(r.Method, path)
		if eo, ok := obs.(prometheus.ExemplarObserver); ok && exLabels != nil {
			eo.ObserveWithExemplar(dur, exLabels)
		} else {
			obs.Observe(dur)
		}

		if ch != nil && ch.Snapshot().Cardinality {
			cardinalityBomb.WithLabelValues(fmt.Sprintf("user-%d", rand.Intn(100000))).Inc()
		}
	})
}

// MetricsHandler serves /metrics with OpenMetrics enabled (required for exemplars).
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}
