package frontend

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// whisperDir is the directory where whisper model files are stored at runtime.
// It is a package-level variable so tests can override it with t.TempDir().
var whisperDir = "/tmp/whisper" //nolint:gochecknoglobals

// whisperModelFile is the filename of the whisper model binary.
const whisperModelFile = "ggml-tiny.en.bin"

// whisperModelURL is the default CDN URL for the whisper tiny.en model.
const whisperModelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin"

// whisperStatusResponse is the JSON response for GET /api/whisper/status.
type whisperStatusResponse struct {
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
	Progress  int    `json:"progress"`
	Available bool   `json:"available"`
}

// whisperState holds the current whisper model download state.
var whisperState = struct { //nolint:gochecknoglobals
	state    string
	err      string
	progress int
	mu       sync.Mutex
}{state: "idle"}

// whisperModelExists checks whether the whisper model file exists in whisperDir.
func whisperModelExists() bool {
	_, err := os.Stat(filepath.Join(whisperDir, whisperModelFile))

	return err == nil
}

// handleWhisperStatus returns the current whisper availability and download state.
func (s *Server) handleWhisperStatus(w http.ResponseWriter, _ *http.Request) {
	whisperState.mu.Lock()
	resp := whisperStatusResponse{
		Available: whisperModelExists(),
		State:     whisperState.state,
		Progress:  whisperState.progress,
		Error:     whisperState.err,
	}
	whisperState.mu.Unlock()

	// If files exist but state is idle (e.g. server restarted with files
	// still in /tmp from a previous session), report ready.
	if resp.Available && resp.State == "idle" {
		resp.State = "ready"
	}

	s.writeJSON(w, resp)
}

// handleWhisperDownload starts a background download of the whisper model.
func (s *Server) handleWhisperDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErrorStatus(w, http.StatusMethodNotAllowed, "method not allowed")

		return
	}

	whisperState.mu.Lock()
	if whisperState.state == "downloading" {
		whisperState.mu.Unlock()
		s.writeErrorStatus(w, http.StatusConflict, "download already in progress")

		return
	}

	whisperState.state = "downloading"
	whisperState.progress = 0
	whisperState.err = ""
	whisperState.mu.Unlock()

	go s.downloadWhisperModel()

	s.writeJSON(w, map[string]string{"status": "downloading"})
}

// handleWhisperDownloadStatus returns the current download progress.
func (s *Server) handleWhisperDownloadStatus(w http.ResponseWriter, _ *http.Request) {
	whisperState.mu.Lock()
	resp := whisperStatusResponse{
		Available: whisperModelExists(),
		State:     whisperState.state,
		Progress:  whisperState.progress,
		Error:     whisperState.err,
	}
	whisperState.mu.Unlock()

	s.writeJSON(w, resp)
}

// handleWhisperRemove deletes the downloaded whisper model files.
func (s *Server) handleWhisperRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.writeErrorStatus(w, http.StatusMethodNotAllowed, "method not allowed")

		return
	}

	if err := os.RemoveAll(whisperDir); err != nil {
		s.writeErrorStatus(w, http.StatusInternalServerError, fmt.Sprintf("failed to remove whisper files: %v", err))

		return
	}

	whisperState.mu.Lock()
	whisperState.state = "idle"
	whisperState.progress = 0
	whisperState.err = ""
	whisperState.mu.Unlock()

	s.writeJSON(w, map[string]string{"status": "removed"})
}

// downloadWhisperModel downloads the whisper model to whisperDir.
func (s *Server) downloadWhisperModel() {
	setError := func(msg string) {
		s.log.Error().Msg(msg)

		_ = os.RemoveAll(whisperDir)

		whisperState.mu.Lock()
		whisperState.state = "error"
		whisperState.err = msg
		whisperState.mu.Unlock()
	}

	if err := os.MkdirAll(whisperDir, 0o755); err != nil {
		setError(fmt.Sprintf("failed to create whisper directory: %v", err))

		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: 30 * time.Minute}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, whisperModelURL, nil)
	if err != nil {
		setError(fmt.Sprintf("invalid download URL: %v", err))

		return
	}

	resp, err := client.Do(req)
	if err != nil {
		setError(fmt.Sprintf("download failed: %v", err))

		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		setError(fmt.Sprintf("download returned status %d", resp.StatusCode))

		return
	}

	outPath := filepath.Join(whisperDir, whisperModelFile)

	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		setError(fmt.Sprintf("failed to create file: %v", err))

		return
	}
	defer outFile.Close()

	totalSize := resp.ContentLength

	var downloaded int64

	buf := make([]byte, 32*1024) //nolint:mnd

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := outFile.Write(buf[:n]); wErr != nil {
				setError(fmt.Sprintf("write failed: %v", wErr))

				return
			}

			downloaded += int64(n)

			if totalSize > 0 {
				whisperState.mu.Lock()
				whisperState.progress = int(downloaded * 100 / totalSize)
				whisperState.mu.Unlock()
			}
		}

		if readErr == io.EOF {
			break
		}

		if readErr != nil {
			setError(fmt.Sprintf("read failed: %v", readErr))

			return
		}
	}

	s.log.Info().Str("path", outPath).Int64("bytes", downloaded).Msg("whisper model downloaded")

	whisperState.mu.Lock()
	whisperState.state = "ready"
	whisperState.progress = 100
	whisperState.mu.Unlock()
}
