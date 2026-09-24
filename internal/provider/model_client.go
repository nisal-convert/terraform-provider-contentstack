package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/labd/contentstack-go-sdk/management"
)

type modelClient struct {
	baseURL    string
	httpClient *http.Client
	headers    http.Header
}

type modelInput struct {
	UID               *string         `json:"uid,omitempty"`
	Title             *string         `json:"title,omitempty"`
	Description       *string         `json:"description,omitempty"`
	Schema            json.RawMessage `json:"schema,omitempty"`
	Options           json.RawMessage `json:"options,omitempty"`
	FieldRules        json.RawMessage `json:"field_rules,omitempty"`
	MaintainRevisions *bool           `json:"maintain_revisions,omitempty"`
}

type modelResponse struct {
	UID               string          `json:"uid"`
	Title             string          `json:"title"`
	Description       string          `json:"description"`
	Schema            json.RawMessage `json:"schema"`
	Options           json.RawMessage `json:"options"`
	FieldRules        json.RawMessage `json:"field_rules"`
	MaintainRevisions bool            `json:"maintain_revisions"`
}

func newModelClient(config management.ClientConfig, auth management.StackAuth) *modelClient {
	headers := make(http.Header)
	headers.Set("api_key", auth.ApiKey)
	if auth.ManagementToken != "" {
		headers.Set("authorization", auth.ManagementToken)
	} else {
		headers.Set("authtoken", config.AuthToken)
	}
	if auth.Branch != "" {
		headers.Set("branch", auth.Branch)
	}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	return &modelClient{baseURL: config.BaseURL, httpClient: config.HTTPClient, headers: headers}
}

func (c *modelClient) request(ctx context.Context, method, kind, uid string, input *modelInput) (*modelResponse, error) {
	if kind != "content_type" && kind != "global_field" {
		return nil, fmt.Errorf("unsupported model kind: %s", kind)
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	endpoint := base.ResolveReference(&url.URL{Path: "/v3/" + kind + "s/" + uid})
	if method == http.MethodGet {
		endpoint.RawQuery = "include_global_field_schema=false"
	}
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(map[string]*modelInput{kind: input})
		if err != nil {
			return nil, fmt.Errorf("encode %s request: %w", kind, err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header = c.headers.Clone()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, &management.ErrorMessage{ErrorMessage: "Resource not found", ErrorCode: 404}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Contentstack %s request failed (HTTP %d)", kind, resp.StatusCode)
	}
	if method == http.MethodDelete {
		return nil, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", kind, err)
	}
	var result modelResponse
	if err := json.Unmarshal(envelope[kind], &result); err != nil {
		return nil, fmt.Errorf("decode %s model: %w", kind, err)
	}
	if result.UID == "" {
		return nil, fmt.Errorf("Contentstack %s response is missing a UID", kind)
	}
	return &result, nil
}
