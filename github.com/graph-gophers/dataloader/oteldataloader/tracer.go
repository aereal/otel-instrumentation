package oteldataloader

import (
	"context"
	"fmt"

	"github.com/graph-gophers/dataloader/v7"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/aereal/otel-instrumentation/github.com/graph-gophers/dataloader/oteldataloader"

type config struct {
	tp   trace.TracerProvider
	name string
}

type Option func(*config)

// WithTracerProvider creates an new Option that indicates the tracer to use given tracer provider.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *config) {
		c.tp = tp
	}
}

// WithName creates an new Option that indicates data loader's name.
func WithName(name string) Option {
	return func(c *config) {
		c.name = name
	}
}

// WithTracer creates an new dataloader.Option that instruments OpenTelemetry traces.
func WithTracer[K comparable, V any](opts ...Option) dataloader.Option[K, V] {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.tp == nil {
		cfg.tp = otel.GetTracerProvider()
	}

	ot := &otelTracer[K, V]{
		name:   cfg.name,
		tracer: cfg.tp.Tracer(tracerName),
	}
	return dataloader.WithTracer[K, V](ot)
}

type otelTracer[K comparable, V any] struct {
	tracer trace.Tracer
	name   string
}

func (t *otelTracer[K, V]) TraceLoad(ctx context.Context, key K) (context.Context, dataloader.TraceLoadFinishFunc[V]) {
	ctx, span := t.tracer.Start(ctx, fmt.Sprintf("dataloader.Load %s", t.name), trace.WithAttributes(attrDataloaderKey(key)))
	return ctx, func(_ dataloader.Thunk[V]) {
		span.End()
	}
}

func (t *otelTracer[K, V]) TraceLoadMany(ctx context.Context, keys []K) (context.Context, dataloader.TraceLoadManyFinishFunc[V]) {
	ctx, span := t.tracer.Start(ctx, fmt.Sprintf("dataloader.LoadMany %s", t.name), trace.WithAttributes(attrDataloaderKeys(keys), attrKeyDataloaderKeysCount.Int(len(keys))))
	return ctx, func(_ dataloader.ThunkMany[V]) {
		span.End()
	}
}

func (t *otelTracer[K, V]) TraceBatch(ctx context.Context, keys []K) (context.Context, dataloader.TraceBatchFinishFunc[V]) {
	ctx, span := t.tracer.Start(ctx, fmt.Sprintf("dataloader.Batch %s", t.name), trace.WithAttributes(attrDataloaderKeys(keys), attrKeyDataloaderKeysCount.Int(len(keys))))
	return ctx, func(results []*dataloader.Result[V]) {
		span.SetAttributes(attrKeyDataloaderResultsCount.Int(len(results)))
		span.End()
	}
}

var (
	attrKeyDataloaderKey          = attribute.Key("dataloader.key")
	attrKeyDataloaderKeys         = attribute.Key("dataloader.keys")
	attrKeyDataloaderKeysCount    = attribute.Key("dataloader.keys_count")
	attrKeyDataloaderResultsCount = attribute.Key("dataloader.results_count")
)

func attrDataloaderKey[K comparable](key K) attribute.KeyValue {
	return attrKeyDataloaderKey.String(fmt.Sprintf("%#v", key))
}

func attrDataloaderKeys[K comparable](keys []K) attribute.KeyValue {
	return attrKeyDataloaderKeys.String(fmt.Sprintf("%#v", keys))
}
