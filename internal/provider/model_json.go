package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type modelJSONValidator struct {
	shape byte
}

func (v modelJSONValidator) Description(context.Context) string {
	if v.shape == '{' {
		return "Must be a JSON object."
	}
	return "Must be a JSON array."
}

func (v modelJSONValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v modelJSONValidator) Validate(ctx context.Context, req tfsdk.ValidateAttributeRequest, resp *tfsdk.ValidateAttributeResponse) {
	value, ok := req.AttributeConfig.(types.String)
	if !ok || value.IsNull() || value.IsUnknown() {
		return
	}
	data := bytes.TrimSpace([]byte(value.Value))
	if !json.Valid(data) || len(data) == 0 || data[0] != v.shape {
		resp.Diagnostics.AddAttributeError(req.AttributePath, "Invalid model JSON", v.Description(ctx))
	}
}

func modelJSONAttribute(description string, shape byte) tfsdk.Attribute {
	return tfsdk.Attribute{
		Type:        types.StringType,
		Optional:    true,
		Computed:    true,
		Description: description,
		Validators:  []tfsdk.AttributeValidator{modelJSONValidator{shape: shape}},
	}
}

func modelJSONInput(value types.String) json.RawMessage {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return json.RawMessage(value.Value)
}

func modelStringInput(value types.String) *string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return &value.Value
}

func modelBoolInput(value types.Bool) *bool {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return &value.Value
}

func modelJSONState(value json.RawMessage, fallback string) types.String {
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		if fallback == "" {
			return types.String{Null: true}
		}
		return types.String{Value: fallback}
	}
	if normalized, err := normalizeModelJSON(string(value)); err == nil {
		return types.String{Value: string(normalized)}
	}
	return types.String{Value: string(value)}
}

func normalizeModelJSON(value string) ([]byte, error) {
	var decoded any
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	if !json.Valid([]byte(value)) {
		return nil, fmt.Errorf("invalid JSON")
	}
	return json.Marshal(decoded)
}

func preserveModelJSON(actual, prior types.String) types.String {
	if actual.IsNull() || actual.IsUnknown() || prior.IsNull() || prior.IsUnknown() {
		return actual
	}
	a, err := normalizeModelJSON(actual.Value)
	if err != nil {
		return actual
	}
	b, err := normalizeModelJSON(prior.Value)
	if err == nil && bytes.Equal(a, b) {
		return prior
	}
	return actual
}
