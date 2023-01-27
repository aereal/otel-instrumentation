package awslambda

import (
	"context"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"go.opentelemetry.io/contrib/propagators/aws/xray"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	commonAttrs = []attribute.KeyValue{
		semconv.MessagingSystemKey.String("AmazonSQS"),
		semconv.MessagingOperationProcess,
		semconv.FaaSTriggerPubsub,
		attribute.String("messaging.source.kind", "queue"),
	}
	messagingSourceNameKey        = attribute.Key("messaging.source.name")
	messagingMessageIDKey         = attribute.Key("messaging.message.id")
	messagingBatchMessageCountKey = attribute.Key("messaging.batch.message_count")
)

const (
	attrAWSTraceHeader = "AWSTraceHeader"
	headerXrayID       = "X-Amzn-Trace-Id"
)

// StartTraceFromSQSEvent creates a new span that receives messages from SQS queue.
//
// It conforms to [SQS Event convention].
//
// [SQS Event convention]: https://opentelemetry.io/docs/reference/specification/trace/semantic_conventions/instrumentation/aws-lambda/#sqs-event
func StartTraceFromSQSEvent(ctx context.Context, tracer trace.Tracer, queueName string, ev events.SQSEvent) (context.Context, trace.Span) {
	attrs := commonAttrs[:]
	attrs = append(attrs, messagingSourceNameKey.String(queueName), messagingBatchMessageCountKey.Int(len(ev.Records)))
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	}
	return tracer.Start(ctx, fmt.Sprintf("%s process", queueName), opts...)
}

// StartTraceFromSQSMessage creates a new span that processes a message from SQS queue.
//
// It conforms to [SQS Message convention].
//
// [SQS Message convention]: https://opentelemetry.io/docs/reference/specification/trace/semantic_conventions/instrumentation/aws-lambda/#sqs-message
func StartTraceFromSQSMessage(ctx context.Context, tracer trace.Tracer, queueName string, msg events.SQSMessage) (context.Context, trace.Span) {
	attrs := commonAttrs[:]
	attrs = append(attrs, messagingSourceNameKey.String(queueName), messagingMessageIDKey.String(msg.MessageId))
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	}
	if traceID, ok := msg.Attributes[attrAWSTraceHeader]; ok {
		carrier := propagation.MapCarrier{}
		carrier.Set(headerXrayID, traceID)
		remoteCtx := xray.Propagator{}.Extract(ctx, carrier)
		link := trace.LinkFromContext(remoteCtx)
		opts = append(opts, trace.WithLinks(link))
	}
	return tracer.Start(ctx, fmt.Sprintf("%s process", queueName), opts...)
}
