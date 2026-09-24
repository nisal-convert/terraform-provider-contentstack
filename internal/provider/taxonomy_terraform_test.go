package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTerraformTaxonomyLifecycle(t *testing.T) {
	cli, providerDir := os.Getenv("CONTENTSTACK_TEST_TERRAFORM"), os.Getenv("CONTENTSTACK_TEST_PROVIDER_DIR")
	if cli == "" || providerDir == "" {
		t.Skip("set CONTENTSTACK_TEST_TERRAFORM and CONTENTSTACK_TEST_PROVIDER_DIR for local integration tests")
	}
	dir := t.TempDir()
	var mutex sync.Mutex
	var taxonomy map[string]any
	terms := map[string]map[string]any{}
	writes, moves, references := 0, 0, 0
	failRename := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		assert.Equal(t, "mock-key", r.Header.Get("api_key"))
		assert.Equal(t, "mock-token", r.Header.Get("authorization"))
		assert.Equal(t, "main", r.Header.Get("branch"))
		assert.Empty(t, r.URL.Query().Get("force"))
		assert.Empty(t, r.URL.Query().Get("locale"))
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 2 || parts[0] != "v3" || parts[1] != "taxonomies" {
			http.Error(w, "bad path", 400)
			return
		}
		var body map[string]map[string]any
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		}
		if len(parts) <= 3 {
			switch r.Method {
			case http.MethodPost:
				if taxonomy != nil {
					http.Error(w, "exists", 409)
					return
				}
				taxonomy = body["taxonomy"]
				taxonomy["locale"] = "en-us"
				writes++
				if taxonomy["description"] == nil {
					taxonomy["description"] = ""
				}
			case http.MethodPut:
				for k, v := range body["taxonomy"] {
					taxonomy[k] = v
				}
				writes++
			case http.MethodDelete:
				require.Empty(t, terms)
				taxonomy = nil
				writes++
				w.WriteHeader(204)
				return
			}
			if taxonomy == nil {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"taxonomy": taxonomy})
			return
		}
		if parts[3] != "terms" {
			http.Error(w, "bad path", 400)
			return
		}
		if len(parts) == 4 && r.Method == http.MethodGet {
			list := []map[string]any{}
			for _, term := range terms {
				list = append(list, term)
			}
			json.NewEncoder(w).Encode(map[string]any{"terms": list})
			return
		}
		uid := ""
		if len(parts) > 4 {
			uid = parts[4]
		} else {
			uid = body["term"]["uid"].(string)
		}
		switch r.Method {
		case http.MethodPost:
			if terms[uid] != nil {
				http.Error(w, "exists", 409)
				return
			}
			value := body["term"]
			if p := value["parent_uid"]; p != nil && terms[p.(string)] == nil {
				http.Error(w, "missing parent", 422)
				return
			}
			assert.Equal(t, float64(1), value["order"])
			delete(value, "order")
			value["locale"] = "en-us"
			terms[uid] = value
			writes++
		case http.MethodPut:
			if len(parts) == 6 {
				assert.Equal(t, "move", parts[5])
				moves++
				terms[uid]["parent_uid"] = body["term"]["parent_uid"]
			} else {
				if failRename {
					http.Error(w, "private failure details", 422)
					return
				}
				require.Len(t, body["term"], 1)
				terms[uid]["name"] = body["term"]["name"]
			}
			writes++
		case http.MethodDelete:
			delete(terms, uid)
			writes++
			w.WriteHeader(204)
			return
		}
		if terms[uid] == nil {
			http.NotFound(w, r)
			return
		}
		result := map[string]any{}
		for k, v := range terms[uid] {
			result[k] = v
		}
		result["children_count"] = 0
		result["referenced_entries_count"] = 0
		for _, term := range terms {
			if term["parent_uid"] == uid {
				result["children_count"] = result["children_count"].(int) + 1
			}
		}
		if uid == "child" {
			result["referenced_entries_count"] = references
		}
		json.NewEncoder(w).Encode(map[string]any{"term": result})
	}))
	defer server.Close()
	configPath := filepath.Join(dir, "terraform.rc")
	require.NoError(t, os.WriteFile(configPath, []byte(fmt.Sprintf(`provider_installation {
 dev_overrides { "nisal-convert/contentstack" = %q }
 direct {}
}
disable_checkpoint = true
`, providerDir)), 0600))
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TF_") {
			env = append(env, value)
		}
	}
	env = append(env, "TF_CLI_CONFIG_FILE="+configPath, "TF_IN_AUTOMATION=1", "CHECKPOINT_DISABLE=1")
	run := func(success bool, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, args...)
		cmd.Dir = dir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if success {
			require.NoError(t, err, "terraform %v:\n%s", args, out)
		} else {
			require.Error(t, err, "terraform %v should fail:\n%s", args, out)
		}
		return string(out)
	}
	writeConfig := func(moveParent, childRoot, updated bool) {
		t.Helper()
		parent := "null"
		if moveParent {
			parent = "contentstack_taxonomy_term.other.uid"
		}
		childParent := "contentstack_taxonomy_term.parent.uid"
		if childRoot {
			childParent = "null"
		}
		name, description, childName := "Topics", "Initial description", "Child"
		if updated {
			name, description, childName = "Updated topics", "", "Updated child"
		}
		config := fmt.Sprintf(`terraform {
 required_providers { contentstack = { source = "nisal-convert/contentstack" } }
}
provider "contentstack" {
 base_url = %q
 api_key = "mock-key"
 management_token = "mock-token"
 branch = "main"
}
resource "contentstack_taxonomy" "topics" {
 uid = "topics"
 name = %q
 description = %q
}
resource "contentstack_taxonomy_term" "other" {
 taxonomy_uid = contentstack_taxonomy.topics.uid
 uid = "other"
 name = "Other"
}
resource "contentstack_taxonomy_term" "parent" {
 taxonomy_uid = contentstack_taxonomy.topics.uid
 uid = "parent"
 name = "Parent"
 parent_uid = %s
}
resource "contentstack_taxonomy_term" "child" {
 taxonomy_uid = contentstack_taxonomy.topics.uid
 uid = "child"
 name = %q
 parent_uid = %s
}
`, server.URL, name, description, parent, childName, childParent)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.tf"), []byte(config), 0600))
	}
	writeConfig(false, false, false)
	run(true, "apply", "-auto-approve", "-input=false", "-no-color")
	run(true, "plan", "-detailed-exitcode", "-input=false", "-no-color")
	for _, pair := range [][2]string{{"contentstack_taxonomy.topics", "topics"}, {"contentstack_taxonomy_term.parent", "topics/parent"}, {"contentstack_taxonomy_term.child", "topics/child"}, {"contentstack_taxonomy_term.other", "topics/other"}} {
		run(true, "state", "rm", pair[0])
		run(true, "import", "-input=false", "-no-color", pair[0], pair[1])
	}
	run(true, "plan", "-detailed-exitcode", "-input=false", "-no-color")
	mutex.Lock()
	assert.Equal(t, 4, writes)
	mutex.Unlock()
	writeConfig(true, false, false)
	require.Contains(t, run(false, "apply", "-auto-approve", "-input=false", "-no-color"), "Term move refused")
	mutex.Lock()
	assert.Equal(t, 0, moves)
	mutex.Unlock()
	writeConfig(false, false, false)
	run(true, "plan", "-detailed-exitcode", "-input=false", "-no-color")
	writeConfig(false, true, true)
	mutex.Lock()
	failRename = true
	mutex.Unlock()
	require.Contains(t, run(false, "apply", "-auto-approve", "-input=false", "-no-color"), "HTTP 422")
	mutex.Lock()
	failRename = false
	require.Nil(t, terms["child"]["parent_uid"])
	assert.Equal(t, 1, moves)
	mutex.Unlock()
	run(true, "apply", "-auto-approve", "-input=false", "-no-color")
	run(true, "plan", "-detailed-exitcode", "-input=false", "-no-color")
	mutex.Lock()
	assert.Equal(t, 1, moves)
	assert.Equal(t, "", taxonomy["description"])
	terms["child"]["name"] = "Remote drift"
	mutex.Unlock()
	run(false, "plan", "-detailed-exitcode", "-input=false", "-no-color")
	run(true, "apply", "-auto-approve", "-input=false", "-no-color")
	mutex.Lock()
	references = 1
	mutex.Unlock()
	require.Contains(t, run(false, "destroy", "-target=contentstack_taxonomy_term.child", "-auto-approve", "-input=false", "-no-color"), "Term deletion refused")
	mutex.Lock()
	require.NotNil(t, terms["child"])
	references = 0
	terms["foreign"] = map[string]any{"uid": "foreign", "name": "Unmanaged", "parent_uid": nil}
	mutex.Unlock()
	require.Contains(t, run(false, "destroy", "-auto-approve", "-input=false", "-no-color"), "Taxonomy deletion refused")
	mutex.Lock()
	require.NotNil(t, taxonomy)
	require.NotNil(t, terms["foreign"])
	delete(terms, "foreign")
	mutex.Unlock()
	run(true, "destroy", "-auto-approve", "-input=false", "-no-color")
	mutex.Lock()
	assert.Nil(t, taxonomy)
	assert.Empty(t, terms)
	mutex.Unlock()
}
