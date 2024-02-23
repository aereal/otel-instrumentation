module github.com/aereal/otel-instrumentation/awslambda

go 1.19

require (
	github.com/aws/aws-lambda-go v1.37.0
	github.com/google/go-cmp v0.6.0
	go.opentelemetry.io/contrib/propagators/aws v1.24.0
	go.opentelemetry.io/otel v1.24.0
	go.opentelemetry.io/otel/sdk v1.24.0
	go.opentelemetry.io/otel/trace v1.24.0
)

require (
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	go.opentelemetry.io/otel/metric v1.24.0 // indirect
	golang.org/x/sys v0.17.0 // indirect
)
