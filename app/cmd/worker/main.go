package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"

	dbx "obscourse/internal/db"
	"obscourse/internal/events"
	"obscourse/internal/obs"
)

const service = "entitlement-worker"

var (
	version   = getenv("VERSION", "v1.0.0")
	pgDSN     = getenv("POSTGRES_DSN", "postgres://course:course@postgres:5432/course?sslmode=disable")
	rabbitURL = getenv("RABBITMQ_URL", "amqp://course:course@rabbitmq:5672/")
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
		if err := sentry.Init(sentry.ClientOptions{Dsn: dsn, Release: version, Environment: "production"}); err == nil {
			defer sentry.Flush(2 * time.Second)
		}
	}

	database, err := dbx.Open(pgDSN, 10)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}

	bus, err := events.Connect(rabbitURL)
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}
	defer bus.Close()

	// Small HTTP server for /metrics and /healthz.
	mux := http.NewServeMux()
	mux.Handle("/metrics", obs.MetricsHandler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	srv := &http.Server{Addr: ":8083", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("metrics server: %v", err)
		}
	}()

	logger.Info("entitlement-worker consuming", "queue", events.Queue, "release", version)

	go func() {
		err := bus.Consume(tracer, func(mctx context.Context, body []byte, correlationID string) error {
			mctx = obs.WithLogger(mctx, logger)
			if correlationID != "" {
				mctx = obs.WithCorrelation(mctx, correlationID)
			}
			var ev struct {
				Type    string `json:"type"`
				OrderID int64  `json:"order_id"`
			}
			if err := json.Unmarshal(body, &ev); err != nil {
				return err
			}

			// Simulated entitlement work.
			time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)

			err := dbx.WithConn(mctx, tracer, database, "db.insert_entitlement",
				"INSERT INTO entitlements(order_id) VALUES($1)",
				func(qctx context.Context, conn *sql.Conn) error {
					_, err := conn.ExecContext(qctx, "INSERT INTO entitlements(order_id) VALUES($1)", ev.OrderID)
					return err
				})
			if err != nil {
				obs.L(mctx).Error("entitlement failed", "order_id", ev.OrderID, "error", err.Error())
				sentry.CaptureException(err)
				return err
			}
			obs.EntitlementGranted.Inc()
			obs.Business(mctx, "EntitlementGranted", "order_id", ev.OrderID)
			return nil
		})
		if err != nil {
			log.Fatalf("consume: %v", err)
		}
	}()

	<-ctx.Done()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(c)
}
