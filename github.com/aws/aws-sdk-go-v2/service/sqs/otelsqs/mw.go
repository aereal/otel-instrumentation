package otelsqs

import (
	"context"
	"fmt"
	neturl "net/url"
	"path"
	"strings"
	"time"

	sdkmw "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	mw "github.com/aws/smithy-go/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	scope                     = "github.com/aereal/otel-instrumentation/github.com/aws/aws-sdk-go-v2/service/sqs/otelsqs"
	idOtelAWSInitializeBefore = "OTelInitializeMiddlewareBefore"
	idOtelAWSInitializeAfter  = "OTelInitializeMiddlewareAfter"
)

var (
	KeyAWSErrorCode      = attribute.Key("aws.error.code")
	KeyAWSErrorSendFault = attribute.Key("aws.error.sender_fault")
	KeySQSBatchRequestID = attribute.Key("aws.sqs.batch_request_id")

	attrRPCSystemAWSAPI    = semconv.RPCSystemKey.String("aws-api")
	attrOpSendMessage      = semconv.MessagingOperationName("SendMessage")
	attrOpSendMessageBatch = semconv.MessagingOperationName("SendMessageBatch")
	initializeBefore       = mw.InitializeMiddlewareFunc(scope+".InitializeBefore", func(ctx context.Context, input mw.InitializeInput, handler mw.InitializeHandler) (mw.InitializeOutput, mw.Metadata, error) {
		return handler.HandleInitialize(context.WithValue(ctx, spanTimestampKey{}, time.Now()), input)
	})
)

type config struct {
	tracerProvider trace.TracerProvider
}

type Option func(*config)

func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *config) { c.tracerProvider = tp }
}

func AppendMiddlewares(apiOptions *[]func(*mw.Stack) error, opts ...Option) {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.tracerProvider == nil {
		cfg.tracerProvider = otel.GetTracerProvider()
	}
	r := &root{
		tracer: cfg.tracerProvider.Tracer(scope),
	}
	*apiOptions = append(*apiOptions, r.initializeBefore, r.initializeAfter)
}

type spanTimestampKey struct{}

type root struct {
	tracer trace.Tracer
}

func (r *root) initializeBefore(stack *mw.Stack) error {
	if swapped := swap(stack.Initialize, idOtelAWSInitializeBefore, initializeBefore); swapped {
		return nil
	}
	return stack.Initialize.Add(initializeBefore, mw.Before)
}

func (r *root) initializeAfter(stack *mw.Stack) error {
	fn := mw.InitializeMiddlewareFunc(scope+".InitializeAfter", func(ctx context.Context, input mw.InitializeInput, handler mw.InitializeHandler) (mw.InitializeOutput, mw.Metadata, error) {
		ts := ctx.Value(spanTimestampKey{}).(time.Time)
		serviceID := sdkmw.GetServiceID(ctx)
		operation := sdkmw.GetOperationName(ctx)

		createSpanByRequestID := map[string]trace.Span{}
		var span trace.Span
		switch params := input.Parameters.(type) {
		case *sqs.SendMessageInput:
			attrs := make([]attribute.KeyValue, 0, 6)
			attrs = append(attrs,
				attrRPCSystemAWSAPI,
				semconv.RPCMethod(operation),
				semconv.RPCService(serviceID),
				attrOpSendMessage,
				semconv.MessagingSystemAWSSqs,
				semconv.MessagingOperationTypePublish,
			)
			addAttrsFromQueueURL(&attrs, params.QueueUrl)
			addAttrsFromMessageBody(&attrs, params.MessageBody)
			queueName := getQueueName(*params.QueueUrl)
			ctx, span = r.tracer.Start(ctx, fmt.Sprintf("publish %s", queueName),
				trace.WithSpanKind(trace.SpanKindProducer),
				trace.WithAttributes(attrs...))
		case *sqs.SendMessageBatchInput:
			attrs := make([]attribute.KeyValue, 0, 6)
			attrs = append(attrs,
				attrRPCSystemAWSAPI,
				semconv.RPCMethod(operation),
				semconv.RPCService(serviceID),
				attrOpSendMessageBatch,
				semconv.MessagingBatchMessageCount(len(params.Entries)),
				semconv.MessagingSystemAWSSqs,
				semconv.MessagingOperationTypePublish,
			)
			addAttrsFromQueueURL(&attrs, params.QueueUrl)
			queueName := getQueueName(*params.QueueUrl)
			ctx, span = r.tracer.Start(ctx, fmt.Sprintf("publish %s", queueName),
				trace.WithSpanKind(trace.SpanKindClient),
				trace.WithAttributes(attrs...))
			for _, entry := range params.Entries {
				entry := entry
				batchID := *entry.Id
				_, createSpan := r.tracer.Start(ctx, fmt.Sprintf("create %s", queueName),
					trace.WithAttributes(
						semconv.MessagingSystemAWSSqs,
						semconv.MessagingOperationTypeCreate,
						semconv.MessagingDestinationName(*params.QueueUrl),
						semconv.NetworkPeerAddress(*params.QueueUrl),
						KeySQSBatchRequestID.String(batchID),
					),
					trace.WithTimestamp(ts),
					trace.WithSpanKind(trace.SpanKindProducer))
				createSpanByRequestID[batchID] = createSpan
				defer createSpan.End()
			}
		default:
			ctx, span = r.tracer.Start(ctx, defaultSpanName(serviceID, operation),
				trace.WithTimestamp(ts),
				trace.WithSpanKind(trace.SpanKindClient))
		}
		defer span.End()

		out, metadata, err := handler.HandleInitialize(ctx, input)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		if requestID, ok := sdkmw.GetRequestIDMetadata(metadata); ok {
			span.SetAttributes(semconv.AWSRequestID(requestID))
		}

		if out.Result != nil {
			switch res := out.Result.(type) {
			case *sqs.SendMessageOutput:
				if msgID := res.MessageId; msgID != nil {
					span.SetAttributes(semconv.MessagingMessageID(*msgID))
				}
			case *sqs.SendMessageBatchOutput:
				for _, successfulEntry := range res.Successful {
					batchID := *successfulEntry.Id
					createSpan, ok := createSpanByRequestID[batchID]
					if !ok {
						continue
					}
					createSpan.SetAttributes(semconv.MessagingMessageID(*successfulEntry.MessageId))
					createSpan.SetStatus(codes.Ok, "")
				}
				for _, failedEntry := range res.Failed {
					failedEntry := failedEntry
					createSpan, ok := createSpanByRequestID[*failedEntry.Id]
					if !ok {
						continue
					}
					recordSendMessageBatchError(createSpan, &SendMessageBatchError{Entry: failedEntry})
				}
			}
		}
		return out, metadata, err
	})
	if swapped := swap(stack.Initialize, idOtelAWSInitializeAfter, fn); swapped {
		return nil
	}
	return stack.Initialize.Add(fn, mw.After)
}

