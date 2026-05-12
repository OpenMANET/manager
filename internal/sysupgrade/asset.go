package sysupgrade

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by MatchAsset. Handlers map these to
// CodeFailedPrecondition.
var (
	// ErrNoMatchingAsset is returned when neither the manifest nor the
	// substring heuristic produces a single matching asset.
	ErrNoMatchingAsset = errors.New("sysupgrade: no matching asset for this hardware")

	// ErrAmbiguousAsset is returned when the heuristic produces more
	// than one candidate. The caller should publish a manifest to
	// disambiguate.
	ErrAmbiguousAsset = errors.New("sysupgrade: multiple assets match this hardware")
)

// validSysupgradeSuffixes is the closed set of asset filename suffixes
// considered to be sysupgrade images. The set is intentionally narrow:
// initramfs, factory, and rootfs images are not flashable via
// sysupgrade(1).
//
//nolint:gochecknoglobals // closed-set lookup table; treated as const
var validSysupgradeSuffixes = []string{
	"sysupgrade.bin",
	"sysupgrade.img.gz",
	"sysupgrade.img",
	"sysupgrade.tar",
}

// MatchAsset returns the asset that should be flashed onto a device
// described by boardName + target. If a manifest is present, its
// boards[boardName] entry is the authoritative answer. Otherwise the
// substring heuristic is applied.
//
// The heuristic requires:
//  1. filename ends with a known sysupgrade suffix,
//  2. all dot-and-comma-replaced board_name tokens appear,
//  3. all slash-replaced target tokens appear.
//
// Exactly one match is required; zero → ErrNoMatchingAsset, two or more
// → ErrAmbiguousAsset.
func MatchAsset(boardName, target string, manifest *Manifest, assets []Asset) (Asset, error) {
	if manifest != nil {
		if name, ok := manifest.Boards[boardName]; ok && name != "" {
			for _, a := range assets {
				if a.Name == name {
					return a, nil
				}
			}

			return Asset{}, fmt.Errorf("%w: manifest names %q but asset is missing", ErrNoMatchingAsset, name)
		}
	}

	return heuristicMatch(boardName, target, assets)
}

// heuristicMatch implements the substring fallback. Tokens are
// extracted by replacing the canonical separators in board_name (".",
// ",") and target ("/") with empty/dash so they line up with the
// dash-separated chunks of an OpenWrt asset filename.
func heuristicMatch(boardName, target string, assets []Asset) (Asset, error) {
	boardTokens := tokenizeBoardName(boardName)
	targetTokens := tokenizeTarget(target)

	candidates := make([]Asset, 0, 2)

	for _, a := range assets {
		if !hasValidSuffix(a.Name) {
			continue
		}

		lower := strings.ToLower(a.Name)
		if !containsAll(lower, boardTokens) {
			continue
		}

		if !containsAll(lower, targetTokens) {
			continue
		}

		candidates = append(candidates, a)
	}

	switch len(candidates) {
	case 0:
		return Asset{}, ErrNoMatchingAsset
	case 1:
		return candidates[0], nil
	default:
		names := make([]string, 0, len(candidates))
		for _, a := range candidates {
			names = append(names, a.Name)
		}

		return Asset{}, fmt.Errorf("%w: candidates=%s", ErrAmbiguousAsset, strings.Join(names, ","))
	}
}

// tokenizeBoardName splits a board id like "bcm2711,mm8108-usb" into
// lowercase tokens that are expected to appear in an asset filename.
func tokenizeBoardName(boardName string) []string {
	if boardName == "" {
		return nil
	}

	// Replace common separators with dashes so each chunk is a single
	// token, then split.
	normalized := strings.ToLower(boardName)
	normalized = strings.ReplaceAll(normalized, ",", "-")
	normalized = strings.ReplaceAll(normalized, ".", "-")

	return splitNonEmpty(normalized, "-")
}

// tokenizeTarget splits a DISTRIB_TARGET like "bcm27xx/bcm2711" into
// lowercase tokens.
func tokenizeTarget(target string) []string {
	if target == "" {
		return nil
	}

	normalized := strings.ToLower(target)
	normalized = strings.ReplaceAll(normalized, "/", "-")

	return splitNonEmpty(normalized, "-")
}

// splitNonEmpty splits s by sep and discards empty entries.
func splitNonEmpty(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := parts[:0]

	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}

	return out
}

// containsAll reports whether every token appears as a substring in s.
func containsAll(s string, tokens []string) bool {
	for _, t := range tokens {
		if !strings.Contains(s, t) {
			return false
		}
	}

	return true
}

// hasValidSuffix reports whether the asset filename ends with a known
// sysupgrade suffix.
func hasValidSuffix(name string) bool {
	lower := strings.ToLower(name)
	for _, s := range validSysupgradeSuffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}

	return false
}
