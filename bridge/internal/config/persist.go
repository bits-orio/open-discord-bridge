package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// UpdateRoutesFile rewrites just the discord.routes section of the YAML config at path, in
// place. Used by the Control API's POST /v1/config: routing changes are applied live in
// memory immediately, but without this they silently revert on the next restart. A
// surgical node edit (rather than re-marshaling the whole loaded *Config) is used
// deliberately: Load() expands ${ENV} vars and ~/ paths in place, so re-marshaling the
// loaded struct would bake those expansions into the file and destroy portability for
// every other field — this only ever touches discord.routes.
func UpdateRoutesFile(path string, routes []Route) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("config: unexpected document shape")
	}
	root := doc.Content[0]

	discordNode := mapValue(root, "discord")
	if discordNode == nil {
		// No discord section at all — add one.
		discordNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, keyNode("discord"), discordNode)
	}

	routesNode, err := routesToNode(routes)
	if err != nil {
		return err
	}
	if existing := mapValue(discordNode, "routes"); existing != nil {
		*existing = *routesNode
	} else {
		discordNode.Content = append(discordNode.Content, keyNode("routes"), routesNode)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

// mapValue returns the value node for key in a YAML mapping node, or nil if absent/m isn't
// a mapping.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func keyNode(name string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
}

// routesToNode marshals routes through YAML to get a properly-shaped sequence node (a
// mapping node per route, in the same source/channel_id shape the rest of the file uses).
func routesToNode(routes []Route) (*yaml.Node, error) {
	b, err := yaml.Marshal(routes)
	if err != nil {
		return nil, fmt.Errorf("marshal routes: %w", err)
	}
	var wrapper yaml.Node
	if err := yaml.Unmarshal(b, &wrapper); err != nil {
		return nil, fmt.Errorf("re-parse routes: %w", err)
	}
	if len(wrapper.Content) == 0 {
		return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}, nil
	}
	return wrapper.Content[0], nil
}
