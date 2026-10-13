// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package selection reads addons.yaml, doodba's format for choosing which
// modules of which repositories are active:
//
//	web:
//	  - web_responsive
//	server-tools:
//	  - "*date*"        # wildcards, as in doodba
//	---
//	ONLY:               # this document only when GODOO_ENV is local
//	  GODOO_ENV: [local]
//	web:
//	  - web_environment_ribbon
//
// Without addons.yaml nothing changes: every addons directory of the project
// is on addons_path as a whole. With it, the selected modules are symlinked
// into .godoo/addons (doodba: /opt/odoo/auto/addons) - see link.go.
package selection

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Mechas-Heart/Godoo/internal/config"
)

const (
	FileName = "addons.yaml"

	// Special entries, as in doodba.
	Private    = "private"    // the project's own addons (odoo.addons in godoo.yaml)
	Enterprise = "enterprise" // the enterprise addons (edition: enterprise)
	Core       = "odoo/addons"

	// DefaultRepoPattern is where repos named only in addons.yaml come from.
	DefaultRepoPattern = "https://github.com/OCA/{}.git"
)

// Selection is the merged content of addons.yaml (all documents whose ONLY
// conditions match).
type Selection struct {
	// Patterns maps a repo name (or Private / Enterprise) to the module
	// patterns selected from it.
	Patterns map[string][]string
	// Order lists the names in the order they first appear in the file.
	Order []string
	// Downloads holds, for each name, the ENV of the document that named it:
	// DEFAULT_REPO_PATTERN and ODOO_VERSION, used for repos only named here.
	Downloads map[string]Download
	// Skipped counts documents left out by ONLY.
	Skipped int

	env string // value of GODOO_ENV for ONLY
}

// Download is how to fetch a repo that is only named in addons.yaml.
type Download struct {
	Pattern string // e.g. https://github.com/OCA/{}.git
	Branch  string // ODOO_VERSION from ENV; empty = detect
}

// Has reports whether name has an entry.
func (s *Selection) Has(name string) bool {
	_, ok := s.Patterns[name]
	return ok
}

// Load reads <root>/addons.yaml. It returns nil, nil when there is none.
// env is the project's effective env (local/prod): ONLY sees it as
// GODOO_ENV even when that variable is not set in the shell.
func Load(root, env string) (*Selection, error) {
	f, err := os.Open(filepath.Join(root, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Selection{Patterns: map[string][]string{}, Downloads: map[string]Download{}, env: env}
	dec := yaml.NewDecoder(f)
	for doc := 1; ; doc++ {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			return s, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", FileName, err)
		}
		if err := s.addDocument(&node); err != nil {
			return nil, fmt.Errorf("%s, document %d: %w", FileName, doc, err)
		}
	}
}

// addDocument merges one YAML document (between --- lines) into s.
func (s *Selection) addDocument(doc *yaml.Node) error {
	if len(doc.Content) == 0 {
		return nil // empty document
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return errors.New("expected `repo: [modules]` entries")
	}

	var only map[string][]string
	env := map[string]string{}
	repos := map[string][]string{}
	var order []string
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, value := m.Content[i].Value, m.Content[i+1]
		var err error
		switch key {
		case "ONLY":
			err = value.Decode(&only)
		case "ENV":
			err = value.Decode(&env)
		default:
			var patterns []string
			err = value.Decode(&patterns)
			repos[key] = patterns
			order = append(order, key)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}

	if !s.onlyMatches(only) {
		s.Skipped++
		return nil
	}
	download := Download{Pattern: env["DEFAULT_REPO_PATTERN"], Branch: env["ODOO_VERSION"]}
	if download.Pattern == "" {
		download.Pattern = DefaultRepoPattern
	}
	for _, name := range order {
		if !s.Has(name) {
			s.Order = append(s.Order, name)
			s.Downloads[name] = download
		}
		s.Patterns[name] = append(s.Patterns[name], repos[name]...)
	}
	return nil
}

// onlyMatches implements doodba's ONLY: every listed environment variable
// must be set and have one of the listed values.
func (s *Selection) onlyMatches(only map[string][]string) bool {
	for name, allowed := range only {
		value, set := os.LookupEnv(name)
		if name == "GODOO_ENV" && s.env != "" {
			value, set = s.env, true
		}
		if !set || !slices.Contains(allowed, value) {
			return false
		}
	}
	return true
}

// ImplicitRepos are repositories named in addons.yaml but not in godoo.yaml
// `repos:` (and not a special entry). Like doodba, godoo downloads them from
// DEFAULT_REPO_PATTERN (OCA by default).
func (s *Selection) ImplicitRepos(c *config.Config) ([]config.Repo, error) {
	var out []config.Repo
	for _, name := range s.Order {
		if name == Private || name == Enterprise || name == Core {
			continue
		}
		if _, known := c.FindRepo(name); known {
			continue
		}
		d := s.Downloads[name]
		r, err := config.ParseRepoSpec(strings.ReplaceAll(d.Pattern, "{}", name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", FileName, err)
		}
		r.Name, r.Branch = name, d.Branch
		out = append(out, r)
	}
	return out, nil
}
