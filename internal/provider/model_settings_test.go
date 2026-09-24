package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/labd/contentstack-go-sdk/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelSettingsInputs(t *testing.T) {
	options := `{"title":"title","singleton":true,"is_page":false,"url_prefix":false,"sub_title":[],"future_option":{"enabled":true}}`
	rules := `[{"conditions":[{"operand_field":"title","operator":"equals","value":"Example"}],"actions":[{"action":"show","target_field":"body"}]}]`
	contentType := ContentTypeData{
		UID: types.String{Value: "example"}, Title: types.String{Value: "Example"},
		Schema:  types.String{Value: `[{"uid":"title","data_type":"text"}]`},
		Options: types.String{Value: options}, FieldRules: types.String{Value: rules},
		MaintainRevisions: types.Bool{Value: false},
	}
	input := NewContentTypeInput(&contentType)
	data, err := json.Marshal(input)
	require.NoError(t, err)
	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &result))
	assert.JSONEq(t, options, string(result["options"]))
	assert.JSONEq(t, rules, string(result["field_rules"]))
	assert.Equal(t, "false", string(result["maintain_revisions"]))

	globalField := GlobalFieldData{
		UID: contentType.UID, Title: contentType.Title, Schema: contentType.Schema,
		FieldRules: types.String{Value: "[]"}, MaintainRevisions: types.Bool{Value: false},
	}
	data, err = json.Marshal(NewGlobalFieldInput(&globalField))
	require.NoError(t, err)
	result = nil
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, "[]", string(result["field_rules"]))
	assert.Equal(t, "false", string(result["maintain_revisions"]))
	assert.NotContains(t, result, "options")
}

func TestUnknownAndNullModelSettingsAreOmitted(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "null", true: "unknown"}[unknown], func(t *testing.T) {
			value := types.String{Null: !unknown, Unknown: unknown}
			input := NewContentTypeInput(&ContentTypeData{
				UID: value, Schema: value, Options: value, FieldRules: value,
				MaintainRevisions: types.Bool{Null: !unknown, Unknown: unknown},
			})
			data, err := json.Marshal(input)
			require.NoError(t, err)
			var result map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &result))
			for _, attribute := range []string{"uid", "schema", "options", "field_rules", "maintain_revisions"} {
				assert.NotContains(t, result, attribute)
			}
		})
	}
}

func TestModelJSONValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape byte
		value types.String
		valid bool
	}{
		{"object", '{', types.String{Value: `{"singleton":false}`}, true},
		{"array", '[', types.String{Value: `[]`}, true},
		{"wrong_shape", '{', types.String{Value: `[]`}, false},
		{"rules_object", '[', types.String{Value: `{}`}, false},
		{"json_null", '{', types.String{Value: `null`}, false},
		{"invalid", '[', types.String{Value: `[`}, false},
		{"trailing_data", '{', types.String{Value: `{} {}`}, false},
		{"unknown", '{', types.String{Unknown: true}, true},
		{"null", '[', types.String{Null: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var response tfsdk.ValidateAttributeResponse
			modelJSONValidator{shape: tc.shape}.Validate(context.Background(), tfsdk.ValidateAttributeRequest{
				AttributeConfig: tc.value, AttributePath: tftypes.NewAttributePath().WithAttributeName("settings"),
			}, &response)
			assert.Equal(t, !tc.valid, response.Diagnostics.HasError(), response.Diagnostics)
		})
	}
}

func TestModelJSONPreservesFormattingButExposesDrift(t *testing.T) {
	prior := types.String{Value: "{\n  \"singleton\": true, \"is_page\": false\n}"}
	actual := types.String{Value: `{"is_page":false,"singleton":true}`}
	assert.Equal(t, prior, preserveModelJSON(actual, prior))
	changed := types.String{Value: `{"is_page":false,"singleton":false}`}
	assert.Equal(t, changed, preserveModelJSON(changed, prior))
	assert.Equal(t, actual, preserveModelJSON(actual, types.String{Unknown: true}))
	assert.Equal(t, actual, preserveModelJSON(actual, types.String{Null: true}))
	largeNumber := types.String{Value: `{"value":9007199254740993}`}
	assert.Equal(t, largeNumber, preserveModelJSON(largeNumber, types.String{Value: `{"value":9007199254740992}`}))
	assert.Equal(t, types.String{Value: "[]"}, modelJSONState(nil, "[]"))
}

