package otelsqs_test

import (
	"testing"

	"github.com/aereal/otel-instrumentation/github.com/aws/aws-sdk-go-v2/service/sqs/otelsqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/go-cmp/cmp"
)

func TestSQSSystemAttributesCarrier(t *testing.T) {
	keyNotExist := "not_exist"
	keyExistent := "existent"
	valExistent := "val"
	input := &sqs.SendMessageInput{MessageSystemAttributes: map[string]types.MessageSystemAttributeValue{}}
	c := otelsqs.SQSSystemAttributesCarrier{Attributes: input.MessageSystemAttributes}
	if diff := cmp.Diff([]string{}, c.Keys()); diff != "" {
		t.Errorf("Keys() (-want, +got):\n%s", diff)
	}
	if got := c.Get(keyNotExist); got != "" {
		t.Errorf("key=%s: wanted empty string but got: %q", keyNotExist, got)
	}
	if got := c.Get(keyExistent); got != "" {
		t.Errorf("key=%s: wanted empty string because Set is not called yet but got %q", keyExistent, got)
	}
	c.Set(keyExistent, valExistent)
	if got := c.Get(keyExistent); got != valExistent {
		t.Errorf("key=%s: wanted %q but got %q", keyExistent, valExistent, got)
	}
	if diff := cmp.Diff([]string{keyExistent}, c.Keys()); diff != "" {
		t.Errorf("Keys() (-want, +got):\n%s", diff)
	}
	wantAttrs := map[string]types.MessageSystemAttributeValue{
		keyExistent: {
			DataType:    ref("String"),
			StringValue: ref(valExistent),
		},
	}
	if diff := cmp.Diff(wantAttrs, input.MessageSystemAttributes, cmp.AllowUnexported(types.MessageSystemAttributeValue{})); diff != "" {
		t.Errorf("input.MessageSystemAttributes (-want, +got):\n%s", diff)
	}
}
