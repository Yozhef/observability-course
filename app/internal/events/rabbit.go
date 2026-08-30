package events

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"obscourse/internal/obs"
)

const Queue = "orders"

// amqpCarrier adapts amqp headers to the OTel propagation API.
type amqpCarrier amqp.Table

func (c amqpCarrier) Get(key string) string {
	if v, ok := c[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
func (c amqpCarrier) Set(key, value string) { c[key] = value }
func (c amqpCarrier) Keys() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

type Bus struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

// Connect retries until RabbitMQ is up (it boots slower than the apps).
func Connect(url string) (*Bus, error) {
	var conn *amqp.Connection
	var err error
	for i := 0; i < 30; i++ {
		conn, err = amqp.Dial(url)
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("rabbitmq connect: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	if _, err := ch.QueueDeclare(Queue, true, false, false, false, nil); err != nil {
		return nil, err
	}
	return &Bus{conn: conn, ch: ch}, nil
}

func (b *Bus) Close() {
	if b.ch != nil {
		_ = b.ch.Close()
	}
	if b.conn != nil {
		_ = b.conn.Close()
	}
}

// Publish sends a message with the trace context injected into headers
// (producer span, W3C traceparent travels inside amqp.Table).
func (b *Bus) Publish(ctx context.Context, tracer trace.Tracer, body []byte, correlationID string) error {
	ctx, span := tracer.Start(ctx, "orders publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination.name", Queue),
		))
	defer span.End()

	headers := amqp.Table{}
	otel.GetTextMapPropagator().Inject(ctx, amqpCarrier(headers))
	headers["x-correlation-id"] = correlationID

	return b.ch.PublishWithContext(ctx, "", Queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		Headers:      headers,
		Timestamp:    time.Now(),
		DeliveryMode: amqp.Persistent,
	})
}

// Consume runs handler for every message with an extracted consumer span and
// records the message age (publish -> pickup) for the consumer-lag demo.
func (b *Bus) Consume(tracer trace.Tracer, handler func(ctx context.Context, body []byte, correlationID string) error) error {
	msgs, err := b.ch.Consume(Queue, "entitlement-worker", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for d := range msgs {
		ctx := otel.GetTextMapPropagator().Extract(context.Background(), amqpCarrier(d.Headers))

		age := 0.0
		if !d.Timestamp.IsZero() {
			age = time.Since(d.Timestamp).Seconds()
			obs.EventAge.Observe(age)
		}

		ctx, span := tracer.Start(ctx, "orders process",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(
				attribute.String("messaging.system", "rabbitmq"),
				attribute.String("messaging.destination.name", Queue),
				attribute.Float64("messaging.message.age_seconds", age),
			))

		corr, _ := d.Headers["x-correlation-id"].(string)
		if err := handler(ctx, d.Body, corr); err != nil {
			span.RecordError(err)
			_ = d.Nack(false, false) // no requeue -> DLQ-style drop for the demo
		} else {
			_ = d.Ack(false)
		}
		span.End()
	}
	return nil
}
