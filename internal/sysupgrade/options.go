package sysupgrade

import (
	"errors"
	"fmt"
)

// SysupgradeOptions maps directly to the OpenWrt sysupgrade(1) script
// flags. Booleans become the corresponding flag when true; string fields
// become "-x value" when non-empty.
//
// See https://github.com/openwrt/openwrt/blob/openwrt-24.10/package/base-files/files/sbin/sysupgrade.
type SysupgradeOptions struct {
	ConfigArchivePath  string // -f <path>
	BackupPath         string // -b <path>
	RestorePath        string // -r <path>
	NoPreserveConfig   bool   // -n
	PreserveChangedEtc bool   // -c
	PreserveOverlay    bool   // -o
	SkipPackageFiles   bool   // -u
	IncludeEtcConfig   bool   // -k
	TestOnly           bool   // -T
	Force              bool   // -F
	Quiet              bool   // -q
	Verbose            bool   // -v
	PreservePartitions bool   // -p
	ErasePartitions    bool   // -e
}

// Sentinel errors returned by ToArgs for mutually exclusive flag
// combinations the script itself rejects.
var (
	// ErrOptionConflict is returned when caller-supplied options are
	// incompatible (e.g. -n and -c, -T and -F).
	ErrOptionConflict = errors.New("sysupgrade: incompatible option combination")
)

// ToArgs builds the argv (excluding the trailing image path) that should
// be passed to the sysupgrade binary. Returns ErrOptionConflict when the
// supplied options are mutually exclusive.
//
// The argv is allocated with capacity tuned for the typical case (a
// handful of flags); pre-sizing avoids repeated reallocation in the hot
// path that prepares an upgrade.
func (o SysupgradeOptions) ToArgs() ([]string, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}

	args := make([]string, 0, 14)

	if o.NoPreserveConfig {
		args = append(args, "-n")
	}

	if o.PreserveChangedEtc {
		args = append(args, "-c")
	}

	if o.PreserveOverlay {
		args = append(args, "-o")
	}

	if o.SkipPackageFiles {
		args = append(args, "-u")
	}

	if o.IncludeEtcConfig {
		args = append(args, "-k")
	}

	if o.ConfigArchivePath != "" {
		args = append(args, "-f", o.ConfigArchivePath)
	}

	if o.TestOnly {
		args = append(args, "-T")
	}

	if o.Force {
		args = append(args, "-F")
	}

	if o.Quiet {
		args = append(args, "-q")
	}

	if o.Verbose {
		args = append(args, "-v")
	}

	if o.BackupPath != "" {
		args = append(args, "-b", o.BackupPath)
	}

	if o.RestorePath != "" {
		args = append(args, "-r", o.RestorePath)
	}

	if o.PreservePartitions {
		args = append(args, "-p")
	}

	if o.ErasePartitions {
		args = append(args, "-e")
	}

	return args, nil
}

// validate checks the supplied flag combinations for consistency. The
// rules mirror the sysupgrade(1) script's preflight rejections.
func (o SysupgradeOptions) validate() error {
	if o.NoPreserveConfig && o.PreserveChangedEtc {
		return fmt.Errorf("%w: -n and -c are mutually exclusive", ErrOptionConflict)
	}

	if o.NoPreserveConfig && o.PreserveOverlay {
		return fmt.Errorf("%w: -n and -o are mutually exclusive", ErrOptionConflict)
	}

	if o.TestOnly && o.Force {
		return fmt.Errorf("%w: -T and -F are mutually exclusive", ErrOptionConflict)
	}

	if o.BackupPath != "" && o.RestorePath != "" {
		return fmt.Errorf("%w: -b and -r are mutually exclusive", ErrOptionConflict)
	}

	if o.PreservePartitions && o.ErasePartitions {
		return fmt.Errorf("%w: -p and -e are mutually exclusive", ErrOptionConflict)
	}

	return nil
}
