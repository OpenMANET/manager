package sysupgrade

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSysupgradeOptions_ToArgs(t *testing.T) {
	tests := []struct {
		name string
		opts SysupgradeOptions
		want []string
	}{
		{name: "zero", opts: SysupgradeOptions{}, want: []string{}},
		{name: "verbose only", opts: SysupgradeOptions{Verbose: true}, want: []string{"-v"}},
		{name: "no preserve", opts: SysupgradeOptions{NoPreserveConfig: true}, want: []string{"-n"}},
		{name: "preserve etc + overlay", opts: SysupgradeOptions{PreserveChangedEtc: true, PreserveOverlay: true}, want: []string{"-c", "-o"}},
		{name: "config archive", opts: SysupgradeOptions{ConfigArchivePath: "/tmp/cfg.tar.gz"}, want: []string{"-f", "/tmp/cfg.tar.gz"}},
		{name: "test only", opts: SysupgradeOptions{TestOnly: true}, want: []string{"-T"}},
		{name: "force only", opts: SysupgradeOptions{Force: true}, want: []string{"-F"}},
		{name: "backup", opts: SysupgradeOptions{BackupPath: "/tmp/bk.tar.gz"}, want: []string{"-b", "/tmp/bk.tar.gz"}},
		{name: "restore", opts: SysupgradeOptions{RestorePath: "/tmp/bk.tar.gz"}, want: []string{"-r", "/tmp/bk.tar.gz"}},
		{name: "preserve partitions", opts: SysupgradeOptions{PreservePartitions: true}, want: []string{"-p"}},
		{name: "erase partitions", opts: SysupgradeOptions{ErasePartitions: true}, want: []string{"-e"}},
		{name: "verbose+test", opts: SysupgradeOptions{Verbose: true, TestOnly: true}, want: []string{"-T", "-v"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.opts.ToArgs()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSysupgradeOptions_ToArgs_Conflicts(t *testing.T) {
	tests := []struct {
		name string
		opts SysupgradeOptions
	}{
		{name: "n+c", opts: SysupgradeOptions{NoPreserveConfig: true, PreserveChangedEtc: true}},
		{name: "n+o", opts: SysupgradeOptions{NoPreserveConfig: true, PreserveOverlay: true}},
		{name: "T+F", opts: SysupgradeOptions{TestOnly: true, Force: true}},
		{name: "b+r", opts: SysupgradeOptions{BackupPath: "/a", RestorePath: "/b"}},
		{name: "p+e", opts: SysupgradeOptions{PreservePartitions: true, ErasePartitions: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.opts.ToArgs()
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrOptionConflict))
		})
	}
}
