package otelsqs_test

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aereal/otel-instrumentation/github.com/aws/aws-sdk-go-v2/service/sqs/otelsqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

var (
	queueURL     = "http://aws.test/123456789012/my-queue"
	emptyTraceID trace.TraceID
)

func TestMiddleware_SendMessage(t *testing.T) {
	body := `{"msg":"0xdeadbeaf"}`
	msgID := "msg-1"
	testCases := []struct {
		name   string
		input  *sqs.SendMessageInput
		output *sqs.SendMessageOutput
		want   tracetest.SpanStubs
	}{
		{
			"ok",
			&sqs.SendMessageInput{
				MessageBody: &body,
				QueueUrl:    &queueURL,
			},
			&sqs.SendMessageOutput{
				MessageId:      &msgID,
				SequenceNumber: ref("1"),
			},
			tracetest.SpanStubs{
				{
					Name:     "publish my-queue",
					SpanKind: trace.SpanKindProducer,
					Attributes: []attribute.KeyValue{
						attribute.String("rpc.system", "aws-api"),
						attribute.String("rpc.method", "SendMessage"),
						attribute.String("rpc.service", "SQS"),
						attribute.String("messaging.operation.name", "SendMessage"),
						attribute.String("messaging.system", "aws_sqs"),
						attribute.String("messaging.operation.type", "publish"),
						attribute.String("messaging.destination.name", queueURL),
						attribute.String("network.peer.address", queueURL),
						attribute.Int("messaging.message.body.size", len(body)),
						attribute.String("messaging.message.id", msgID),
					},
				},
				{
					Name:           "SendMessage",
					SpanKind:       trace.SpanKindInternal,
					ChildSpanCount: 1,
				},
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	if deadline, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(context.Background(), deadline)
	}
	defer cancel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			propagator := propagation.TraceContext{}
			var traceID string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer r.Body.Close()
				input := new(sqs.SendMessageInput)
				if err := json.NewDecoder(r.Body).Decode(input); err != nil {
					t.Errorf("failed to decode request body: %+v", err)
				}
				carrier := &otelsqs.SQSSystemAttributesCarrier{Attributes: input.MessageSystemAttributes}
				sc := trace.SpanContextFromContext(propagator.Extract(context.Background(), carrier))
				traceID = sc.TraceID().String()
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(tc.output)
			}))
			t.Cleanup(srv.Close)

			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
			var opts sqs.Options
			opts.BaseEndpoint = &srv.URL
			otelsqs.AppendMiddlewares(&opts.APIOptions, otelsqs.WithTracerProvider(tp), otelsqs.WithPropagator(propagator))
			client := sqs.New(opts)
			ctx, span := tp.Tracer("test").Start(ctx, "SendMessage")
			if traceID == emptyTraceID.String() {
				t.Errorf("expected trace ID injected but got nothing")
			}
			_, err := client.SendMessage(ctx, tc.input)
			if err != nil {
				t.Fatalf("SendMessage: %+v", err)
			}
			span.End()
			if err := tp.ForceFlush(ctx); err != nil {
				t.Fatalf("ForceFlush: %+v", err)
			}
			if diff := cmpSpans(tc.want, exporter.GetSpans()); diff != "" {
				t.Errorf("spans (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestMiddleware_SendMessageBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	if deadline, ok := t.Deadline(); ok {
		ctx, cancel = context.WithDeadline(context.Background(), deadline)
	}
	defer cancel()

	testCases := []struct {
		name   string
		input  *sqs.SendMessageBatchInput
		output *sqs.SendMessageBatchOutput
		want   tracetest.SpanStubs
	}{
		{
			"ok",
			&sqs.SendMessageBatchInput{
				QueueUrl: &queueURL,
				Entries: []types.SendMessageBatchRequestEntry{
					{
						Id:          ref("batch-1"),
						MessageBody: ref(`{"id":"msg-1"}`),
					},
					{
						Id:          ref("batch-2"),
						MessageBody: ref(`{"id":"msg-2"}`),
					},
					{
						Id:          ref("batch-3"),
						MessageBody: ref(`{"id":"msg-3"}`),
					},
					{
						Id:          ref("batch-4"),
						MessageBody: ref(`{"id":"msg-4"}`),
					},
				},
			},
			&sqs.SendMessageBatchOutput{
				Successful: []types.SendMessageBatchResultEntry{
					{
						Id:               ref("batch-1"),
						MD5OfMessageBody: ref(md5Digest(`{"id":"msg-1"}`)),
						MessageId:        ref("msg-1"),
					},
					{
						Id:               ref("batch-2"),
						MD5OfMessageBody: ref(md5Digest(`{"id":"msg-2"}`)),
						MessageId:        ref("msg-2"),
					},
				},
				Failed: []types.BatchResultErrorEntry{
					{
						Code:        ref("SomeError"),
						Id:          ref("batch-3"),
						SenderFault: false,
						Message:     ref("oops"),
					},
					{
						Code:        ref("AnotherError"),
						Id:          ref("batch-4"),
						SenderFault: true,
					},
				},
			},
			tracetest.SpanStubs{
				{
					Name:           "publish my-queue",
					SpanKind:       trace.SpanKindClient,
					ChildSpanCount: 4,
					Attributes: []attribute.KeyValue{
						attribute.String("rpc.system", "aws-api"),
						attribute.String("rpc.method", "SendMessageBatch"),
						attribute.String("rpc.service", "SQS"),
						attribute.String("messaging.operation.name", "SendMessageBatch"),
						attribute.Int64("messaging.batch.message_count", 4),
						attribute.String("messaging.system", "aws_sqs"),
						attribute.String("messaging.operation.type", "publish"),
						attribute.String("messaging.destination.name", queueURL),
						attribute.String("network.peer.address", queueURL),
					},
				},
				{
					Name:     "create my-queue",
					SpanKind: trace.SpanKindProducer,
					Status:   sdktrace.Status{Code: codes.Error, Description: "AnotherError"},
					Events: []sdktrace.Event{
						{
							Name: "exception",
							Attributes: []attribute.KeyValue{
								attribute.String("aws.sqs.batch_request_id", "batch-4"),
								attribute.String("aws.error.code", "AnotherError"),
								attribute.Bool("aws.error.sender_fault", true),
								attribute.String("exception.type", "*otelsqs.SendMessageBatchError"),
								attribute.String("exception.message", "AnotherError"),
							},
						},
					},
					Attributes: []attribute.KeyValue{
						attribute.String("messaging.system", "aws_sqs"),
						attribute.String("messaging.operation.type", "create"),
						attribute.String("messaging.destination.name", queueURL),
						attribute.String("network.peer.address", queueURL),
						attribute.String("aws.sqs.batch_request_id", "batch-4"),
					},
				},
				{
					Name:     "create my-queue",
					SpanKind: trace.SpanKindProducer,
					Status:   sdktrace.Status{Code: codes.Error, Description: "SomeError: oops"},
					Events: []sdktrace.Event{
						{
							Name: "exception",
							Attributes: []attribute.KeyValue{
								attribute.String("aws.sqs.batch_request_id", "batch-3"),
								attribute.String("aws.error.code", "SomeError"),
								attribute.Bool("aws.error.sender_fault", false),
								attribute.String("exception.type", "*otelsqs.SendMessageBatchError"),
								attribute.String("exception.message", "SomeError: oops"),
							},
						},
					},
					Attributes: []attribute.KeyValue{
						attribute.String("messaging.system", "aws_sqs"),
						attribute.String("messaging.operation.type", "create"),
						attribute.String("messaging.destination.name", queueURL),
						attribute.String("network.peer.address", queueURL),
						attribute.String("aws.sqs.batch_request_id", "batch-3"),
					},
				},
				{
					Name:     "create my-queue",
					SpanKind: trace.SpanKindProducer,
					Status:   sdktrace.Status{Code: codes.Ok},
					Attributes: []attribute.KeyValue{
						attribute.String("messaging.system", "aws_sqs"),
						attribute.String("messaging.operation.type", "create"),
						attribute.String("messaging.destination.name", queueURL),
						attribute.String("network.peer.address", queueURL),
						attribute.String("aws.sqs.batch_request_id", "batch-2"),
						attribute.String("messaging.message.id", "msg-2"),
					},
				},
				{
					Name:     "create my-queue",
					SpanKind: trace.SpanKindProducer,
					Status:   sdktrace.Status{Code: codes.Ok},
					Attributes: []attribute.KeyValue{
						attribute.String("messaging.system", "aws_sqs"),
						attribute.String("messaging.operation.type", "create"),
						attribute.String("messaging.destination.name", queueURL),
						attribute.String("network.peer.address", queueURL),
						attribute.String("aws.sqs.batch_request_id", "batch-1"),
						attribute.String("messaging.message.id", "msg-1"),
					},
				},
				{
					Name:           "SendMessageBatch",
					SpanKind:       trace.SpanKindInternal,
					ChildSpanCount: 1,
				},
			},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			propagator := propagation.TraceContext{}
			traceIDs := make([]trace.TraceID, 0)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer r.Body.Close()
				input := new(sqs.SendMessageBatchInput)
				if err := json.NewDecoder(r.Body).Decode(input); err != nil {
					t.Errorf("failed to decode request body: %+v", err)
				}
				for _, entry := range input.Entries {
					carrier := &otelsqs.SQSSystemAttributesCarrier{Attributes: entry.MessageSystemAttributes}
					sc := trace.SpanContextFromContext(propagator.Extract(context.Background(), carrier))
					if traceID := sc.TraceID(); traceID.IsValid() {
						traceIDs = append(traceIDs, sc.TraceID())
					}
				}
				w.Header().Set("content-type", "application/json")
				_ = json.NewEncoder(w).Encode(tc.output)
			}))
			t.Cleanup(srv.Close)

			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
			var opts sqs.Options
			opts.BaseEndpoint = &srv.URL
			otelsqs.AppendMiddlewares(&opts.APIOptions, otelsqs.WithTracerProvider(tp), otelsqs.WithPropagator(propagator))
			client := sqs.New(opts)
			ctx, span := tp.Tracer("test").Start(ctx, "SendMessageBatch")
			_, err := client.SendMessageBatch(ctx, tc.input)
			if err != nil {
				t.Fatalf("SendMessage: %+v", err)
			}
			span.End()
			if len(traceIDs) != len(tc.input.Entries) {
				t.Errorf("the number of trace IDs in the message system attributes mismatch:\n\twant: %d\n\t got: %d", len(tc.input.Entries), len(traceIDs))
			}
			if err := tp.ForceFlush(ctx); err != nil {
				t.Fatalf("ForceFlush: %+v", err)
			}
			if diff := cmpSpans(tc.want, exporter.GetSpans()); diff != "" {
				t.Errorf("spans (-want, +got):\n%s", diff)
			}
		})
	}
}

func cmpSpans(want, got tracetest.SpanStubs) string {
	return cmp.Diff(want, got, cmpSpansOptions...)
}

var cmpSpansOptions = []cmp.Option{
	cmp.Transformer("attribute.KeyValue", transformAttr),
	cmpopts.IgnoreFields(tracetest.SpanStub{},
		"Parent", "SpanContext", "StartTime", "EndTime",
		"Resource", "InstrumentationLibrary",
		"DroppedAttributes", "DroppedEvents", "DroppedLinks"),
	cmpopts.IgnoreFields(sdktrace.Event{}, "Time"),
}

func transformAttr(attr attribute.KeyValue) map[attribute.Key]any {
	return map[attribute.Key]any{attr.Key: attr.Value.AsInterface()}
}

func ref[T any](v T) *T { return &v }

func md5Digest(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}
