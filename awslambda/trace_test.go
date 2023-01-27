package awslambda_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aereal/otel-instrumentation/awslambda"
	"github.com/aws/aws-lambda-go/events"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

func TestStartTraceFromSQSMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	if deadline, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	}
	defer cancel()
	testCases := []struct {
		name      string
		queueName string
		message   events.SQSMessage
		wantSpans tracetest.SpanStubs
	}{
		{
			name:      "with no links",
			queueName: "test-queue",
			message: events.SQSMessage{
				MessageId: "msg-1",
			},
			wantSpans: tracetest.SpanStubs{
				{
					Name:     fmt.Sprintf("%s process", "test-queue"),
					SpanKind: trace.SpanKindConsumer,
					Attributes: []attribute.KeyValue{
						attribute.String("messaging.system", "AmazonSQS"),
						attribute.String("messaging.operation", "process"),
						attribute.String("faas.trigger", "pubsub"),
						attribute.String("messaging.source.kind", "queue"),
						attribute.String("messaging.source.name", "test-queue"),
						attribute.String("messaging.message.id", "msg-1"),
					},
				},
			},
		},
		{
			name:      "with links",
			queueName: "test-queue",
			message: events.SQSMessage{
				MessageId: "msg-1",
				Attributes: map[string]string{
					"AWSTraceHeader": "Root=1-5759e988-bd862e3fe1be46a994272793;Sampled=1;Parent=53995c3f42cd8ad8",
				},
			},
			wantSpans: tracetest.SpanStubs{
				{
					Name:     fmt.Sprintf("%s process", "test-queue"),
					SpanKind: trace.SpanKindConsumer,
					Attributes: []attribute.KeyValue{
						attribute.String("messaging.system", "AmazonSQS"),
						attribute.String("messaging.operation", "process"),
						attribute.String("faas.trigger", "pubsub"),
						attribute.String("messaging.source.kind", "queue"),
						attribute.String("messaging.source.name", "test-queue"),
						attribute.String("messaging.message.id", "msg-1"),
					},
					Links: []sdktrace.Link{{
						SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
							TraceID:    mustTraceIDFromHex("5759e988bd862e3fe1be46a994272793"),
							SpanID:     mustSpanIDFromHex("53995c3f42cd8ad8"),
							TraceFlags: trace.FlagsSampled,
							Remote:     true,
						}),
						Attributes: nil,
					}},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
			_, span := awslambda.StartTraceFromSQSMessage(ctx, tp.Tracer("test"), tc.queueName, tc.message)
			span.End()
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

func TestStartTraceFromSQSEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	if deadline, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	}
	defer cancel()

	testCases := []struct {
		queueName string
		wantSpans tracetest.SpanStubs
	}{
		{
			queueName: "test-queue-1",
			wantSpans: tracetest.SpanStubs{
				{
					Name:     "test-queue-1 process",
					SpanKind: trace.SpanKindConsumer,
					Attributes: []attribute.KeyValue{
						attribute.String("messaging.system", "AmazonSQS"),
						attribute.String("messaging.operation", "process"),
						attribute.String("faas.trigger", "pubsub"),
						attribute.String("messaging.source.kind", "queue"),
						attribute.String("messaging.source.name", "test-queue-1"),
					},
				},
			},
		},
		{
			queueName: "test-queue-2",
			wantSpans: tracetest.SpanStubs{
				{
					Name:     "test-queue-2 process",
					SpanKind: trace.SpanKindConsumer,
					Attributes: []attribute.KeyValue{
						attribute.String("messaging.system", "AmazonSQS"),
						attribute.String("messaging.operation", "process"),
						attribute.String("faas.trigger", "pubsub"),
						attribute.String("messaging.source.kind", "queue"),
						attribute.String("messaging.source.name", "test-queue-2"),
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.queueName, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
			_, span := awslambda.StartTraceFromSQSEvent(ctx, tp.Tracer("test"), tc.queueName)
			span.End()
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

func cmpSpans(want, got tracetest.SpanStubs) string {
	opts := []cmp.Option{
		cmp.Transformer("attribute.KeyValue", transformKeyValue),
		cmp.Transformer("trace.SpanContext", transformSpanContext),
		cmpopts.IgnoreFields(sdktrace.Event{}, "Time"),
		cmpopts.IgnoreFields(tracetest.SpanStub{}, "Parent", "SpanContext", "StartTime", "EndTime", "DroppedAttributes", "DroppedEvents", "DroppedLinks", "ChildSpanCount", "Resource", "InstrumentationLibrary"),
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

func mustSpanIDFromHex(v string) trace.SpanID {
	id, err := trace.SpanIDFromHex(v)
	if err != nil {
		panic(err)
	}
	return id
}

func mustTraceIDFromHex(v string) trace.TraceID {
	id, err := trace.TraceIDFromHex(v)
	if err != nil {
		panic(err)
	}
	return id
}