func TestModelStateReadsSettingsAndDoesNotHideRemoteChanges(t *testing.T) {
	remote := &modelResponse{
		UID: "example", Title: "Example", Schema: json.RawMessage(`[]`),
		Options:           json.RawMessage(`{"singleton":false,"is_page":true,"url_pattern":"/:title","url_prefix":false}`),
		FieldRules:        json.RawMessage(`[{"actions":[{"action":"hide","target_field":"body"}]}]`),
		MaintainRevisions: false,
	}
	state := NewContentTypeData(remote)
	prior := *state
	prior.Options = types.String{Value: `{"singleton":true,"is_page":false}`}
	prior.FieldRules = types.String{Value: `[]`}
	prior.MaintainRevisions = types.Bool{Value: true}
	MergeContentType(state, &prior)
	assert.JSONEq(t, string(remote.Options), state.Options.Value)
	assert.JSONEq(t, string(remote.FieldRules), state.FieldRules.Value)
	assert.False(t, state.MaintainRevisions.Value)
	global := NewGlobalFieldData(remote)
	MergeGlobalField(global, &GlobalFieldData{FieldRules: types.String{Value: `[]`}})
	assert.JSONEq(t, string(remote.FieldRules), global.FieldRules.Value)
}

func TestModelAttributesAreOptionalComputed(t *testing.T) {
	for _, kind := range []string{"content_type", "global_field"} {
		t.Run(kind, func(t *testing.T) {
			var schema tfsdk.Schema
			attributes := []string{"field_rules", "maintain_revisions"}
			if kind == "content_type" {
				schema, _ = (resourceContentTypeType{}).GetSchema(context.Background())
				attributes = append(attributes, "options")
			} else {
				schema, _ = (resourceGlobalFieldType{}).GetSchema(context.Background())
			}
			for _, name := range attributes {
				assert.True(t, schema.Attributes[name].Optional, name)
				assert.True(t, schema.Attributes[name].Computed, name)
			}
		})
	}
}

func TestModelClientRequests(t *testing.T) {
	for _, kind := range []string{"content_type", "global_field"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodGet, http.MethodDelete} {
			t.Run(kind+"/"+method, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, method, r.Method)
					assert.Equal(t, "/v3/"+kind+"s/example", r.URL.Path)
					if method == http.MethodGet {
						assert.Equal(t, "false", r.URL.Query().Get("include_global_field_schema"))
					}
					assert.Equal(t, "test-key", r.Header.Get("api_key"))
					assert.Equal(t, "test-token", r.Header.Get("authorization"))
					assert.Equal(t, "test-branch", r.Header.Get("branch"))
					assert.Empty(t, r.Header.Get("authtoken"))
					if method == http.MethodPost || method == http.MethodPut {
						var body map[string]json.RawMessage
						if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
							return
						}
						assert.JSONEq(t, `{"field_rules":[],"maintain_revisions":false}`, string(body[kind]))
					}
					if method == http.MethodDelete {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{kind: map[string]any{
						"uid": "example", "schema": []any{}, "field_rules": []any{}, "maintain_revisions": false,
						"options": map[string]any{"singleton": false, "url_prefix": false, "unknown": true},
					}})
				}))
				defer server.Close()
				client := newModelClient(management.ClientConfig{BaseURL: server.URL, HTTPClient: server.Client()},
					management.StackAuth{ApiKey: "test-key", ManagementToken: "test-token", Branch: "test-branch"})
				var input *modelInput
				if method == http.MethodPost || method == http.MethodPut {
					value := false
					input = &modelInput{FieldRules: json.RawMessage(`[]`), MaintainRevisions: &value}
				}
				result, err := client.request(context.Background(), method, kind, "example", input)
				require.NoError(t, err)
				if method != http.MethodDelete {
					require.NotNil(t, result)
					assert.JSONEq(t, `{"singleton":false,"url_prefix":false,"unknown":true}`, string(result.Options))
				}
			})
		}
	}
}

func TestModelClientErrorsAndAuthToken(t *testing.T) {
	for _, status := range []int{401, 403, 404, 422, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "auth-token", r.Header.Get("authtoken"))
				assert.Empty(t, r.Header.Get("authorization"))
				w.WriteHeader(status)
				w.Write([]byte(`{"error_message":"private-response"}`))
			}))
			defer server.Close()
			client := newModelClient(management.ClientConfig{BaseURL: server.URL, HTTPClient: server.Client(), AuthToken: "auth-token"}, management.StackAuth{})
			_, err := client.request(context.Background(), http.MethodGet, "content_type", "example", nil)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "private-response")
			assert.Equal(t, status == 404, IsNotFoundError(err))
		})
	}
}
