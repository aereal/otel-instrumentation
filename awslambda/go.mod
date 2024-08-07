module github.com/aereal/otel-instrumentation/awslambda

go 1.21

toolchain go1.22.6

require (
	github.com/aws/aws-lambda-go v1.47.0
	github.com/google/go-cmp v0.6.0
	go.opentelemetry.io/contrib/propagators/aws v1.28.0
	go.opentelemetry.io/otel v1.28.0
	go.opentelemetry.io/otel/sdk v1.28.0
	go.opentelemetry.io/otel/trace v1.28.0
)

require (
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/otel/metric v1.28.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
)
