package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var site = fstest.MapFS{
	"index.html":           {Data: []byte("<html>app</html>")},
	"assets/index-abc.js":  {Data: []byte("console.log(1)")},
	"sw.js":                {Data: []byte("sw")},
	"manifest.webmanifest": {Data: []byte("{}")},
	"icon-192.png":         {Data: []byte("png")},
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestServesIndexAndFiles(t *testing.T) {
	h := NewHandler(site)
	if r := get(h, "GET", "/"); r.Code != 200 || !strings.Contains(r.Body.String(), "app") {
		t.Fatalf("/: %d %q", r.Code, r.Body.String())
	}
	r := get(h, "GET", "/assets/index-abc.js")
	if r.Code != 200 || !strings.Contains(r.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %v", r.Code, r.Header())
	}
	if r := get(h, "GET", "/sw.js"); r.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("sw.js must be revalidated: %v", r.Header())
	}
	if r := get(h, "GET", "/index.html"); r.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index.html must be revalidated: %v", r.Header())
	}
	if ct := get(h, "GET", "/manifest.webmanifest").Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/manifest+json") {
		t.Fatalf("manifest content type %q", ct)
	}
}

func TestUnknownPathsFallBackToIndexButNotApiOrFiles(t *testing.T) {
	h := NewHandler(site)
	if r := get(h, "GET", "/plan"); r.Code != 200 || !strings.Contains(r.Body.String(), "app") {
		t.Fatalf("/plan: %d", r.Code)
	}
	for _, p := range []string{"/api/nothing", "/missing.js", "/assets/gone.css"} {
		if r := get(h, "GET", p); r.Code != 404 {
			t.Errorf("%s: %d, want 404", p, r.Code)
		}
	}
}

func TestOnlyReadMethods(t *testing.T) {
	h := NewHandler(site)
	if r := get(h, "POST", "/"); r.Code != 405 {
		t.Fatalf("POST: %d", r.Code)
	}
	if r := get(h, "HEAD", "/"); r.Code != 200 {
		t.Fatalf("HEAD: %d", r.Code)
	}
}

func TestPathTraversalStaysInsideTheSite(t *testing.T) {
	h := NewHandler(site)
	if r := get(h, "GET", "/../../etc/passwd"); r.Code == 200 && !strings.Contains(r.Body.String(), "app") {
		t.Fatalf("leaked: %q", r.Body.String())
	}
}

func TestMissingBuildAnswers503(t *testing.T) {
	r := get(NewHandler(fstest.MapFS{".gitkeep": {}}), "GET", "/")
	if r.Code != http.StatusServiceUnavailable || !strings.Contains(r.Body.String(), "npm run build") {
		t.Fatalf("%d %q", r.Code, r.Body.String())
	}
}
