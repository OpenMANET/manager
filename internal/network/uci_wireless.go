package network

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	defaultWirelessConfigPath string = "/etc/config/wireless"
)

type wirelessSection struct {
	options map[string]string
	typ     string
}

// GetWirelessMeshPassphrase returns the first enabled mesh interface passphrase
// from the OpenWrt wireless UCI configuration.
func GetWirelessMeshPassphrase() (string, error) {
	return GetWirelessMeshPassphraseFromPath(defaultWirelessConfigPath)
}

// GetWirelessMeshPassphraseFromPath returns the first enabled mesh interface
// passphrase from the provided OpenWrt wireless config path.
func GetWirelessMeshPassphraseFromPath(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open wireless config %s: %w", path, err)
	}
	defer file.Close()

	var (
		current      *wirelessSection
		meshFound    bool
		meshMissingK bool
	)

	finalize := func(section *wirelessSection) (string, bool) {
		if section == nil || section.typ != "wifi-iface" {
			return "", false
		}

		if strings.ToLower(strings.TrimSpace(section.options["mode"])) != "mesh" {
			return "", false
		}

		if strings.TrimSpace(section.options["disabled"]) == "1" {
			return "", false
		}

		meshFound = true

		key := strings.TrimSpace(section.options["key"])
		if key == "" {
			meshMissingK = true

			return "", false
		}

		return key, true
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if typ, _, ok := parseUCIConfigLine(line); ok {
			if key, found := finalize(current); found {
				return key, nil
			}

			current = &wirelessSection{
				typ:     typ,
				options: map[string]string{},
			}

			continue
		}

		if current == nil {
			continue
		}

		if option, value, ok := parseUCIOptionLine(line); ok {
			current.options[option] = value
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read wireless config %s: %w", path, err)
	}

	if key, found := finalize(current); found {
		return key, nil
	}

	if !meshFound {
		return "", fmt.Errorf("no enabled mesh wifi-iface section found in %s", path)
	}

	if meshMissingK {
		return "", fmt.Errorf("enabled mesh wifi-iface section missing key in %s", path)
	}

	return "", fmt.Errorf("mesh key not found in %s", path)
}

func parseUCIConfigLine(line string) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "config" {
		return "", "", false
	}

	typ := trimUCIValue(fields[1])

	name := ""
	if len(fields) > 2 {
		name = trimUCIValue(fields[2])
	}

	return typ, name, true
}

func parseUCIOptionLine(line string) (string, string, bool) {
	if !strings.HasPrefix(line, "option") {
		return "", "", false
	}

	if len(line) > len("option") {
		next := line[len("option")]
		if next != ' ' && next != '\t' {
			return "", "", false
		}
	}

	rest := strings.TrimSpace(strings.TrimPrefix(line, "option"))
	if rest == "" {
		return "", "", false
	}

	spaceIdx := strings.IndexAny(rest, " \t")
	if spaceIdx == -1 {
		return "", "", false
	}

	name := strings.TrimSpace(rest[:spaceIdx])
	if name == "" {
		return "", "", false
	}

	value := trimUCIValue(strings.TrimSpace(rest[spaceIdx+1:]))

	return name, value, true
}

func trimUCIValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 2 {
		return v
	}

	first := v[0]
	last := v[len(v)-1]

	if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
		return v[1 : len(v)-1]
	}

	return v
}
