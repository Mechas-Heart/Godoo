// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"path/filepath"
	"slices"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/selection"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// Addons is the resolved addons_path and what happened while resolving it.
type Addons struct {
	Path     []string
	Selected bool              // addons.yaml is in use
	Links    map[string]string // addons.yaml: module -> directory, linked into .godoo/addons
	Warnings []string
}

// ScanDirs are the directories to scan for module dependencies: the
// addons_path without core Odoo, but with the selected module directories
// instead of the .godoo/addons links.
func (a Addons) ScanDirs(p *project.Project) []string {
	var dirs []string
	for _, d := range a.Path {
		if d != coreDir(p) && d != p.LinkedAddonsDir() {
			dirs = append(dirs, d)
		}
	}
	for _, module := range sortedValues(a.Links) {
		dirs = append(dirs, module)
	}
	return dirs
}

func sortedValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// ApplyLinks writes the .godoo/addons symlinks for a (a no-op cleanup when
// addons.yaml is not used). Only WriteConf calls it, so read-only commands
// never change what a running Odoo sees.
func ApplyLinks(p *project.Project, a Addons) error {
	return selection.Apply(p.LinkedAddonsDir(), selection.Plan{Links: a.Links})
}

func coreDir(p *project.Project) string { return filepath.Join(p.SourceDir(), "addons") }

// ResolveAddons computes addons_path. Odoo uses the first directory that has
// a module, so the order decides which copy of a duplicated module wins.
//
// Without addons.yaml (godoo's default):
//
//	enterprise, core odoo, your addons dirs, every repo
//
// With addons.yaml (doodba style), only the selected repo modules, which are
// symlinked into .godoo/addons:
//
//	your addons dirs (no "private" entry), .godoo/addons, enterprise (no
//	"enterprise" entry), core odoo
//
// Directories without any module are left out: Odoo refuses to start with
// them.
//
// ResolveAddons only computes; ApplyLinks writes the symlinks.
func ResolveAddons(p *project.Project) (Addons, error) {
	sel, err := selection.Load(p.Root, p.Cfg.Env)
	if err != nil {
		return Addons{}, err
	}
	if sel == nil {
		return wholeDirs(p), nil
	}

	implicit, err := sel.ImplicitRepos(p.Cfg)
	if err != nil {
		return Addons{}, err
	}
	res := selection.MakePlan(sel, sources(p, implicit))
	a := Addons{Selected: true, Links: res.Links, Warnings: res.Warnings}
	direct := map[string][]string{}
	for _, src := range res.Direct {
		direct[src.Name] = src.Dirs
	}
	a.add(direct[selection.Private]...) // your own addons win, as in doodba
	if len(res.Links) > 0 {
		a.Path = append(a.Path, p.LinkedAddonsDir()) // filled by ApplyLinks
	}
	a.add(direct[selection.Enterprise]...)
	a.Path = append(a.Path, coreDir(p))
	return a, nil
}

// wholeDirs is the behaviour without addons.yaml.
func wholeDirs(p *project.Project) Addons {
	var a Addons
	if dir := p.EnterpriseDir(); dir != "" {
		a.Path = append(a.Path, dir) // Odoo's recommended order: enterprise first
	}
	a.Path = append(a.Path, coreDir(p))
	a.add(p.AddonsPaths()...)
	return a
}

// add appends the directories that contain at least one module.
func (a *Addons) add(dirs ...string) {
	for _, d := range dirs {
		if len(ModulesIn(d)) > 0 {
			a.Path = append(a.Path, d)
		} else {
			ui.Debug("addons_path: skipping %s (no modules)", d)
		}
	}
}

// sources lists where modules come from, in doodba's priority order:
// private addons, enterprise, then repositories (godoo.yaml order, then the
// ones only named in addons.yaml).
func sources(p *project.Project, implicit []config.Repo) []selection.Source {
	var private []string
	for _, dir := range p.Cfg.Odoo.Addons {
		private = append(private, p.Abs(dir))
	}
	out := []selection.Source{{Name: selection.Private, Dirs: private}}
	if dir := p.EnterpriseDir(); dir != "" {
		out = append(out, selection.Source{Name: selection.Enterprise, Dirs: []string{dir}})
	}
	for _, r := range append(slices.Clone(p.Cfg.Repos), implicit...) {
		out = append(out, selection.Source{Name: r.Name, Dirs: []string{p.Abs(r.Path)}})
	}
	return out
}
