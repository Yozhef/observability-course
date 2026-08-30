package obs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

type ctxKey int

const loggerKey ctxKey = 1

// NewLogger builds the root logger. LOG_FORMAT=text switches to the "bad old days"
// unstructured format used in the Day 1 grep demo; default is structured JSON.
func NewLogger(service, version string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
		// slog за замовчуванням пише "ERROR"/"INFO" — нормалізуємо в lowercase,
		// щоб LogQL-запити курсу (level="error") працювали як на слайдах.
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				if lv, ok := a.Value.Any().(slog.Level); ok {
					a.Value = slog.StringValue(strings.ToLower(lv.String()))
				}
			}
			return a
		},
	}
	var h slog.Handler
	if os.Getenv("LOG_FORMAT") == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h).With(
		"service", service,
		"environment", "production",
		"release", version,
	)
}

// L returns the request-scoped logger (or the default one).
func L(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// WithLogger stores a logger in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// WithCorrelation enriches the request logger with a business correlation id.
func WithCorrelation(ctx context.Context, correlationID string) context.Context {
	return WithLogger(ctx, L(ctx).With("correlation_id", correlationID))
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// LoggingMiddleware enriches every request with request_id and trace_id.
// Must be mounted INSIDE otelhttp so the span context is already present.
func LoggingMiddleware(base *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = newID()
		}
		l := base.With("request_id", reqID)
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			l = l.With("trace_id", sc.TraceID().String())
		}
		if corr := r.Header.Get("X-Correlation-ID"); corr != "" {
			l = l.With("correlation_id", corr)
		}
		w.Header().Set("X-Request-ID", reqID)
		next.ServeHTTP(w, r.WithContext(WithLogger(r.Context(), l)))
	})
}

// Business logs a business event (OrderCreated, PaymentFailed, ...) at info level.
func Business(ctx context.Context, event string, args ...any) {
	L(ctx).Info("business_event", append([]any{"event", event}, args...)...)
}
