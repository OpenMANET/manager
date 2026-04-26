package sysupgrade

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// sha256SumsBodyLimit caps the size of a sha256sums file the daemon will
// read into memory.
const sha256SumsBodyLimit = 16 << 10 // 16 KiB

// ErrChecksumNotFound is returned by ParseSHA256Sums when the requested
// filename is not present in the sums file.
var ErrChecksumNotFound = errors.New("sysupgrade: checksum not found for asset")

// ErrChecksumMismatch is returned by VerifyDigest when the computed
// digest does not match the expected hex digest.
var ErrChecksumMismatch = errors.New("sysupgrade: sha256 checksum mismatch")

// FetchSHA256Sum downloads the sha256sums file from the supplied URL,
// parses it, and returns the lowercase hex digest for assetName. The
// download is bounded by sha256SumsBodyLimit. httpClient may be nil.
func FetchSHA256Sum(ctx context.Context, httpClient *http.Client, sumsURL, assetName string) (string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return "", fmt.Errorf("sha256sums: build request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sha256sums: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sha256sums: status %d", resp.StatusCode)
	}

	digest, err := ParseSHA256Sums(io.LimitReader(resp.Body, sha256SumsBodyLimit), assetName)
	if err != nil {
		return "", err
	}

	return digest, nil
}

// ParseSHA256Sums reads a sha256sums-formatted stream and returns the
// lowercase hex digest associated with assetName. The file format is
// "<hex>  <filename>" or "<hex> *<filename>" per line; leading "./" on
// the filename is tolerated.
func ParseSHA256Sums(r io.Reader, assetName string) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on the first whitespace block to separate the digest
		// from the filename. Handle both "  " and " *" separators.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		name := strings.Join(fields[1:], " ")
		name = strings.TrimPrefix(name, "*")
		name = strings.TrimPrefix(name, "./")

		if name == assetName {
			digest := strings.ToLower(fields[0])
			if _, err := hex.DecodeString(digest); err != nil {
				return "", fmt.Errorf("sha256sums: malformed digest for %q: %w", assetName, err)
			}

			return digest, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("sha256sums: scan: %w", err)
	}

	return "", fmt.Errorf("%w: %s", ErrChecksumNotFound, assetName)
}

// VerifyDigest compares two hex digests in constant time after lowering.
// Returns ErrChecksumMismatch when they differ.
func VerifyDigest(expected, got string) error {
	if strings.EqualFold(expected, got) {
		return nil
	}

	return fmt.Errorf("%w: expected=%s got=%s", ErrChecksumMismatch, expected, got)
}