func addAttrsFromQueueURL(attrs *[]attribute.KeyValue, queueURL *string) {
	if queueURL != nil {
		*attrs = append(*attrs, semconv.MessagingDestinationName(*queueURL), semconv.NetworkPeerAddress(*queueURL))
	}
}

func addAttrsFromMessageBody(attrs *[]attribute.KeyValue, body *string) {
	if body != nil {
		*attrs = append(*attrs, semconv.MessagingMessageBodySize(len(*body)))
	}
}

type swappableStep[T any] interface {
	Swap(string, T) (T, error)
}

func swap[T any](step swappableStep[T], id string, mwFn T) bool {
	_, err := step.Swap(id, mwFn)
	return err == nil
}

func defaultSpanName(serviceID, operation string) string {
	b := new(strings.Builder)
	b.WriteString(serviceID)
	if operation != "" {
		b.Grow(len(operation) + 1)
		b.WriteRune('.')
		b.WriteString(operation)
	}
	return b.String()
}

func getQueueName(queueURL string) string {
	url, err := neturl.Parse(queueURL)
	if err != nil {
		return queueURL
	}
	return path.Base(url.Path)
}

type SendMessageBatchError struct {
	Entry types.BatchResultErrorEntry
}

func (e *SendMessageBatchError) Code() string {
	if e == nil || e.Entry.Code == nil {
		return ""
	}
	return *e.Entry.Code
}

func (e *SendMessageBatchError) Message() string {
	if e == nil || e.Entry.Message == nil {
		return ""
	}
	return *e.Entry.Message
}

func (e *SendMessageBatchError) BatchRequestID() string {
	if e == nil || e.Entry.Id == nil {
		return ""
	}
	return *e.Entry.Id
}

func (e *SendMessageBatchError) SenderFault() bool {
	if e == nil {
		return false
	}
	return e.Entry.SenderFault
}

func (e *SendMessageBatchError) Error() string {
	b := new(strings.Builder)
	b.WriteString(e.Code())
	if msg := e.Message(); msg != "" {
		b.Grow(len(msg) + 2)
		b.WriteString(": ")
		b.WriteString(msg)
	}
	return b.String()
}

func recordSendMessageBatchError(span trace.Span, err *SendMessageBatchError) {
	span.RecordError(err, trace.WithAttributes(
		KeySQSBatchRequestID.String(err.BatchRequestID()),
		KeyAWSErrorCode.String(err.Code()),
		KeyAWSErrorSendFault.Bool(err.SenderFault())))
	span.SetStatus(codes.Error, err.Error())
}
