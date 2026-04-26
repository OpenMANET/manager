package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/openmanet/openmanetd/internal/sysupgrade"
)

// fakeUploadStorer satisfies handlers.SysupgradeUploadStorer with
// configurable behavior. All mutation guarded by mu so the handler
// can be exercised concurrently from a streaming test.
type fakeUploadStorer struct {
	mu       sync.Mutex
	staged   *sysupgrade.StagedImage
	storeErr error
	calls    int
	lastName string
	lastBody []byte
}

func (f *fakeUploadStorer) StoreStagedImage(_ context.Context, src io.Reader, filename string) (*sysupgrade.StagedImage, error) {
	body, _ := io.ReadAll(src)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.lastName = filename
	f.lastBody = body

	if f.storeErr != nil {
		return nil, f.storeErr
	}

	if f.staged != nil {
		return f.staged, nil
	}

	return &sysupgrade.StagedImage{
		Filename:              filename,
		SizeBytes:             int64(len(body)),
		Sha256:                "deadbeef",
		FilenameMatchesTarget: true,
		PreflightOK:           true,
	}, nil
}

func TestSysupgradeUploadHandler_MultipartHappyPath(t *testing.T) {
	storer := &fakeUploadStorer{}
	h := &handlers.SysupgradeUploadHandler{Log: zerolog.Nop(), Manager: storer}

	body, contentType := buildMultipartBody(t, "image", "openmanet-bcm27xx-bcm2711.img.gz", []byte("payload"))

	req := httptest.NewRequest(http.MethodPost, "/api/sysupgrade/upload", body)
	req.Header.Set("Content-Type", contentType)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Filename              string `json:"filename"`
		SizeBytes             int64  `json:"size_bytes"`
		Sha256                string `json:"sha256"`
		FilenameMatchesTarget bool   `json:"filename_matches_target"`
		PreflightOK           bool   `json:"preflight_ok"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "openmanet-bcm27xx-bcm2711.img.gz", resp.Filename)
	assert.Equal(t, int64(7), resp.SizeBytes)
	assert.Equal(t, "deadbeef", resp.Sha256)
	assert.True(t, resp.FilenameMatchesTarget)
	assert.True(t, resp.PreflightOK)

	assert.Equal(t, 1, storer.calls)
	assert.Equal(t, "openmanet-bcm27xx-bcm2711.img.gz", storer.lastName)
	assert.Equal(t, []byte("payload"), storer.lastBody)
}

func TestSysupgradeUploadHandler_RawBodyWithFilenameHeader(t *testing.T) {
	storer := &fakeUploadStorer{}
	h := &handlers.SysupgradeUploadHandler{Log: zerolog.Nop(), Manager: storer}

	req := httptest.NewRequest(http.MethodPost, "/api/sysupgrade/upload", strings.NewReader("rawbytes"))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Filename", "manual.img")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "manual.img", storer.lastName)
	assert.Equal(t, []byte("rawbytes"), storer.lastBody)
}

func TestSysupgradeUploadHandler_RejectsNonPost(t *testing.T) {
	h := &handlers.SysupgradeUploadHandler{Log: zerolog.Nop(), Manager: &fakeUploadStorer{}}

	req := httptest.NewRequest(http.MethodGet, "/api/sysupgrade/upload", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestSysupgradeUploadHandler_RejectsMissingManager(t *testing.T) {
	h := &handlers.SysupgradeUploadHandler{Log: zerolog.Nop(), Manager: nil}

	req := httptest.NewRequest(http.MethodPost, "/api/sysupgrade/upload", strings.NewReader("x"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestSysupgradeUploadHandler_MapsUploadInFlight(t *testing.T) {
	storer := &fakeUploadStorer{storeErr: sysupgrade.ErrUploadInFlight}
	h := &handlers.SysupgradeUploadHandler{Log: zerolog.Nop(), Manager: storer}

	body, contentType := buildMultipartBody(t, "image", "x.img", []byte("a"))

	req := httptest.NewRequest(http.MethodPost, "/api/sysupgrade/upload", body)
	req.Header.Set("Content-Type", contentType)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)

	var errBody struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errBody))
	assert.Contains(t, errBody.Error, "another upload")
}

func TestSysupgradeUploadHandler_MapsTooLarge(t *testing.T) {
	storer := &fakeUploadStorer{storeErr: sysupgrade.ErrUploadTooLarge}
	h := &handlers.SysupgradeUploadHandler{Log: zerolog.Nop(), Manager: storer}

	body, contentType := buildMultipartBody(t, "image", "x.img", []byte("a"))

	req := httptest.NewRequest(http.MethodPost, "/api/sysupgrade/upload", body)
	req.Header.Set("Content-Type", contentType)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestSysupgradeUploadHandler_MapsEmptyAndOther(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"empty", sysupgrade.ErrUploadEmpty, http.StatusBadRequest},
		{"random internal", errors.New("disk full"), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storer := &fakeUploadStorer{storeErr: tc.err}
			h := &handlers.SysupgradeUploadHandler{Log: zerolog.Nop(), Manager: storer}

			body, ct := buildMultipartBody(t, "image", "x.img", []byte("a"))

			req := httptest.NewRequest(http.MethodPost, "/api/sysupgrade/upload", body)
			req.Header.Set("Content-Type", ct)

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			assert.Equal(t, tc.status, w.Code)
		})
	}
}

func TestSysupgradeUploadHandler_RejectsMultipartWithoutFilePart(t *testing.T) {
	storer := &fakeUploadStorer{}
	h := &handlers.SysupgradeUploadHandler{Log: zerolog.Nop(), Manager: storer}

	var buf bytes.Buffer

	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("not-a-file", "still-not-a-file"))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/sysupgrade/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, storer.calls)
}

// buildMultipartBody returns a populated multipart body and its
// Content-Type header for use in handler tests.
func buildMultipartBody(t *testing.T, field, filename string, payload []byte) (*bytes.Buffer, string) {
	t.Helper()

	var buf bytes.Buffer

	mw := multipart.NewWriter(&buf)

	w, err := mw.CreateFormFile(field, filename)
	require.NoError(t, err)

	_, err = w.Write(payload)
	require.NoError(t, err)

	require.NoError(t, mw.Close())

	return &buf, mw.FormDataContentType()
}
