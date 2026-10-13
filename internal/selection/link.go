// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package selection

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/Mechas-Heart/Godoo/internal/project"
)

// Source is a place modules come from: the private addons, the enterprise
// addons, or one repository. Sources are given in priority order: when two
// contain a module with the same name, the first one wins (doodba: private,
// then repositories, then core Odoo).
type Source struct {
	Name string   // Private, Enterprise or a repo name
	Dirs []string // absolute addons directories
}

// Plan is what addons.yaml selects, worked out without touching the disk.
type Plan struct {
	Links    map[string]string // module name -> absolute module directory
	Direct   []Source          // private/enterprise without an entry: used as a whole
	Warnings []string
}

// Linked returns the selected module names, sorted.
func (pl Plan) Linked() []string {
	names := make([]string, 0, len(pl.Links))
	for m := range pl.Links {
		names = append(names, m)
	}
	sort.Strings(names)
	return names
}

// MakePlan matches every source's modules against addons.yaml. Private and
// enterprise addons without an entry stay available as a whole (Direct);
// repositories without an entry are inactive, as in doodba.
func MakePlan(s *Selection, sources []Source) Plan {
	pl := Plan{Links: map[string]string{}}
	from := map[string]string{} // module -> source that provided it
	known := map[string]bool{}
	for _, src := range sources {
		known[src.Name] = true
		patterns, selected := s.Patterns[src.Name]
		if !selected {
			if src.Name == Private || src.Name == Enterprise {
				pl.Direct = append(pl.Direct, src)
			} else {
				pl.Warnings = append(pl.Warnings, fmt.Sprintf(
					"repo %q has no entry in %s, so none of its modules are available (add `%s: [\"*\"]` for all)",
					src.Name, FileName, src.Name))
			}
			continue
		}

		matched := map[string]bool{}
		for _, d := range src.Dirs {
			for _, module := range project.ModulesIn(d) {
				pattern, ok := firstMatch(patterns, module)
				if !ok {
					continue
				}
				matched[pattern] = true
				if prev, dup := from[module]; dup {
					pl.Warnings = append(pl.Warnings, fmt.Sprintf(
						"module %s is in both %s and %s; using %s", module, prev, src.Name, prev))
					continue
				}
				from[module] = src.Name
				pl.Links[module] = filepath.Join(d, module)
			}
		}
		for _, p := range patterns {
			if !matched[p] {
				pl.Warnings = append(pl.Warnings, fmt.Sprintf("%s: no module matches %q", src.Name, p))
			}
		}
	}

	for _, name := range s.Order {
		switch {
		case name == Core:
			pl.Warnings = append(pl.Warnings, fmt.Sprintf("%s: %q entries are ignored, core Odoo modules are always available", FileName, Core))
		case !known[name]:
			pl.Warnings = append(pl.Warnings, fmt.Sprintf("%s: %q is not available (not cloned, or enterprise edition is off)", FileName, name))
		}
	}
	return pl
}

// Apply rebuilds dir (.godoo/addons) as a folder of symlinks, one per
// selected module. An empty plan leaves no folder behind.
func Apply(dir string, pl Plan) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if len(pl.Links) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for module, target := range pl.Links {
		if err := os.Symlink(target, filepath.Join(dir, module)); err != nil {
			return err
		}
	}
	return nil
}

// firstMatch returns the first pattern matching module. Patterns use shell
// wildcards (*, ?, [abc]) like doodba's Python glob.
func firstMatch(patterns []string, module string) (string, bool) {
	for _, p := range patterns {
		if ok, _ := path.Match(p, module); ok {
			return p, true
		}
	}
	return "", false
}
