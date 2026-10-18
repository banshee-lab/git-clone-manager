package webui

import (
	"io/fs"
	"net/http/httptest"
	"testing"

	"testing/fstest"
)

func testAssets() fs.FS {
	return fstest.MapFS{
		"index.html":            {Data: []byte("<html>app</html>")},
		"about.html":            {Data: []byte("about")},
		"_app/immutable/app.js": {Data: []byte("bundle")},
	}
}

func TestHandlerServesEmbeddedFile(t *testing.T) {
	handler := NewHandler(testAssets())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/about.html", nil))

	if recorder.Code != 200 || recorder.Body.String() != "about" {
		t.Fatalf("got status %d and body %q", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerFallsBackToIndex(t *testing.T) {
	handler := NewHandler(testAssets())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/repositories/123", nil))

	if recorder.Code != 200 || recorder.Body.String() != "<html>app</html>" {
		t.Fatalf("got status %d and body %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
}

func TestHandlerReturnsJSONNotFoundForReservedPaths(t *testing.T) {
	handler := NewHandler(testAssets())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/unknown", nil))

	if recorder.Code != 404 {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Body.String(); got != "{\"error\":{\"code\":\"not_found\",\"message\":\"route not found\"}}\n" {
		t.Fatalf("error body = %q", got)
	}
}

func TestHandlerSetsImmutableAssetCache(t *testing.T) {
	handler := NewHandler(testAssets())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/_app/immutable/app.js", nil))

	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
}
