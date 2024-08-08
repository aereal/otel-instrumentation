package otelsqs

import (
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"go.opentelemetry.io/otel/propagation"
)

type SQSSystemAttributesCarrier struct {
	Attributes map[string]types.MessageSystemAttributeValue
}

var _ propagation.TextMapCarrier = (*SQSSystemAttributesCarrier)(nil)

func (c *SQSSystemAttributesCarrier) Get(key string) string {
	if c.Attributes == nil {
		return ""
	}
	val, ok := c.Attributes[key]
	if !ok {
		return ""
	}
	sv := val.StringValue
	if sv == nil {
		return ""
	}
	return *sv
}

func (c *SQSSystemAttributesCarrier) Set(key, value string) {
	if c.Attributes == nil {
		c.Attributes = map[string]types.MessageSystemAttributeValue{}
	}
	c.Attributes[key] = types.MessageSystemAttributeValue{DataType: ref("String"), StringValue: ref(value)}
}

func (c *SQSSystemAttributesCarrier) Keys() []string {
	keys := make([]string, 0, len(c.Attributes))
	for k := range c.Attributes {
		keys = append(keys, k)
	}
	return keys
}

func ref[T any](v T) *T { return &v }
