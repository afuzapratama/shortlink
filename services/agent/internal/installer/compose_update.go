package installer

import (
	"errors"
	"gopkg.in/yaml.v3"
)

// Update only the managed agent image. Keep proxy, mounts, logging and other
// operator customizations, including additional services and YAML comments.
func updateComposeImage(raw []byte, image, proxyImage string) ([]byte, error) {
	var doc yaml.Node
	if yaml.Unmarshal(raw, &doc) != nil || len(doc.Content) != 1 {
		return nil, errors.New("invalid existing Compose configuration")
	}
	field := func(node *yaml.Node, name string) *yaml.Node {
		if node == nil || node.Kind != yaml.MappingNode {
			return nil
		}
		for n := 0; n+1 < len(node.Content); n += 2 {
			if node.Content[n].Value == name {
				return node.Content[n+1]
			}
		}
		return nil
	}
	target := field(field(field(doc.Content[0], "services"), "agent"), "image")
	if target == nil || target.Kind != yaml.ScalarNode {
		return nil, errors.New("existing Compose configuration has no managed agent image")
	}
	target.Value = image
	target.Tag = "!!str"
	// A first-install scaffold may precede resolving the pinned Caddy image.
	proxy := field(field(field(doc.Content[0], "services"), "proxy"), "image")
	if proxy != nil && proxy.Kind == yaml.ScalarNode && proxy.Value == "" && proxyImage != "" {
		proxy.Value = proxyImage
		proxy.Tag = "!!str"
	}
	return yaml.Marshal(&doc)
}
