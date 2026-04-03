package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// GetConfigFilePath returns the path of the config file currently in use.
func (c *Config) GetConfigFilePath() string {
	return c.v.ConfigFileUsed()
}

// PersistBLOSConfig updates the blos.enable value in the YAML config file
// and refreshes the in-memory config state. It preserves comments and key
// ordering in the YAML file by operating on the yaml.Node tree.
func (c *Config) PersistBLOSConfig(enable bool) error {
	c.persistMu.Lock()
	defer c.persistMu.Unlock()

	filePath := c.v.ConfigFileUsed()
	if filePath == "" {
		return fmt.Errorf("no config file path configured")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	var doc yaml.Node

	err = yaml.Unmarshal(data, &doc)
	if err != nil {
		return fmt.Errorf("parsing config file: %w", err)
	}

	err = setBLOSEnable(&doc, enable)
	if err != nil {
		return fmt.Errorf("updating blos.enable: %w", err)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	//nolint:gosec // config file permissions match the original file
	err = os.WriteFile(filePath, out, 0644)
	if err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	// Update in-memory state immediately without waiting for fsnotify.
	c.v.Set("blos.enable", enable)
	c.reload()

	return nil
}

// setBLOSEnable finds or creates the blos.enable key in the YAML document
// node tree and sets its value.
func setBLOSEnable(doc *yaml.Node, enable bool) error {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("unexpected YAML structure: expected document node")
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("unexpected YAML structure: expected mapping node at root")
	}

	// Find or create the "blos" mapping
	blosMapping := findOrCreateMapping(root, "blos")

	// Find or create the "enable" key within the blos mapping
	setScalarValue(blosMapping, "enable", fmt.Sprintf("%t", enable))

	return nil
}

// findOrCreateMapping finds a key in a mapping node and returns its value node.
// If the key doesn't exist, it creates a new mapping entry. The value node
// is expected to be (or created as) a mapping node.
func findOrCreateMapping(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}

	// Key not found; create it
	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: key,
	}
	valueNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	mapping.Content = append(mapping.Content, keyNode, valueNode)

	return valueNode
}

// setScalarValue finds a key in a mapping node and sets its scalar value.
// If the key doesn't exist, it creates a new entry.
func setScalarValue(mapping *yaml.Node, key string, value string) {
	setScalarWithTag(mapping, key, value, "!!bool")
}

// setScalarWithTag finds a key in a mapping node and sets its scalar value
// with the given YAML tag. If the key doesn't exist, it creates a new entry.
func setScalarWithTag(mapping *yaml.Node, key string, value string, tag string) {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1].Value = value
			mapping.Content[i+1].Tag = tag

			return
		}
	}

	// Key not found; create it
	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: key,
	}
	valueNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   tag,
		Value: value,
	}
	mapping.Content = append(mapping.Content, keyNode, valueNode)
}

// PersistCommsConfig updates the comms.enable and comms.controlSource values
// in the YAML config file and refreshes the in-memory config state. It
// preserves comments and key ordering by operating on the yaml.Node tree.
func (c *Config) PersistCommsConfig(enable bool, controlSource string) error {
	c.persistMu.Lock()
	defer c.persistMu.Unlock()

	filePath := c.v.ConfigFileUsed()
	if filePath == "" {
		return fmt.Errorf("no config file path configured")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	var doc yaml.Node

	err = yaml.Unmarshal(data, &doc)
	if err != nil {
		return fmt.Errorf("parsing config file: %w", err)
	}

	if err = setCommsConfig(&doc, enable, controlSource); err != nil {
		return fmt.Errorf("updating comms config: %w", err)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	//nolint:gosec // config file permissions match the original file
	err = os.WriteFile(filePath, out, 0644)
	if err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	// Update in-memory state immediately without waiting for fsnotify.
	c.v.Set("comms.enable", enable)
	c.v.Set("comms.controlSource", controlSource)
	c.reload()

	return nil
}

// setCommsConfig finds or creates the comms section in the YAML document and
// sets the enable and controlSource keys.
func setCommsConfig(doc *yaml.Node, enable bool, controlSource string) error {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("unexpected YAML structure: expected document node")
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("unexpected YAML structure: expected mapping node at root")
	}

	commsMapping := findOrCreateMapping(root, "comms")
	setScalarValue(commsMapping, "enable", fmt.Sprintf("%t", enable))
	setScalarWithTag(commsMapping, "controlSource", controlSource, "!!str")

	return nil
}

// PersistGNSSConfig updates the GNSS configuration in the YAML config file
// and refreshes the in-memory config state. It preserves comments and key
// ordering in the YAML file by operating on the yaml.Node tree.
func (c *Config) PersistGNSSConfig(enable, sendAsNMEA, sendAsCoT bool, cotUID string) error {
	c.persistMu.Lock()
	defer c.persistMu.Unlock()

	filePath := c.v.ConfigFileUsed()
	if filePath == "" {
		return fmt.Errorf("no config file path configured")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	var doc yaml.Node

	err = yaml.Unmarshal(data, &doc)
	if err != nil {
		return fmt.Errorf("parsing config file: %w", err)
	}

	if err = setGNSSConfig(&doc, enable, sendAsNMEA, sendAsCoT, cotUID); err != nil {
		return fmt.Errorf("updating gnss config: %w", err)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	//nolint:gosec // config file permissions match the original file
	err = os.WriteFile(filePath, out, 0644)
	if err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	// Update in-memory state immediately without waiting for fsnotify.
	c.v.Set("gnss.enable", enable)
	c.v.Set("gnss.sendAsExternalGNSSSource.sendAsNMEA", sendAsNMEA)
	c.v.Set("gnss.sendAsExternalGNSSSource.sendAsCoT", sendAsCoT)
	c.v.Set("gnss.sendAsExternalGNSSSource.cotUID", cotUID)
	c.reload()

	return nil
}

// setGNSSConfig finds or creates the gnss section in the YAML document and
// sets all GNSS configuration keys.
func setGNSSConfig(doc *yaml.Node, enable, sendAsNMEA, sendAsCoT bool, cotUID string) error {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("unexpected YAML structure: expected document node")
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("unexpected YAML structure: expected mapping node at root")
	}

	gnssMapping := findOrCreateMapping(root, "gnss")
	setScalarValue(gnssMapping, "enable", strconv.FormatBool(enable))

	sendMapping := findOrCreateMapping(gnssMapping, "sendAsExternalGNSSSource")
	setScalarValue(sendMapping, "sendAsNMEA", strconv.FormatBool(sendAsNMEA))
	setScalarValue(sendMapping, "sendAsCoT", strconv.FormatBool(sendAsCoT))
	setScalarWithTag(sendMapping, "cotUID", cotUID, "!!str")

	return nil
}
