package sysupgrade

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// versionRE matches semver-shaped strings with an optional leading "v"
// and an optional pre-release suffix.
var versionRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[-.]([0-9A-Za-z.-]+))?$`)

// descriptionRE pulls the OpenMANET semver out of a free-form
// DISTRIB_DESCRIPTION. Matches "OpenMANET 24.10 1.7.0" and
// "OpenWrt 23.05.3 / OpenMANET 1.7.0".
var descriptionRE = regexp.MustCompile(`OpenMANET[^0-9]*(?:\d+\.\d+\s+)?(\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?)`)

// Version is a parsed semver-shaped version with optional pre-release.
type Version struct {
	Raw   string // original input (e.g. "v1.7.0", "1.7.0-rc.1")
	Pre   string // pre-release suffix without leading separator ("rc.1")
	Major int
	Minor int
	Patch int
}

// IsZero reports whether the version is the zero value.
func (v Version) IsZero() bool {
	return v.Major == 0 && v.Minor == 0 && v.Patch == 0 && v.Pre == "" && v.Raw == ""
}

// Canonical returns the canonical "MAJOR.MINOR.PATCH[-PRE]" form,
// without any leading "v".
func (v Version) Canonical() string {
	base := strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
	if v.Pre == "" {
		return base
	}

	return base + "-" + v.Pre
}

// ParseTag parses a release tag such as "v1.8.0", "1.8.0", or
// "v1.8.0-rc.1". Returns an error if the tag does not match the
// canonical shape.
func ParseTag(tag string) (Version, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(tag))
	if m == nil {
		return Version{}, fmt.Errorf("parse version %q: malformed", tag)
	}

	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])

	return Version{
		Raw:   tag,
		Major: major,
		Minor: minor,
		Patch: patch,
		Pre:   m[4],
	}, nil
}

// ParseFromDescription extracts the OpenMANET version from a free-form
// description string (typically DISTRIB_DESCRIPTION). Returns an empty
// Version with an error when no semver-shaped fragment is found.
func ParseFromDescription(s string) (Version, error) {
	m := descriptionRE.FindStringSubmatch(s)
	if len(m) != 2 {
		return Version{}, fmt.Errorf("parse description %q: no openmanet version", s)
	}

	return ParseTag(m[1])
}

// Compare returns -1 if a < b, 0 if a == b, 1 if a > b. Pre-release
// versions sort before stable versions of the same MAJOR.MINOR.PATCH:
// 1.0.0-rc.1 < 1.0.0. Pre-release strings are compared lexicographically.
func Compare(a, b Version) int {
	if a.Major != b.Major {
		return cmpInt(a.Major, b.Major)
	}

	if a.Minor != b.Minor {
		return cmpInt(a.Minor, b.Minor)
	}

	if a.Patch != b.Patch {
		return cmpInt(a.Patch, b.Patch)
	}

	switch {
	case a.Pre == "" && b.Pre == "":
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}

	return strings.Compare(a.Pre, b.Pre)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}

	return 0
}
