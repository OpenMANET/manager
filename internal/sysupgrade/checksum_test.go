package sysupgrade

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sumsBody = `# comment line
abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234  ./openmanet-1.8.0.img.gz
0011223344556677889900112233445566778899001122334455667788990011  *manifest.json
`

func TestParseSHA256Sums_Found(t *testing.T) {
	got, err := ParseSHA256Sums(strings.NewReader(sumsBody), "openmanet-1.8.0.img.gz")
	require.NoError(t, err)
	assert.Equal(t, "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234", got)
}

func TestParseSHA256Sums_Star(t *testing.T) {
	got, err := ParseSHA256Sums(strings.NewReader(sumsBody), "manifest.json")
	require.NoError(t, err)
	assert.Equal(t, "0011223344556677889900112233445566778899001122334455667788990011", got)
}

func TestParseSHA256Sums_NotFound(t *testing.T) {
	_, err := ParseSHA256Sums(strings.NewReader(sumsBody), "missing.img")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrChecksumNotFound))
}

func TestParseSHA256Sums_MalformedDigest(t *testing.T) {
	_, err := ParseSHA256Sums(strings.NewReader("not-a-digest  foo.img\n"), "foo.img")
	require.Error(t, err)
}

func TestVerifyDigest(t *testing.T) {
	require.NoError(t, VerifyDigest("ABCDEF", "abcdef"))

	err := VerifyDigest("abcdef", "deadbe")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrChecksumMismatch))
}

func TestFetchSHA256Sum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sumsBody))
	}))
	t.Cleanup(srv.Close)

	got, err := FetchSHA256Sum(context.Background(), srv.Client(), srv.URL+"/sha256sums", "openmanet-1.8.0.img.gz")
	require.NoError(t, err)
	assert.Equal(t, "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234", got)
}

func TestFetchSHA256Sum_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, err := FetchSHA256Sum(context.Background(), srv.Client(), srv.URL+"/sha256sums", "openmanet-1.8.0.img.gz")
	require.Error(t, err)
}
