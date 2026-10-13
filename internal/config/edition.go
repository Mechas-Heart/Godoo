// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

// Odoo editions. Enterprise is Community plus the proprietary enterprise
// addons, which need an Odoo Enterprise subscription (or partner access) to
// the odoo/enterprise repository, or a company mirror of it.
const (
	EditionCommunity  = "community"
	EditionEnterprise = "enterprise"

	DefaultEnterpriseURL  = "git@github.com:odoo/enterprise.git"
	DefaultEnterprisePath = "./enterprise"
)

// EnterpriseConfig says where the enterprise addons come from.
type EnterpriseConfig struct {
	URL string `yaml:"url"`
	// Branch is detected on first clone (18.0, then 18, then the default
	// branch) and written back to godoo.yaml.
	Branch string `yaml:"branch"`
	Path   string `yaml:"path"`
}

// EnterpriseOn reports whether this project runs Odoo Enterprise.
func (c *Config) EnterpriseOn() bool { return c.Odoo.Edition == EditionEnterprise }

// EnterpriseRepo describes the enterprise addons as a Repo, so cloning and
// branch detection work exactly like for any other addon repository.
func (c *Config) EnterpriseRepo() Repo {
	e := c.Odoo.Enterprise
	return Repo{Name: "enterprise", URL: e.URL, Branch: e.Branch, Path: e.Path}
}

func (c *Config) resolveEdition() error {
	o := &c.Odoo
	if o.Edition == "" {
		o.Edition = EditionCommunity
	}
	if o.Edition != EditionCommunity && o.Edition != EditionEnterprise {
		return fmt.Errorf("odoo.edition must be %q or %q", EditionCommunity, EditionEnterprise)
	}
	if o.Enterprise.URL == "" {
		o.Enterprise.URL = DefaultEnterpriseURL
	}
	if o.Enterprise.Path == "" {
		o.Enterprise.Path = DefaultEnterprisePath
	}
	return nil
}

// SetEdition writes odoo.edition and odoo.enterprise to godoo.yaml, keeping
// comments. Empty fields in e are left as they are. The enterprise keys are
// kept in the order url, branch, path, however the file had them.
func SetEdition(root, edition string, e EnterpriseConfig) error {
	return Edit(root, func(doc *yaml.Node) error {
		odoo := MapChild(doc, "odoo", yaml.MappingNode)
		MapChild(odoo, "edition", yaml.ScalarNode).Value = edition
		ent := MapChild(odoo, "enterprise", yaml.MappingNode)
		set := func(key, value string, quoted bool) {
			if value != "" {
				v := MapChild(ent, key, yaml.ScalarNode)
				v.Value, v.Style = value, Scalar(value, quoted).Style
			}
		}
		set("url", e.URL, false)
		set("branch", e.Branch, true) // "18.0", not the number 18.0
		set("path", e.Path, false)
		orderKeys(ent, "url", "branch", "path")
		return nil
	})
}

// orderKeys moves the given keys of a mapping to the front, in that order;
// other keys follow unchanged. Key and value nodes (with their comments)
// move together.
func orderKeys(m *yaml.Node, keys ...string) {
	var front, rest []*yaml.Node
	for _, key := range keys {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == key {
				front = append(front, m.Content[i], m.Content[i+1])
			}
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if !slices.Contains(keys, m.Content[i].Value) {
			rest = append(rest, m.Content[i], m.Content[i+1])
		}
	}
	m.Content = append(front, rest...)
}
