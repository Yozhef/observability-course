package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Open connects to PostgreSQL with retries (PG boots slower than the apps).
func Open(dsn string, maxConns int) (*sql.DB, error) {
	var d *sql.DB
	var err error
	for i := 0; i < 30; i++ {
		d, err = sql.Open("pgx", dsn)
		if err == nil {
			if err = d.Ping(); err == nil {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	d.SetMaxOpenConns(maxConns)
	d.SetMaxIdleConns(maxConns)
	return d, nil
}

// WithConn makes connection acquisition VISIBLE in traces: a dedicated
// "db.acquire_connection" span shows pool-wait time (Day 3 pool-exhaustion demo),
// then fn runs inside a "db.query" span while holding the connection.
func WithConn(ctx context.Context, tracer trace.Tracer, d *sql.DB, queryName, statement string,
	fn func(ctx context.Context, conn *sql.Conn) error) error {

	acqCtx, acqSpan := tracer.Start(ctx, "db.acquire_connection")
	conn, err := d.Conn(acqCtx)
	if err != nil {
		acqSpan.RecordError(err)
		acqSpan.SetStatus(codes.Error, err.Error())
		acqSpan.End()
		return err
	}
	acqSpan.End()
	defer conn.Close()

	qCtx, qSpan := tracer.Start(ctx, queryName, trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.statement", statement),
	))
	defer qSpan.End()
	if err := fn(qCtx, conn); err != nil {
		qSpan.RecordError(err)
		qSpan.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
