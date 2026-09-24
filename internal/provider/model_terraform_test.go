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

func TestTerraformModelLifecycle(t *testing.T) {
	cli := os.Getenv("CONTENTSTACK_TEST_TERRAFORM")
	providerDir := os.Getenv("CONTENTSTACK_TEST_PROVIDER_DIR")
	if cli == "" || providerDir == "" {
		t.Skip("set CONTENTSTACK_TEST_TERRAFORM and CONTENTSTACK_TEST_PROVIDER_DIR to run local Terraform integration tests")
	}
	dir := t.TempDir()
	var mutex sync.Mutex
	models := map[string]map[string]json.RawMessage{}
	writes := []map[string]json.RawMessage{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		assert.Equal(t, "mock-key", r.Header.Get("api_key"))
		assert.Equal(t, "mock-token", r.Header.Get("authorization"))
		assert.Equal(t, "main", r.Header.Get("branch"))
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 2 || parts[0] != "v3" || (parts[1] != "content_types" && parts[1] != "global_fields") {
			http.Error(w, "unexpected path", http.StatusBadRequest)
			return
		}
		kind := strings.TrimSuffix(parts[1], "s")
		key := r.URL.Path
		switch r.Method {
		case http.MethodPost, http.MethodPut:
			var body map[string]map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			model := body[kind]
			var uid string
			if err := json.Unmarshal(model["uid"], &uid); err != nil || uid == "" {
				http.Error(w, "missing UID", http.StatusBadRequest)
				return
			}
			key = "/v3/" + parts[1] + "/" + uid
			writes = append(writes, model)
			stored := map[string]json.RawMessage{}
			for name, value := range model {
				if name == "field_rules" && string(value) == "[]" {
					continue
				}
				var decoded any
				decoder := json.NewDecoder(strings.NewReader(string(value)))
				decoder.UseNumber()
				if err := decoder.Decode(&decoded); err != nil {
					http.Error(w, "invalid model attribute", http.StatusBadRequest)
					return
				}
				stored[name], _ = json.Marshal(decoded)
			}
			if _, ok := stored["maintain_revisions"]; !ok {
				stored["maintain_revisions"] = json.RawMessage("true")
			}
			if _, ok := stored["description"]; !ok {
				stored["description"] = json.RawMessage(`""`)
			}
			if kind == "content_type" {
				if _, ok := stored["options"]; !ok {
					stored["options"] = json.RawMessage(`{"singleton":false,"is_page":false}`)
				}
			}
			models[key] = stored
		case http.MethodGet:
			if models[key] == nil {
				http.NotFound(w, r)
				return
			}
		case http.MethodDelete:
			delete(models, key)
			w.WriteHeader(http.StatusNoContent)
			return
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{kind: models[key]})
	}))
	defer server.Close()

	cliConfig := filepath.Join(dir, "terraform.rc")
	require.NoError(t, os.WriteFile(cliConfig, []byte(fmt.Sprintf(`provider_installation {
  dev_overrides {
    "nisal-convert/contentstack" = %q
  }
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
	env = append(env, "TF_CLI_CONFIG_FILE="+cliConfig, "TF_IN_AUTOMATION=1", "CHECKPOINT_DISABLE=1")
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, args...)
		cmd.Dir, cmd.Env = dir, env
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "terraform %v:\n%s", args, output)
		return string(output)
	}
	writeConfig := func(updated, omitSettings bool) {
		t.Helper()
		options := `{"title":"title","singleton":true,"is_page":false,"url_prefix":false,"sub_title":[],"future_option":{"number":9007199254740993}}`
		rules := `[{"conditions":[{"operand_field":"title","operator":"equals","value":"Example"}],"actions":[{"action":"show","target_field":"body"}]}]`
		revisions := true
		if updated {
			options = `{"title":"title","singleton":false,"is_page":true,"url_prefix":"/example","url_pattern":"/:title","sub_title":[],"future_option":{"number":9007199254740993}}`
			rules, revisions = "[]", false
		}
		settings, contentOptions := "", ""
		if !omitSettings {
			settings = fmt.Sprintf("field_rules = jsonencode(jsondecode(%q))\nmaintain_revisions = %t\n", rules, revisions)
			contentOptions = fmt.Sprintf("options = jsonencode(jsondecode(%q))\n", options)
		}
		config := fmt.Sprintf(`terraform {
  required_providers {
    contentstack = { source = "nisal-convert/contentstack" }
  }
}
provider "contentstack" {
  base_url = %q
  api_key = "mock-key"
  management_token = "mock-token"
  branch = "main"
}
resource "contentstack_content_type" "example" {
  uid = "example"
  title = "Example"
  schema = jsonencode([{uid="title",data_type="text"},{uid="body",data_type="text"}])
  %s
  %s
}
resource "contentstack_global_field" "example" {
  uid = "example"
  title = "Example"
  schema = jsonencode([{uid="title",data_type="text"},{uid="body",data_type="text"}])
  %s
}
`, server.URL, contentOptions, settings, settings)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.tf"), []byte(config), 0600))
	}

	writeConfig(false, false)
	run("apply", "-auto-approve", "-input=false", "-no-color")
	run("plan", "-detailed-exitcode", "-input=false", "-no-color")
	for _, address := range []string{"contentstack_content_type.example", "contentstack_global_field.example"} {
		run("state", "rm", address)
		run("import", "-input=false", "-no-color", address, "example")
	}
	run("plan", "-detailed-exitcode", "-input=false", "-no-color")
	writeConfig(true, false)
	run("apply", "-auto-approve", "-input=false", "-no-color")
	run("plan", "-detailed-exitcode", "-input=false", "-no-color")
	mutex.Lock()
	assert.Len(t, writes, 4)
	for i, model := range writes {
		if i < 2 {
			assert.Equal(t, "true", string(model["maintain_revisions"]))
			assert.NotEqual(t, "[]", string(model["field_rules"]))
			continue
		}
		assert.Equal(t, "[]", string(model["field_rules"]))
		assert.Equal(t, "false", string(model["maintain_revisions"]))
	}
	mutex.Unlock()
	run("destroy", "-auto-approve", "-input=false", "-no-color")
	writeConfig(false, true)
	run("apply", "-auto-approve", "-input=false", "-no-color")
	run("plan", "-detailed-exitcode", "-input=false", "-no-color")
	run("destroy", "-auto-approve", "-input=false", "-no-color")
	mutex.Lock()
	assert.Empty(t, models)
	mutex.Unlock()
}
