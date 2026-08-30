package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	dbx "obscourse/internal/db"
	"obscourse/internal/events"
	"obscourse/internal/httpx"
	"obscourse/internal/obs"
)

const service = "checkout"

var (
	version    = getenv("VERSION", "v1.0.0")
	regression = os.Getenv("REGRESSION") == "on"
	paymentURL = getenv("PAYMENT_URL", "http://payment:8081")
	pgDSN      = getenv("POSTGRES_DSN", "postgres://course:course@postgres:5432/course?sslmode=disable")
	rabbitURL  = getenv("RABBITMQ_URL", "amqp://course:course@rabbitmq:5672/")
)

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := obs.NewLogger(service, version)

	tracer, shutdown, err := obs.InitTracer(ctx, service, version)
	if err != nil {
		log.Fatalf("tracer: %v", err)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(c)
	}()

	if dsn := os.Getenv("GLITCHTIP_DSN"); dsn != "" {
		if err := sentry.Init(sentry.ClientOptions{
			Dsn: dsn, Release: version, Environment: "production",
		}); err != nil {
			logger.Warn("sentry init failed", "error", err.Error())
		} else {
			defer sentry.Flush(2 * time.Second)
			logger.Info("error tracking enabled", "release", version)
		}
	}

	chaos := obs.NewChaos(obs.Settings{PoolMax: 20})
	database, err := dbx.Open(pgDSN, 20)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	chaos.OnPoolMax(func(n int) {
		if n <= 0 {
			n = 20
		}
		database.SetMaxOpenConns(n)
		database.SetMaxIdleConns(n)
		logger.Warn("db pool resized", "pool_max", n)
	})
	seedOrders(ctx, tracer, database)

	bus, err := events.Connect(rabbitURL)
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	defer bus.Close()

	a := &appCtx{tracer: tracer, db: database, bus: bus, client: httpx.New(chaos), chaos: chaos}

	mux := http.NewServeMux()
	route := func(path string, h http.HandlerFunc) {
		mux.Handle(path, obs.MetricsMiddleware(path, chaos, otelhttp.WithRouteTag(path, h)))
	}
	route("/api/hello", a.hello)
	route("/api/checkout", a.checkout)
	route("/api/orders", a.orders)
	route("/api/aggregate", a.aggregate)

	mux.Handle("/metrics", obs.MetricsHandler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/admin/chaos", chaos.Handler())
	mux.HandleFunc("/admin/reset", chaos.ResetHandler())

	root := otelhttp.NewHandler(obs.LoggingMiddleware(logger, mux), "http.server")
	srv := &http.Server{Addr: ":8080", Handler: root, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		logger.Info("checkout listening", "addr", ":8080", "release", version, "regression", regression)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()
	<-ctx.Done()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(c)
}

type appCtx struct {
	tracer trace.Tracer
	db     *sql.DB
	bus    *events.Bus
	client *httpx.Client
	chaos  *obs.Chaos
}

// ---- helpers ----

func (a *appCtx) injectFaults(w http.ResponseWriter, r *http.Request) bool {
	s := a.chaos.Snapshot()
	if s.DelayMs > 0 {
		time.Sleep(time.Duration(s.DelayMs) * time.Millisecond)
	}
	if s.ErrorRate > 0 && rand.Float64() < s.ErrorRate {
		err := errors.New("internal error injected by chaos")
		obs.L(r.Context()).Error("request failed", "error", err.Error(), "path", r.URL.Path)
		sentry.CaptureException(err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ---- handlers ----

func (a *appCtx) hello(w http.ResponseWriter, r *http.Request) {
	if a.injectFaults(w, r) {
		return
	}
	time.Sleep(time.Duration(rand.Intn(40)) * time.Millisecond)
	writeJSON(w, 200, map[string]string{"message": "hello, observability!"})
}

func (a *appCtx) checkout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	obs.CheckoutStarted.Inc()
	if a.injectFaults(w, r) {
		return
	}

	// 1. Create the order.
	var orderID int64
	amount := 10 + rand.Float64()*90
	err := dbx.WithConn(ctx, a.tracer, a.db, "db.insert_order",
		"INSERT INTO orders(status,total) VALUES('pending',$1) RETURNING id",
		func(qctx context.Context, conn *sql.Conn) error {
			if err := conn.QueryRowContext(qctx,
				"INSERT INTO orders(status,total) VALUES('pending',$1) RETURNING id", amount).Scan(&orderID); err != nil {
				return err
			}
			for i := 1; i <= 3; i++ {
				if _, err := conn.ExecContext(qctx,
					"INSERT INTO order_items(order_id,name,price) VALUES($1,$2,$3)",
					orderID, fmt.Sprintf("item-%d", i), amount/3); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		obs.L(ctx).Error("order create failed", "error", err.Error())
		sentry.CaptureException(err)
		http.Error(w, `{"error":"internal server error"}`, 500)
		return
	}

	correlationID := fmt.Sprintf("checkout-%d", orderID)
	ctx = obs.WithCorrelation(ctx, correlationID)
	r = r.WithContext(ctx)
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("order.id", fmt.Sprint(orderID)),
		attribute.String("correlation.id", correlationID),
	)
	obs.Business(ctx, "OrderCreated", "order_id", orderID, "total", amount)

	// 2. Charge payment (HTTP, propagated context).
	body, _ := json.Marshal(map[string]any{"order_id": orderID, "amount": amount})
	resp, err := a.client.Post(r, paymentURL+"/pay", body, map[string]string{
		"X-Correlation-ID": correlationID,
	})
	if err != nil {
		obs.PaymentFailed.WithLabelValues("system").Inc()
		obs.Business(ctx, "PaymentFailed", "order_id", orderID, "kind", "system", "error", err.Error())
		obs.Business(ctx, "CompensationScheduled", "order_id", orderID)
		obs.L(ctx).Error("payment call failed", "error", err.Error())
		sentry.CaptureException(fmt.Errorf("payment call failed for order %d: %w", orderID, err))
		a.setOrderStatus(ctx, orderID, "failed")
		http.Error(w, `{"error":"payment unavailable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusPaymentRequired: // expected business outcome
		obs.PaymentFailed.WithLabelValues("expected").Inc()
		obs.Business(ctx, "PaymentDeclined", "order_id", orderID, "reason", "card_declined")
		a.setOrderStatus(ctx, orderID, "declined")
		writeJSON(w, 200, map[string]any{"order_id": orderID, "status": "declined"})
		return
	case resp.StatusCode >= 500:
		obs.PaymentFailed.WithLabelValues("system").Inc()
		obs.Business(ctx, "PaymentFailed", "order_id", orderID, "kind", "system", "status", resp.StatusCode)
		obs.Business(ctx, "CompensationScheduled", "order_id", orderID)
		obs.L(ctx).Error("payment provider error", "status", resp.StatusCode, "body", string(respBody))
		sentry.CaptureException(fmt.Errorf("payment provider returned %d for order %d", resp.StatusCode, orderID))
		a.setOrderStatus(ctx, orderID, "failed")
		http.Error(w, `{"error":"payment failed"}`, http.StatusBadGateway)
		return
	}

	obs.PaymentSucceeded.Inc()
	obs.Business(ctx, "PaymentSucceeded", "order_id", orderID)
	a.setOrderStatus(ctx, orderID, "paid")

	// 3. Publish async event for entitlement worker.
	event, _ := json.Marshal(map[string]any{"type": "order.completed", "order_id": orderID})
	if err := a.bus.Publish(ctx, a.tracer, event, correlationID); err != nil {
		obs.L(ctx).Error("event publish failed", "error", err.Error())
		sentry.CaptureException(err)
	} else {
		obs.Business(ctx, "OrderCompletedPublished", "order_id", orderID)
	}

	writeJSON(w, 200, map[string]any{"order_id": orderID, "status": "paid"})
}

func (a *appCtx) setOrderStatus(ctx context.Context, orderID int64, status string) {
	_ = dbx.WithConn(ctx, a.tracer, a.db, "db.update_order",
		"UPDATE orders SET status=$1 WHERE id=$2",
		func(qctx context.Context, conn *sql.Conn) error {
			_, err := conn.ExecContext(qctx, "UPDATE orders SET status=$1 WHERE id=$2", status, orderID)
			return err
		})
}

type orderRow struct {
	ID     int64    `json:"id"`
	Status string   `json:"status"`
	Total  float64  `json:"total"`
	Items  []string `json:"items"`
}

func (a *appCtx) orders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if a.injectFaults(w, r) {
		return
	}
	if regression { // "bad release" behaviour for deploy-bad demo
		time.Sleep(1500 * time.Millisecond)
		if rand.Float64() < 0.15 {
			err := errors.New("regression v2.1.0: orders backend timeout")
			obs.L(ctx).Error("request failed", "error", err.Error())
			sentry.CaptureException(err)
			http.Error(w, `{"error":"timeout"}`, 500)
			return
		}
	}

	snap := a.chaos.Snapshot()
	nPlusOne := r.URL.Query().Get("n_plus_one") == "true"
	var out []orderRow

	if nPlusOne {
		// ANTIPATTERN on purpose: 1 query for orders + 1 query per order.
		err := dbx.WithConn(ctx, a.tracer, a.db, "db.select_orders",
			"SELECT id,status,total FROM orders ORDER BY id DESC LIMIT 100",
			func(qctx context.Context, conn *sql.Conn) error {
				rows, err := conn.QueryContext(qctx, "SELECT id,status,total FROM orders ORDER BY id DESC LIMIT 100")
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var o orderRow
					if err := rows.Scan(&o.ID, &o.Status, &o.Total); err != nil {
						return err
					}
					out = append(out, o)
				}
				return rows.Err()
			})
		if err != nil {
			http.Error(w, `{"error":"db"}`, 500)
			return
		}
		for i := range out {
			_ = dbx.WithConn(ctx, a.tracer, a.db, "db.select_items",
				"SELECT name FROM order_items WHERE order_id=$1",
				func(qctx context.Context, conn *sql.Conn) error {
					rows, err := conn.QueryContext(qctx, "SELECT name FROM order_items WHERE order_id=$1", out[i].ID)
					if err != nil {
						return err
					}
					defer rows.Close()
					for rows.Next() {
						var n string
						_ = rows.Scan(&n)
						out[i].Items = append(out[i].Items, n)
					}
					return rows.Err()
				})
		}
	} else {
		err := dbx.WithConn(ctx, a.tracer, a.db, "db.select_orders_join",
			"SELECT o.id,o.status,o.total,i.name FROM orders o LEFT JOIN order_items i ON i.order_id=o.id ORDER BY o.id DESC LIMIT 300",
			func(qctx context.Context, conn *sql.Conn) error {
				if snap.QueryDelayMs > 0 { // slow query while HOLDING the connection (pool demo)
					time.Sleep(time.Duration(snap.QueryDelayMs) * time.Millisecond)
				}
				rows, err := conn.QueryContext(qctx,
					"SELECT o.id,o.status,o.total,i.name FROM orders o LEFT JOIN order_items i ON i.order_id=o.id ORDER BY o.id DESC LIMIT 300")
				if err != nil {
					return err
				}
				defer rows.Close()
				byID := map[int64]*orderRow{}
				for rows.Next() {
					var id int64
					var status string
					var total float64
					var name sql.NullString
					if err := rows.Scan(&id, &status, &total, &name); err != nil {
						return err
					}
					o, ok := byID[id]
					if !ok {
						o = &orderRow{ID: id, Status: status, Total: total}
						byID[id] = o
						out = append(out, orderRow{})
					}
					if name.Valid {
						o.Items = append(o.Items, name.String)
					}
				}
				out = out[:0]
				for _, o := range byID {
					out = append(out, *o)
				}
				return rows.Err()
			})
		if err != nil {
			obs.L(ctx).Error("orders query failed", "error", err.Error())
			http.Error(w, `{"error":"db"}`, 500)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"orders": out, "count": len(out)})
}

// aggregate demonstrates sequential vs parallel sub-calls (Day 3 critical path demo).
func (a *appCtx) aggregate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if a.injectFaults(w, r) {
		return
	}
	steps := []struct {
		name string
		ms   int
	}{{"enrich.inventory", 300}, {"enrich.pricing", 500}, {"enrich.fraud_check", 700}}

	work := func(name string, ms int) {
		_, span := a.tracer.Start(ctx, name)
		time.Sleep(time.Duration(ms) * time.Millisecond)
		span.End()
	}

	if a.chaos.Snapshot().Parallel {
		var wg sync.WaitGroup
		for _, s := range steps {
			wg.Add(1)
			go func(name string, ms int) { defer wg.Done(); work(name, ms) }(s.name, s.ms)
		}
		wg.Wait()
	} else {
		for _, s := range steps {
			work(s.name, s.ms)
		}
	}
	writeJSON(w, 200, map[string]any{"status": "aggregated", "parallel": a.chaos.Snapshot().Parallel})
}

func seedOrders(ctx context.Context, tracer trace.Tracer, d *sql.DB) {
	var n int
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM orders").Scan(&n); err != nil || n > 0 {
		return
	}
	for i := 0; i < 30; i++ {
		var id int64
		if err := d.QueryRowContext(ctx,
			"INSERT INTO orders(status,total) VALUES('paid',$1) RETURNING id", 10+rand.Float64()*90).Scan(&id); err != nil {
			return
		}
		for j := 1; j <= 3; j++ {
			_, _ = d.ExecContext(ctx, "INSERT INTO order_items(order_id,name,price) VALUES($1,$2,$3)",
				id, fmt.Sprintf("item-%d", j), 5.0)
		}
	}
}
