package oteldataloader_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/aereal/otel-instrumentation/github.com/graph-gophers/dataloader/oteldataloader"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/graph-gophers/dataloader/v7"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"go.opentelemetry.io/otel/trace"
)

func TestTracer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	if deadline, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	}
	defer cancel()
	testCases := []struct {
		name      string
		before    func(ctx context.Context, l *dataloader.Loader[int, string])
		wantSpans tracetest.SpanStubs
	}{
		{
			name: "ok/Load",
			before: func(ctx context.Context, l *dataloader.Loader[int, string]) {
				_, _ = l.Load(ctx, 0)()
			},
			wantSpans: tracetest.SpanStubs{
				{
					Name:     "dataloader.Load test",
					SpanKind: trace.SpanKindInternal,
					Resource: resource.NewWithAttributes(semconv.SchemaURL),
					Attributes: []attribute.KeyValue{
						attribute.String("dataloader.key", "0"),
					},
				},
				{
					Name:     "dataloader.Batch test",
					SpanKind: trace.SpanKindInternal,
					Resource: resource.NewWithAttributes(semconv.SchemaURL),
					Attributes: []attribute.KeyValue{
						attribute.String("dataloader.keys", "[]int{0}"),
						attribute.Int("dataloader.keys_count", 1),
						attribute.Int("dataloader.results_count", 0),
					},
				},
			},
		},
		{
			name: "ok/LoadMany",
			before: func(ctx context.Context, l *dataloader.Loader[int, string]) {
				_, _ = l.LoadMany(ctx, []int{0, 1, 2})()
			},
			wantSpans: tracetest.SpanStubs{
				{
					Name:     "dataloader.LoadMany test",
					SpanKind: trace.SpanKindInternal,
					Resource: resource.NewWithAttributes(semconv.SchemaURL),
					Attributes: []attribute.KeyValue{
						attribute.String("dataloader.keys", "[]int{0, 1, 2}"),
						attribute.Int("dataloader.keys_count", 3),
					},
				},
				{
					Name:     "dataloader.Load test",
					SpanKind: trace.SpanKindInternal,
					Resource: resource.NewWithAttributes(semconv.SchemaURL),
					Attributes: []attribute.KeyValue{
						attribute.String("dataloader.key", "0"),
					},
				},
				{
					Name:     "dataloader.Load test",
					SpanKind: trace.SpanKindInternal,
					Resource: resource.NewWithAttributes(semconv.SchemaURL),
					Attributes: []attribute.KeyValue{
						attribute.String("dataloader.key", "1"),
					},
				},
				{
					Name:     "dataloader.Load test",
					SpanKind: trace.SpanKindInternal,
					Resource: resource.NewWithAttributes(semconv.SchemaURL),
					Attributes: []attribute.KeyValue{
						attribute.String("dataloader.key", "2"),
					},
				},
				{
					Name:     "dataloader.Batch test",
					SpanKind: trace.SpanKindInternal,
					Resource: resource.NewWithAttributes(semconv.SchemaURL),
					Attributes: []attribute.KeyValue{
						attribute.String("dataloader.keys", "[]int{0, 1, 2}"),
						attribute.Int("dataloader.keys_count", 3),
						attribute.Int("dataloader.results_count", 0),
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			res := resource.NewWithAttributes(semconv.SchemaURL)
			tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
			loader := dataloader.NewBatchedLoader(
				testLoaderFunc,
				dataloader.WithCache[int, string](&dataloader.NoCache[int, string]{}),
				oteldataloader.WithTracer[int, string](oteldataloader.WithName("test"), oteldataloader.WithTracerProvider(tp)),
			)
			tc.before(ctx, loader)
			if err := tp.ForceFlush(ctx); err != nil {
				t.Fatal(err)
			}
			gotSpans := exporter.GetSpans()
			if diff := cmpSpans(tc.wantSpans, gotSpans); diff != "" {
				t.Errorf("-want, +got:\n%s", diff)
			}
		})
	}
}

func testLoaderFunc(ctx context.Context, ids []int) []*dataloader.Result[string] {
	results := make([]*dataloader.Result[string], len(ids))
	for i, id := range ids {
		var r *dataloader.Result[string]
		if i%2 == 0 {
			r = &dataloader.Result[string]{Data: strconv.Itoa(id)}
		} else {
			r = &dataloader.Result[string]{Error: errors.New("oops")}
		}
		results[i] = r
	}
	return results
}

func cmpSpans(want, got tracetest.SpanStubs) string {
	opts := []cmp.Option{
		cmp.Transformer("attribute.KeyValue", transformKeyValue),
		cmp.Transformer("trace.SpanContext", transformSpanContext),
		cmpopts.IgnoreFields(sdktrace.Event{}, "Time"),
		cmpopts.IgnoreFields(tracetest.SpanStub{}, "Parent", "SpanContext", "StartTime", "EndTime", "DroppedAttributes", "DroppedEvents", "DroppedLinks", "ChildSpanCount", "InstrumentationLibrary"),
	}
	return cmp.Diff(want, got, opts...)
}

func transformKeyValue(kv attribute.KeyValue) map[attribute.Key]any {
	if kv.Key == semconv.ExceptionStacktraceKey {
		return map[attribute.Key]any{semconv.ExceptionStacktraceKey: "stacktrace"}
	}
	return map[attribute.Key]any{kv.Key: kv.Value.AsInterface()}
}

func transformSpanContext(sc trace.SpanContext) map[string]any {
	b, err := json.Marshal(sc)
	if err != nil {
		panic(err)
	}
	var scc map[string]any
	if err := json.Unmarshal(b, &scc); err != nil {
		panic(err)
	}
	return scc
}
