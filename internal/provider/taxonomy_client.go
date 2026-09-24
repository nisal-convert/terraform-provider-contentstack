package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var errTaxonomyNotFound = errors.New("taxonomy resource not found")
var taxonomyUIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type taxonomyResponse struct {
	UID                    string  `json:"uid"`
	Name                   string  `json:"name"`
	Description            string  `json:"description"`
	Locale                 string  `json:"locale"`
	ParentUID              *string `json:"parent_uid"`
	ChildrenCount          *int    `json:"children_count"`
	ReferencedEntriesCount *int    `json:"referenced_entries_count"`
}

type taxonomyEnvelope struct {
	Taxonomy *taxonomyResponse  `json:"taxonomy"`
	Term     *taxonomyResponse  `json:"term"`
	Terms    []taxonomyResponse `json:"terms"`
}

type taxonomyUIDValidator struct{}

func (taxonomyUIDValidator) Description(context.Context) string {
	return "Use a non-empty UID containing letters, digits, underscores or hyphens."
}
func (v taxonomyUIDValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v taxonomyUIDValidator) Validate(ctx context.Context, req tfsdk.ValidateAttributeRequest, resp *tfsdk.ValidateAttributeResponse) {
	value, ok := req.AttributeConfig.(types.String)
	if ok && !value.IsNull() && !value.IsUnknown() && !taxonomyUIDPattern.MatchString(value.Value) {
		resp.Diagnostics.AddAttributeError(req.AttributePath, "Invalid taxonomy UID", v.Description(ctx))
	}
}
func taxonomyUIDAttribute(description string) tfsdk.Attribute {
	return tfsdk.Attribute{Type: types.StringType, Required: true, Description: description, Validators: []tfsdk.AttributeValidator{taxonomyUIDValidator{}}, PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()}}
}

func taxonomyImportParts(id string, length int) ([]string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != length {
		return nil, fmt.Errorf("expected %d slash-separated UID components", length)
	}
	for _, part := range parts {
		if !taxonomyUIDPattern.MatchString(part) {
			return nil, fmt.Errorf("invalid taxonomy import UID")
		}
	}
	return parts, nil
}

func (c *modelClient) taxonomyRequest(ctx context.Context, method string, segments []string, query url.Values, body any) (*taxonomyEnvelope, error) {
	for _, segment := range segments {
		if !taxonomyUIDPattern.MatchString(segment) {
			return nil, fmt.Errorf("invalid taxonomy endpoint segment")
		}
	}
	baseURL := c.baseURL
	if baseURL == "" {
		baseURL = "https://api.contentstack.io/"
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Contentstack base URL")
	}
	endpoint := base.ResolveReference(&url.URL{Path: "/v3/taxonomies"})
	if len(segments) > 0 {
		endpoint.Path += "/" + strings.Join(segments, "/")
	}
	endpoint.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode taxonomy request")
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("build taxonomy request")
	}
	request.Header = c.headers.Clone()
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Contentstack taxonomy transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, errTaxonomyNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Contentstack taxonomy request failed (HTTP %d)", response.StatusCode)
	}
	if method == http.MethodDelete {
		return &taxonomyEnvelope{}, nil
	}
	var result taxonomyEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid Contentstack taxonomy response")
	}
	return &result, nil
}

func (c *modelClient) taxonomy(ctx context.Context, method, uid string, body any) (*taxonomyResponse, error) {
	segments := []string{uid}
	if method == http.MethodPost {
		segments = nil
	}
	result, err := c.taxonomyRequest(ctx, method, segments, nil, body)
	if err != nil {
		return nil, err
	}
	if result.Taxonomy == nil || result.Taxonomy.UID != uid || result.Taxonomy.Name == "" {
		return nil, fmt.Errorf("Contentstack taxonomy response identity is invalid")
	}
	return result.Taxonomy, nil
}

func (c *modelClient) taxonomyTerm(ctx context.Context, method, taxonomyUID, uid string, move bool, body any) (*taxonomyResponse, error) {
	segments := []string{taxonomyUID, "terms", uid}
	if method == http.MethodPost {
		segments = segments[:2]
	}
	if move {
		segments = append(segments, "move")
	}
	query := url.Values{}
	if method == http.MethodGet {
		query.Set("include_children_count", "true")
		query.Set("include_referenced_entries_count", "true")
	}
	result, err := c.taxonomyRequest(ctx, method, segments, query, body)
	if err != nil {
		return nil, err
	}
	if result.Term == nil || result.Term.UID != uid || result.Term.Name == "" {
		return nil, fmt.Errorf("Contentstack term response identity is invalid")
	}
	return result.Term, nil
}
