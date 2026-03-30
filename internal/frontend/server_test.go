package frontend

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/util/logger"
	ws "github.com/openmanet/openmanetd/internal/websocket"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestServer(opts ...func(*Server)) *Server {
	hub := ws.NewHub(nil)
	go hub.Run()

	cfg := config.NewWithoutWatch(nil)
	staticFS := fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte("<html><body>SPA</body></html>")},
		"assets/app.js":    &fstest.MapFile{Data: []byte("console.log('app')")},
		"assets/style.css": &fstest.MapFile{Data: []byte("body{}")},
		"pcm-worklet.js":   &fstest.MapFile{Data: []byte("// worklet")},
	}

	s := &Server{
		log:      logger.GetLogger("frontend.test"),
		staticFS: staticFS,
		hub:      hub,
		cfg:      cfg,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func decodeJSON(t *testing.T, body io.Reader, v any) {
	t.Helper()

	if err := json.NewDecoder(body).Decode(v); err != nil {
		t.Fatalf("json decode: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helper method tests
// ---------------------------------------------------------------------------

func TestWriteJSON(t *testing.T) {
	srv := newTestServer()

	w := httptest.NewRecorder()
	srv.writeJSON(w, map[string]string{"key": "value"})

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var result map[string]string
	decodeJSON(t, w.Body, &result)

	if result["key"] != "value" {
		t.Errorf("key = %q, want %q", result["key"], "value")
	}
}

func TestWriteError(t *testing.T) {
	srv := newTestServer()

	w := httptest.NewRecorder()
	srv.writeError(w, "something failed")

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var result errorResponse
	decodeJSON(t, w.Body, &result)

	if result.Error != "something failed" {
		t.Errorf("error = %q, want %q", result.Error, "something failed")
	}
}

func TestWriteErrorStatus(t *testing.T) {
	srv := newTestServer()

	w := httptest.NewRecorder()
	srv.writeErrorStatus(w, http.StatusNotFound, "not found")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	var result errorResponse
	decodeJSON(t, w.Body, &result)

	if result.Error != "not found" {
		t.Errorf("error = %q, want %q", result.Error, "not found")
	}
}

// ---------------------------------------------------------------------------
// Middleware tests
// ---------------------------------------------------------------------------

func TestCoiMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := coiMiddleware(inner)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(w, r)

	if v := w.Header().Get("Cross-Origin-Opener-Policy"); v != "same-origin" {
		t.Errorf("COOP = %q, want same-origin", v)
	}

	if v := w.Header().Get("Cross-Origin-Embedder-Policy"); v != "require-corp" {
		t.Errorf("COEP = %q, want require-corp", v)
	}

	if v := w.Header().Get("Permissions-Policy"); v != "microphone=*, speaker-selection=*" {
		t.Errorf("Permissions-Policy = %q, want microphone=*, speaker-selection=*", v)
	}
}

// ---------------------------------------------------------------------------
// SPA / static file server tests
// ---------------------------------------------------------------------------

func TestSPAHandler_ServesIndexAtRoot(t *testing.T) {
	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !containsStr(string(body), "SPA") {
		t.Errorf("body does not contain SPA: %s", body)
	}
}

func TestSPAHandler_ServesStaticAsset(t *testing.T) {
	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/assets/app.js")
	if err != nil {
		t.Fatalf("GET /assets/app.js error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !containsStr(string(body), "console.log") {
		t.Errorf("body does not contain expected JS: %s", body)
	}
}

func TestSPAHandler_FallsBackToIndex(t *testing.T) {
	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	// A client-side route that doesn't map to a static file should return index.html.
	resp, err := http.Get(ts.URL + "/settings")
	if err != nil {
		t.Fatalf("GET /settings error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !containsStr(string(body), "SPA") {
		t.Errorf("expected SPA fallback, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Route registration test
// ---------------------------------------------------------------------------

func TestHandler_RegistersExpectedRoutes(t *testing.T) {
	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	routes := []struct {
		path string
	}{
		{"/"},
		{"/settings"},
		{"/api/system/info"},
		{"/api/system/processes"},
		{"/api/network/interfaces"},
		{"/api/settings/config"},
	}

	for _, tc := range routes {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s error: %v", tc.path, err)
			}

			resp.Body.Close()
			// Just verify the route is registered (we get a response, not 404).
			if resp.StatusCode == http.StatusNotFound {
				t.Errorf("route %s returned 404, expected it to be registered", tc.path)
			}
		})
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
