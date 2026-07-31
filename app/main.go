package main

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const serviceName = "demo-service"

var tracer trace.Tracer

// ---------- Prometheus metrics ----------

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		},
		[]string{"method", "path"},
	)

	httpRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Number of HTTP requests currently being served.",
		},
	)
)

// statusRecorder captures the response status code for metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// metricsMiddleware records RED metrics for every request.
func metricsMiddleware(path string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		httpRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(rec.status)).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
	})
}

// ---------- OpenTelemetry ----------

func initTracer(ctx context.Context) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://tempo:4318"
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"),
	)
	if err != nil {
		return nil, err
	}

	res, err := sdkresource.Merge(
		sdkresource.Default(),
		sdkresource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	tracer = tp.Tracer(serviceName)
	return tp.Shutdown, nil
}

// ---------- Fake "work" to make traces interesting ----------

func fakeDBQuery(ctx context.Context, query string) error {
	_, span := tracer.Start(ctx, "db.query",
		trace.WithAttributes(attribute.String("db.statement", query)),
	)
	defer span.End()

	time.Sleep(time.Duration(5+rand.Intn(40)) * time.Millisecond)

	// ~10% of queries fail to make error-rate panels interesting.
	if rand.Intn(10) == 0 {
		err := errors.New("db: connection timeout")
		span.RecordError(err)
		return err
	}
	return nil
}

func fakeCacheLookup(ctx context.Context, key string) {
	_, span := tracer.Start(ctx, "cache.get",
		trace.WithAttributes(attribute.String("cache.key", key)),
	)
	defer span.End()
	time.Sleep(time.Duration(1 + rand.Intn(5)) * time.Millisecond)
}

// ---------- Handlers ----------

func helloHandler(w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"message": "hello, observability!"}`))
}

func usersHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fakeCacheLookup(ctx, "users:list")
	if err := fakeDBQuery(ctx, "SELECT * FROM users LIMIT 10"); err != nil {
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"users": [{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]}`))
}

func ordersHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := fakeDBQuery(ctx, "SELECT * FROM orders WHERE status = 'pending'"); err != nil {
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}
	// Orders are "slow": extra downstream call.
	if err := fakeDBQuery(ctx, "SELECT * FROM order_items WHERE order_id IN (...)"); err != nil {
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"orders": [{"id": 101, "total": 49.99}, {"id": 102, "total": 15.00}]}`))
}

// route wires up metrics + tracing middleware for one endpoint.
func route(mux *http.ServeMux, path string, h http.HandlerFunc) {
	handler := otelhttp.WithRouteTag(path, h)
	mux.Handle(path, metricsMiddleware(path, handler))
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, err := initTracer(ctx)
	if err != nil {
		log.Fatalf("failed to init tracer: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	mux := http.NewServeMux()
	route(mux, "/api/hello", helloHandler)
	route(mux, "/api/users", usersHandler)
	route(mux, "/api/orders", ordersHandler)

	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// otelhttp wraps the whole mux: creates a server span per request
	// and propagates context (W3C traceparent) from incoming headers.
	root := otelhttp.NewHandler(mux, "http.server")

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Println("demo-service listening on :8080")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
