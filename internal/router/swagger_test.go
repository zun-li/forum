package router

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestOpenAPIRoutes(t *testing.T) {
	r := New(nil, nil, nil, nil, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /openapi.yaml returned %d", w.Code)
	}

	type server struct{ URL string }
	var spec struct {
		Servers []server
		Paths   map[string]struct {
			Servers []server
			Fields  map[string]yaml.Node `yaml:",inline"`
		}
	}
	if err := yaml.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Servers) == 0 || spec.Servers[0].URL != "/api/v1" {
		t.Fatalf("unexpected API servers: %v", spec.Servers)
	}

	remaining := map[string]bool{}
	param := regexp.MustCompile(`:(\w+)`)
	for _, route := range r.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/") || route.Path == "/healthz" {
			remaining[route.Method+" "+param.ReplaceAllString(route.Path, "{$1}")] = true
		}
	}
	for path, item := range spec.Paths {
		for _, method := range []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"} {
			node, ok := item.Fields[method]
			if !ok {
				continue
			}
			var operation struct{ Servers []server }
			if err := node.Decode(&operation); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			base := spec.Servers[0].URL
			if len(item.Servers) > 0 {
				base = item.Servers[0].URL
			}
			if len(operation.Servers) > 0 {
				base = operation.Servers[0].URL
			}
			key := strings.ToUpper(method) + " " + strings.TrimSuffix(base, "/") + path
			if !remaining[key] {
				t.Errorf("documented operation has no route: %s", key)
			}
			delete(remaining, key)
		}
	}
	for key := range remaining {
		t.Errorf("registered route is missing from OpenAPI: %s", key)
	}
}

func TestSwaggerUI(t *testing.T) {
	r := New(nil, nil, nil, nil, nil)
	for path, contentType := range map[string]string{
		"/swagger/":                     "text/html",
		"/swagger/swagger-ui.css":       "text/css",
		"/swagger/swagger-ui-bundle.js": "javascript",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), contentType) {
			t.Errorf("GET %s returned %d %s", path, w.Code, w.Header().Get("Content-Type"))
		}
		if path == "/swagger/" && !strings.Contains(w.Body.String(), "/openapi.yaml") {
			t.Error("Swagger UI does not reference /openapi.yaml")
		}
	}
}
