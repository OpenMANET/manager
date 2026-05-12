package frontend

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setTestWhisperDir overrides the package-level whisperDir for test isolation
// and resets it (along with whisperState) when the test completes.
func setTestWhisperDir(t *testing.T, dir string) {
	t.Helper()

	origDir := whisperDir
	whisperDir = dir

	t.Cleanup(func() {
		whisperDir = origDir

		whisperState.mu.Lock()
		whisperState.state = "idle"
		whisperState.progress = 0
		whisperState.err = ""
		whisperState.mu.Unlock()
	})
}

// createTestModel creates a fake whisper model file in dir.
func createTestModel(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, whisperModelFile), []byte("fake-model-data"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
}

// ---------------------------------------------------------------------------
// handleWhisperStatus
// ---------------------------------------------------------------------------

func TestHandleWhisperStatus_Idle(t *testing.T) {
	srv := newTestServer()
	dir := t.TempDir()
	setTestWhisperDir(t, dir)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/whisper/status", nil)
	srv.handleWhisperStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp whisperStatusResponse
	decodeJSON(t, w.Body, &resp)

	if resp.Available {
		t.Error("available = true, want false")
	}

	if resp.State != "idle" {
		t.Errorf("state = %q, want idle", resp.State)
	}
}

func TestHandleWhisperStatus_Available(t *testing.T) {
	srv := newTestServer()
	dir := t.TempDir()
	setTestWhisperDir(t, dir)
	createTestModel(t, dir)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/whisper/status", nil)
	srv.handleWhisperStatus(w, r)

	var resp whisperStatusResponse
	decodeJSON(t, w.Body, &resp)

	if !resp.Available {
		t.Error("available = false, want true")
	}

	if resp.State != "ready" {
		t.Errorf("state = %q, want ready (auto-promoted from idle)", resp.State)
	}
}

// ---------------------------------------------------------------------------
// handleWhisperDownload
// ---------------------------------------------------------------------------

func TestHandleWhisperDownload_MethodNotAllowed(t *testing.T) {
	srv := newTestServer()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/whisper/download", nil)
	srv.handleWhisperDownload(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestHandleWhisperDownload_ConflictWhenDownloading(t *testing.T) {
	srv := newTestServer()
	dir := t.TempDir()
	setTestWhisperDir(t, dir)

	// Simulate an in-progress download.
	whisperState.mu.Lock()
	whisperState.state = "downloading"
	whisperState.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/whisper/download", nil)
	srv.handleWhisperDownload(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

func TestHandleWhisperDownload_StartsDownload(t *testing.T) {
	srv := newTestServer()
	dir := t.TempDir()
	setTestWhisperDir(t, dir)

	// Create a mock HTTP server that serves a tiny fake model.
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer mockServer.Close()

	// We can't easily override whisperModelURL for this test without
	// refactoring, so just verify the handler accepts the POST and
	// transitions state to "downloading".
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/whisper/download", nil)
	srv.handleWhisperDownload(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]string
	decodeJSON(t, w.Body, &resp)

	if resp["status"] != "downloading" {
		t.Errorf("status = %q, want downloading", resp["status"])
	}
}

// ---------------------------------------------------------------------------
// handleWhisperDownloadStatus
// ---------------------------------------------------------------------------

func TestHandleWhisperDownloadStatus_ReturnsState(t *testing.T) {
	srv := newTestServer()
	dir := t.TempDir()
	setTestWhisperDir(t, dir)

	whisperState.mu.Lock()
	whisperState.state = "downloading"
	whisperState.progress = 42
	whisperState.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/whisper/download/status", nil)
	srv.handleWhisperDownloadStatus(w, r)

	var resp whisperStatusResponse
	decodeJSON(t, w.Body, &resp)

	if resp.State != "downloading" {
		t.Errorf("state = %q, want downloading", resp.State)
	}

	if resp.Progress != 42 {
		t.Errorf("progress = %d, want 42", resp.Progress)
	}
}

// ---------------------------------------------------------------------------
// handleWhisperRemove
// ---------------------------------------------------------------------------

func TestHandleWhisperRemove_MethodNotAllowed(t *testing.T) {
	srv := newTestServer()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/whisper/remove", nil)
	srv.handleWhisperRemove(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestHandleWhisperRemove_DeletesFiles(t *testing.T) {
	srv := newTestServer()
	dir := t.TempDir()
	setTestWhisperDir(t, dir)
	createTestModel(t, dir)

	// Verify file exists before remove.
	if !whisperModelExists() {
		t.Fatal("model should exist before remove")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/whisper/remove", nil)
	srv.handleWhisperRemove(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Verify file is gone.
	if whisperModelExists() {
		t.Error("model still exists after remove")
	}

	// Verify state reset.
	whisperState.mu.Lock()
	st := whisperState.state
	whisperState.mu.Unlock()

	if st != "idle" {
		t.Errorf("state = %q, want idle", st)
	}
}

// ---------------------------------------------------------------------------
// Whisper file serving via SPA handler
// ---------------------------------------------------------------------------

func TestWhisperFileServing_FromTmpDir(t *testing.T) {
	dir := t.TempDir()
	setTestWhisperDir(t, dir)
	createTestModel(t, dir)

	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/whisper/" + whisperModelFile)
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "fake-model-data" {
		t.Errorf("body = %q, want fake-model-data", body)
	}
}

func TestWhisperFileServing_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	setTestWhisperDir(t, dir)

	// Create a file outside whisperDir that an attacker might try to read.
	secretPath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	// Attempt path traversal.
	resp, err := http.Get(ts.URL + "/whisper/../secret.txt")
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if containsStr(string(body), "secret") {
		t.Error("path traversal succeeded — served file outside whisperDir")
	}
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

func TestHandler_RegistersWhisperRoutes(t *testing.T) {
	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	routes := []string{
		"/api/whisper/status",
		"/api/whisper/download/status",
	}

	for _, path := range routes {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatalf("GET %s error: %v", path, err)
			}

			resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				t.Errorf("route %s returned 404", path)
			}

			// Verify JSON response.
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("content-type = %q, want application/json", ct)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// whisperModelExists
// ---------------------------------------------------------------------------

func TestWhisperModelExists_False(t *testing.T) {
	dir := t.TempDir()
	setTestWhisperDir(t, dir)

	if whisperModelExists() {
		t.Error("expected false when model file does not exist")
	}
}

func TestWhisperModelExists_True(t *testing.T) {
	dir := t.TempDir()
	setTestWhisperDir(t, dir)
	createTestModel(t, dir)

	if !whisperModelExists() {
		t.Error("expected true when model file exists")
	}
}

// ---------------------------------------------------------------------------
// Download error state
// ---------------------------------------------------------------------------

func TestHandleWhisperDownloadStatus_ErrorState(t *testing.T) {
	srv := newTestServer()
	dir := t.TempDir()
	setTestWhisperDir(t, dir)

	whisperState.mu.Lock()
	whisperState.state = "error"
	whisperState.err = "network timeout"
	whisperState.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/whisper/download/status", nil)
	srv.handleWhisperDownloadStatus(w, r)

	var resp whisperStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}

	if resp.State != "error" {
		t.Errorf("state = %q, want error", resp.State)
	}

	if resp.Error != "network timeout" {
		t.Errorf("error = %q, want 'network timeout'", resp.Error)
	}
}
