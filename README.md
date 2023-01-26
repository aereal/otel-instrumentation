[![status][ci-status-badge]][ci-status]
[![PkgGoDev][pkg-go-dev-badge]][pkg-go-dev]

# otel-instrumentation

**otel-instrumentation** provides some OpenTelemetry instrumentation utilities.

## Synopsis

```go
import (
	"github.com/aereal/otel-instrumentation/awslambda"
	"github.com/aws/aws-lambda-go/events"
	"go.opentelemetry.io/otel/trace"
)

var (
  tracer       trace.Tracer // it must be created properly
  sqsQueueName string
)

func handleReceiveSQSEvent(ctx context.Context, ev events.SQSEvent) error {
  ctx, span := awslambda.StartTraceFromSQSEvent(ctx, tracer, queueName)
  defer span.End()
  for _, msg := range ev.Records {
    handleReceiveSQSMessage(ctx, msg)
  }
  return nil
}

func handleReceiveSQSMessage(ctx context.Context, msg events.SQSMessage) {
  ctx, span := awslambda.StartTraceFromSQSMessage(ctx, tracer, queueName, msg)
  defer span.End()
}
```

## Installation

```sh
go get github.com/aereal/otel-instrumentation/awslambda
```

## License

See LICENSE file.

[pkg-go-dev]: https://pkg.go.dev/github.com/aereal/otel-instrumentation
[pkg-go-dev-badge]: https://pkg.go.dev/badge/aereal/otel-instrumentation
[ci-status-badge]: https://github.com/aereal/otel-instrumentation/workflows/CI/badge.svg?branch=main
[ci-status]: https://github.com/aereal/otel-instrumentation/actions/workflows/CI
