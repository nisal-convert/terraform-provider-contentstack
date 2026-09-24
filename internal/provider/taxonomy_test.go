package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labd/contentstack-go-sdk/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaxonomyClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "key", r.Header.Get("api_key"))
		assert.Equal(t, "token", r.Header.Get("authorization"))
		assert.Equal(t, "main", r.Header.Get("branch"))
		assert.Empty(t, r.URL.Query().Get("force"))
		assert.Empty(t, r.URL.Query().Get("locale"))
		if r.URL.Path == "/v3/taxonomies/missing" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/v3/taxonomies/denied" {
			http.Error(w, "secret-token", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/v3/taxonomies/wrong" {
			json.NewEncoder(w).Encode(map[string]any{"taxonomy": map[string]any{"uid": "other", "name": "Other"}})
			return
		}
		if r.URL.Path == "/v3/taxonomies/topics/terms/child/move" {
			assert.Equal(t, http.MethodPut, r.Method)
			var body map[string]map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Nil(t, body["term"]["parent_uid"])
			assert.Equal(t, float64(1), body["term"]["order"])
			json.NewEncoder(w).Encode(map[string]any{"term": map[string]any{"uid": "child", "name": "Child", "parent_uid": nil, "locale": "en-us"}})
			return
		}
		if r.URL.Path == "/v3/taxonomies/topics/terms/child" {
			assert.Equal(t, "true", r.URL.Query().Get("include_children_count"))
			assert.Equal(t, "true", r.URL.Query().Get("include_referenced_entries_count"))
			json.NewEncoder(w).Encode(map[string]any{"term": map[string]any{"uid": "child", "name": "Child", "parent_uid": "parent", "children_count": 0, "referenced_entries_count": 0, "locale": "en-us"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"taxonomy": map[string]any{"uid": "topics", "name": "Topics", "description": "", "locale": "en-us"}})
	}))
	defer server.Close()
	client := newModelClient(management.ClientConfig{BaseURL: server.URL, HTTPClient: server.Client()}, management.StackAuth{ApiKey: "key", ManagementToken: "token", Branch: "main"})
	value, err := client.taxonomy(context.Background(), http.MethodGet, "topics", nil)
	require.NoError(t, err)
	assert.Equal(t, "en-us", value.Locale)
	assert.False(t, taxonomyState(value).Description.IsNull())
	_, err = client.taxonomy(context.Background(), http.MethodGet, "missing", nil)
	require.ErrorIs(t, err, errTaxonomyNotFound)
	_, err = client.taxonomy(context.Background(), http.MethodGet, "denied", nil)
	require.ErrorContains(t, err, "HTTP 403")
	assert.NotContains(t, err.Error(), "secret-token")
	_, err = client.taxonomy(context.Background(), http.MethodGet, "wrong", nil)
	require.ErrorContains(t, err, "identity")
	_, err = client.taxonomy(context.Background(), http.MethodGet, "../escape", nil)
	require.ErrorContains(t, err, "endpoint")
	term, err := client.taxonomyTerm(context.Background(), http.MethodGet, "topics", "child", false, nil)
	require.NoError(t, err)
	assert.Equal(t, "parent", taxonomyTermState("topics", term).ParentUID.Value)
	term, err = client.taxonomyTerm(context.Background(), http.MethodPut, "topics", "child", true, map[string]any{"term": map[string]any{"parent_uid": nil, "order": 1}})
	require.NoError(t, err)
	assert.True(t, taxonomyTermState("topics", term).ParentUID.IsNull())
}

func TestTaxonomyImportIDs(t *testing.T) {
	for _, id := range []string{"", "../x", "topics/child/extra", "topics/", "topics/child?force=true", "topics/%2F"} {
		_, err := taxonomyImportParts(id, 2)
		require.Error(t, err, id)
	}
	parts, err := taxonomyImportParts("topics/child", 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"topics", "child"}, parts)
	_, err = taxonomyImportParts("topics", 1)
	require.NoError(t, err)
}
