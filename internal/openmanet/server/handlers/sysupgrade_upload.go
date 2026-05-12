package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/sysupgrade"
)

// SysupgradeUploadStorer is the slice of SysupgradeManager required to
// stream an uploaded image. Defined here as a narrow consumer-side
// interface so the upload handler can be tested against a hand-written
// fake without dragging in the rest of the manager surface.
type SysupgradeUploadStorer interface {
	StoreStagedImage(ctx context.Context, src io.Reader, filename string) (*sysupgrade.StagedImage, error)
}

// SysupgradeUploadHandler serves POST /api/sysupgrade/upload. The body
// may be either multipart/form-data (browser-friendly, with the file in
// any single file part) or a raw octet-stream (curl-friendly, with the
// filename supplied via the X-Filename header or fallback).
type SysupgradeUploadHandler struct {
	Log     zerolog.Logger
	Manager SysupgradeUploadStorer
}

// uploadResponse is the success JSON returned to the browser. Mirrors
// the StagedImage proto so the frontend can normalize XHR-upload and
// GetStagedImage RPC results uniformly.
type uploadResponse struct {
	UploadedAt       time.Time `json:"uploaded_at"`
	Filename         string    `json:"filename"`
	Sha256           string    `json:"sha256"`
	CompatVersion    string    `json:"compat_version,omitempty"`
	CompatMessage    string    `json:"compat_message,omitempty"`
	DeviceCompat     string    `json:"device_compat,omitempty"`
	PreflightError   string    `json:"preflight_error,omitempty"`
	SupportedDevices []string  `json:"supported_devices,omitempty"`
	SizeBytes        int64     `json:"size_bytes"`
	MetadataPresent  bool      `json:"metadata_present"`
	ImageCompatible  bool      `json:"image_compatible"`
	PreflightOK      bool      `json:"preflight_ok"`
}

// uploadErrorResponse is the error-shaped JSON returned for non-2xx
// outcomes. Keeps the wire shape stable so the frontend can pull the
// message off without sniffing content-type.
type uploadErrorResponse struct {
	Error string `json:"error"`
}

// ServeHTTP implements http.Handler.
func (h *SysupgradeUploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeUploadError(w, h.Log, http.StatusMethodNotAllowed, "method not allowed")

		return
	}

	if h.Manager == nil {
		writeUploadError(w, h.Log, http.StatusServiceUnavailable, "sysupgrade not configured on this device")

		return
	}

	src, filename, cleanup, err := openUploadBody(r)
	if err != nil {
		writeUploadError(w, h.Log, http.StatusBadRequest, err.Error())

		return
	}

	if cleanup != nil {
		defer cleanup()
	}

	staged, err := h.Manager.StoreStagedImage(r.Context(), src, filename)
	if err != nil {
		status, msg := classifyUploadError(err)
		writeUploadError(w, h.Log, status, msg)

		return
	}

	resp := uploadResponse{
		Filename:         staged.Filename,
		SizeBytes:        staged.SizeBytes,
		Sha256:           staged.Sha256,
		MetadataPresent:  staged.MetadataPresent,
		CompatVersion:    staged.CompatVersion,
		CompatMessage:    staged.CompatMessage,
		SupportedDevices: staged.SupportedDevices,
		DeviceCompat:     staged.DeviceCompat,
		ImageCompatible:  staged.ImageCompatible,
		PreflightOK:      staged.PreflightOK,
		PreflightError:   staged.PreflightError,
		UploadedAt:       staged.UploadedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
		h.Log.Warn().Err(encErr).Msg("sysupgrade upload: response encode failed")
	}
}

// openUploadBody returns a reader over the uploaded image bytes, the
// operator-supplied filename, and an optional cleanup func. Three
// content shapes are accepted:
//
//   - multipart/form-data: the first non-text file part is used.
//   - application/octet-stream (or any other non-multipart type): the
//     raw request body is used and the filename is taken from the
//     X-Filename header (decoded) or, failing that, "uploaded-image.bin".
func openUploadBody(r *http.Request) (io.Reader, string, func(), error) {
	contentType := r.Header.Get("Content-Type")

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil && contentType != "" {
		return nil, "", nil, errors.New("invalid Content-Type header")
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		mr, mErr := r.MultipartReader()
		if mErr != nil {
			return nil, "", nil, errors.New("multipart parse failed: " + mErr.Error())
		}

		for {
			part, pErr := mr.NextPart()
			if pErr == io.EOF {
				return nil, "", nil, errors.New("no file part in multipart body")
			}

			if pErr != nil {
				return nil, "", nil, errors.New("multipart read failed: " + pErr.Error())
			}

			if part.FileName() == "" {
				_ = part.Close()

				continue
			}

			cleanup := func() { _ = part.Close() }

			return part, part.FileName(), cleanup, nil
		}
	}

	_ = params // boundary already consumed by MultipartReader path.

	filename := r.Header.Get("X-Filename")
	if filename == "" {
		filename = "uploaded-image.bin"
	}

	return r.Body, filename, nil, nil
}

// classifyUploadError maps a manager-side error onto an HTTP status
// code + short message safe to expose to the operator.
func classifyUploadError(err error) (int, string) {
	switch {
	case errors.Is(err, sysupgrade.ErrUploadInFlight):
		return http.StatusConflict, "another upload is already in progress"
	case errors.Is(err, sysupgrade.ErrUploadTooLarge):
		return http.StatusRequestEntityTooLarge, "uploaded image exceeds the maximum allowed size"
	case errors.Is(err, sysupgrade.ErrUploadEmpty):
		return http.StatusBadRequest, "uploaded image is empty"
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout, "upload canceled"
	}

	return http.StatusInternalServerError, err.Error()
}

// writeUploadError encodes a JSON error response with the supplied
// status code. The log line is at warn level for 4xx (operator
// problem) and error level for 5xx (daemon problem).
func writeUploadError(w http.ResponseWriter, log zerolog.Logger, status int, msg string) {
	if status >= http.StatusInternalServerError {
		log.Error().Int("status", status).Str("msg", msg).Msg("sysupgrade upload: failed")
	} else {
		log.Warn().Int("status", status).Str("msg", msg).Msg("sysupgrade upload: rejected")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(uploadErrorResponse{Error: msg})
}
