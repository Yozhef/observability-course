package main

import (
	"context"
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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"obscourse/internal/obs"
)

const service = "payment"

var version = getenv("VERSION", "v1.0.0")

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
		if err := sentry.Init(sentry.ClientOptions{Dsn: dsn, Release: version, Environment: "production"}); err != nil {
			logger.Warn("sentry init failed", "error", err.Error())
		} else {
			defer sentry.Flush(2 * time.Second)
		}
	}

	chaos := obs.NewChaos(obs.Settings{})

	pay := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req struct {
			OrderID int64   `json:"order_id"`
			Amount  float64 `json:"amount"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		trace.SpanFromContext(ctx).SetAttributes(attribute.Int64("order.id", req.OrderID))

		snap := chaos.Snapshot()
		if snap.DelayMs > 0 {
			time.Sleep(time.Duration(snap.DelayMs) * time.Millisecond)
		}
		if snap.ErrorRate > 0 && rand.Float64() < snap.ErrorRate {
			err := errors.New("payment internal error injected by chaos")
			obs.L(ctx).Error("payment failed", "error", err.Error(), "order_id", req.OrderID)
			sentry.CaptureException(err)
			http.Error(w, `{"error":"internal"}`, 500)
			return
		}

		// Call to the (stubbed) external payment provider — its own span.
		provCtx, span := tracer.Start(ctx, "provider.charge", trace.WithAttributes(
			attribute.String("peer.service", "stripe-stub"),
			attribute.Float64("payment.amount", req.Amount),
		))
		_ = provCtx

		delay := 20 + rand.Intn(80) + snap.ProviderDelayMs
		time.Sleep(time.Duration(delay) * time.Millisecond)

		if snap.ProviderErrorRate > 0 && rand.Float64() < snap.ProviderErrorRate {
			err := errors.New("provider: 503 service unavailable")
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			obs.L(ctx).Error("provider error", "provider", "stripe-stub", "status", 503,
				"order_id", req.OrderID, "retryable", true)
			sentry.CaptureException(err)
			http.Error(w, `{"error":"provider unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		span.End()

		// ~7% expected business declines: NOT a system error (Day 1 slide 19).
		if rand.Float64() < 0.07 {
			obs.Business(ctx, "PaymentDeclined", "order_id", req.OrderID, "reason", "card_declined")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"status":"declined","code":"card_declined"}`))
			return
		}

		obs.Business(ctx, "PaymentCharged", "order_id", req.OrderID, "amount", req.Amount, "provider", "stripe-stub")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"charged"}`))
	}

	mux := http.NewServeMux()
	mux.Handle("/pay", obs.MetricsMiddleware("/pay", chaos, otelhttp.WithRouteTag("/pay", http.HandlerFunc(pay))))
	mux.Handle("/metrics", obs.MetricsHandler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/admin/chaos", chaos.Handler())
	mux.HandleFunc("/admin/reset", chaos.ResetHandler())

	root := otelhttp.NewHandler(obs.LoggingMiddleware(logger, mux), "http.server")
	srv := &http.Server{Addr: ":8081", Handler: root, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		logger.Info("payment listening", "addr", ":8081", "release", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()
	<-ctx.Done()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(c)
}
