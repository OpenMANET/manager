package sysupgrade

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTag(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Version
		wantErr bool
	}{
		{name: "v-prefix stable", input: "v1.2.3", want: Version{Raw: "v1.2.3", Major: 1, Minor: 2, Patch: 3}},
		{name: "no prefix stable", input: "1.2.3", want: Version{Raw: "1.2.3", Major: 1, Minor: 2, Patch: 3}},
		{name: "pre-release dash", input: "v1.2.3-rc.1", want: Version{Raw: "v1.2.3-rc.1", Major: 1, Minor: 2, Patch: 3, Pre: "rc.1"}},
		{name: "pre-release dot", input: "1.2.3.beta1", want: Version{Raw: "1.2.3.beta1", Major: 1, Minor: 2, Patch: 3, Pre: "beta1"}},
		{name: "missing patch", input: "v1.2", wantErr: true},
		{name: "non-numeric", input: "release-1.2.3", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTag(tt.input)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseFromDescription(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTag   string
		wantPatch int
		wantErr   bool
	}{
		{name: "OpenWrt + OpenMANET", input: "OpenWrt 23.05.3 / OpenMANET 1.7.0", wantTag: "1.7.0", wantPatch: 0},
		{name: "OpenMANET branch + version", input: "OpenMANET 24.10 1.7.0", wantTag: "1.7.0"},
		{name: "OpenMANET only", input: "OpenMANET 2.0.0", wantTag: "2.0.0"},
		{name: "no OpenMANET", input: "OpenWrt 23.05.3", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFromDescription(tt.input)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantTag, got.Canonical())
		})
	}
}

func TestCompare(t *testing.T) {
	mk := func(s string) Version {
		v, err := ParseTag(s)
		require.NoError(t, err)

		return v
	}

	cases := []struct {
		a, b string
		want int
	}{
		{a: "1.0.0", b: "1.0.0", want: 0},
		{a: "1.0.0", b: "2.0.0", want: -1},
		{a: "2.0.0", b: "1.9.9", want: 1},
		{a: "1.10.0", b: "1.9.9", want: 1},
		{a: "1.0.0-rc.1", b: "1.0.0", want: -1},
		{a: "1.0.0", b: "1.0.0-rc.1", want: 1},
		{a: "1.0.0-rc.1", b: "1.0.0-rc.2", want: -1},
		{a: "1.0.0-rc.2", b: "1.0.0-rc.1", want: 1},
	}

	for _, tc := range cases {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			assert.Equal(t, tc.want, Compare(mk(tc.a), mk(tc.b)))
		})
	}
}

func TestVersionIsZero(t *testing.T) {
	assert.True(t, Version{}.IsZero())

	v, _ := ParseTag("1.0.0")
	assert.False(t, v.IsZero())
}
