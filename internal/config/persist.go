package config

import (
	"fmt"
	"os"

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
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1].Value = value
			mapping.Content[i+1].Tag = "!!bool"

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
		Tag:   "!!bool",
		Value: value,
	}
	mapping.Content = append(mapping.Content, keyNode, valueNode)
}
