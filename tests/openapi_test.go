package tests

import (
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The OpenAPI document is the contract an outside client reads. It is only worth
// anything if it says exactly what the server does, so this test compares the two
// sets of operations and fails on either kind of drift: an endpoint that nobody
// wrote down, and a line in the document that no longer exists.
func TestOpenAPIMatchesTheRouter(t *testing.T) {
	spec := loadSpec(t)
	a := newApp(t)

	documented := map[string]bool{}
	for path, item := range spec.Paths {
		for method := range item {
			if isMethod(method) {
				documented[strings.ToUpper(method)+" "+path] = true
			}
		}
	}

	mounted := map[string]bool{}
	for _, r := range a.Router.Routes() {
		if !strings.HasPrefix(r.Path, "/api/v1") {
			continue
		}
		mounted[r.Method+" "+openapiPath(r.Path)] = true
	}

	var missing, extra []string
	for route := range mounted {
		if !documented[route] {
			missing = append(missing, route)
		}
	}
	for route := range documented {
		if !mounted[route] {
			extra = append(extra, route)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("%d endpoint(s) the server serves and api/openapi.yaml does not describe:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("%d endpoint(s) api/openapi.yaml describes and the server does not serve:\n  %s",
			len(extra), strings.Join(extra, "\n  "))
	}
}

// Every operation says what it is and which part of the app it belongs to, and
// every $ref points at something that exists: a reader should never meet a blank.
func TestOpenAPIIsWellFormed(t *testing.T) {
	spec := loadSpec(t)
	if spec.OpenAPI == "" || spec.Info.Title == "" || spec.Info.Version == "" {
		t.Fatal("the document needs openapi, info.title and info.version")
	}
	tags := map[string]bool{}
	for _, tag := range spec.Tags {
		tags[tag.Name] = true
	}
	for path, item := range spec.Paths {
		for method, node := range item {
			if !isMethod(method) {
				continue
			}
			var op operation
			if err := node.Decode(&op); err != nil {
				t.Errorf("%s %s does not read as an operation: %v", strings.ToUpper(method), path, err)
				continue
			}
			where := strings.ToUpper(method) + " " + path
			if op.Summary == "" {
				t.Errorf("%s has no summary", where)
			}
			if len(op.Tags) == 0 {
				t.Errorf("%s belongs to no tag", where)
			}
			for _, tag := range op.Tags {
				if !tags[tag] {
					t.Errorf("%s uses tag %q, which the document does not declare", where, tag)
				}
			}
			if len(op.Responses) == 0 {
				t.Errorf("%s describes no response", where)
			}
		}
	}

	// $ref targets: components/<kind>/<name> must be there.
	var walk func(node any)
	raw := loadRaw(t)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, value := range v {
				if key == "$ref" {
					ref, _ := value.(string)
					if !strings.HasPrefix(ref, "#/components/") {
						t.Errorf("only local component refs are used; got %q", ref)
						continue
					}
					parts := strings.Split(strings.TrimPrefix(ref, "#/components/"), "/")
					if len(parts) != 2 || !componentExists(raw, parts[0], parts[1]) {
						t.Errorf("$ref %q points at nothing", ref)
					}
					continue
				}
				walk(value)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(raw)
}

// The server hands out the same document it was built from (GET /openapi.yaml).
func TestOpenAPIIsServed(t *testing.T) {
	a := newApp(t)
	r := a.get("/api/v1/openapi.yaml", "").expect(http.StatusOK)
	if ct := r.Hdr.Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("content type %q", ct)
	}
	onDisk, err := os.ReadFile("../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Body) != string(onDisk) {
		t.Error("the served document and api/openapi.yaml have drifted apart")
	}
}

type specDoc struct {
	OpenAPI string `yaml:"openapi"`
	Info    struct {
		Title   string `yaml:"title"`
		Version string `yaml:"version"`
	} `yaml:"info"`
	Tags []struct {
		Name string `yaml:"name"`
	} `yaml:"tags"`
	// A path item holds operations keyed by method, beside keys like "parameters"
	// that are not operations at all — so it is decoded loosely and the methods are
	// read out of it.
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

type operation struct {
	Summary   string         `yaml:"summary"`
	Tags      []string       `yaml:"tags"`
	Responses map[string]any `yaml:"responses"`
}

func isMethod(key string) bool {
	switch strings.ToLower(key) {
	case "get", "post", "put", "patch", "delete":
		return true
	}
	return false
}

func loadSpec(t *testing.T) specDoc {
	t.Helper()
	raw, err := os.ReadFile("../api/openapi.yaml")
	if err != nil {
		t.Fatalf("read the spec: %v", err)
	}
	var out specDoc
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the spec is not valid YAML: %v", err)
	}
	return out
}

func loadRaw(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func componentExists(doc map[string]any, kind, name string) bool {
	components, _ := doc["components"].(map[string]any)
	group, _ := components[kind].(map[string]any)
	_, ok := group[name]
	return ok
}

// openapiPath turns gin's ":id" into OpenAPI's "{id}".
func openapiPath(p string) string {
	parts := strings.Split(strings.TrimPrefix(p, "/api/v1"), "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	out := strings.Join(parts, "/")
	if out == "" {
		return "/"
	}
	return out
}
