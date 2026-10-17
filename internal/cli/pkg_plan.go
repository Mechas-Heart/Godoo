// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/nix"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/pypi"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// pkgPlan is what `godoo pkg add` is about to write to godoo.yaml.
//
// For every requested name:
//  1. already in Odoo's package set     -> nothing to do
//  2. in nixpkgs (any common spelling)  -> nix.python_packages
//  3. pure-python wheel on PyPI         -> nix.pypi_packages, and its own
//     dependencies go through the same steps
//  4. otherwise                         -> unresolved (needs a nix derivation)
//
// NAME==VERSION asks for that exact version. When Odoo's set or nixpkgs has
// it, steps 1-2 apply as usual; otherwise that version's wheel is pinned
// from PyPI, and the generated flake uses it instead of the package of the
// same name in Odoo's set (see flake.nix.tmpl, `pinned`).
type pkgPlan struct {
	nixpkgs    []nix.PythonPackage
	wheels     []pypi.Release
	inBase     []string
	unresolved map[string]string // name -> reason
	neededBy   map[string]string // dependency -> package that pulled it in
	replaces   map[string]string // wheel name -> "3.0.1 from odoo's packages"
}

// splitSpec splits "PyPDF2==2.12.1" into name and version ("" = any).
func splitSpec(spec string) (name, version string, err error) {
	name, version, pinned := strings.Cut(spec, "==")
	name, version = strings.TrimSpace(name), strings.TrimSpace(version)
	switch {
	case strings.ContainsAny(name, "<>=!~ "):
		return "", "", fmt.Errorf("%q: only NAME or NAME==VERSION are supported", spec)
	case pinned && version == "":
		return "", "", fmt.Errorf("%q: missing version after ==", spec)
	}
	return name, version, nil
}

// maxDepth bounds how far wheel dependencies are followed.
const maxDepth = 5

func planPackages(ctx context.Context, p *project.Project, specs []string) (pkgPlan, error) {
	plan := pkgPlan{unresolved: map[string]string{}, neededBy: map[string]string{}, replaces: map[string]string{}}
	seen := map[string]bool{}
	want := map[string]string{} // normalized name -> requested version
	var queue []string
	for _, spec := range specs {
		name, version, err := splitSpec(spec)
		if err != nil {
			return plan, err
		}
		if version != "" {
			want[normalizeName(name)] = version
		}
		queue = append(queue, name)
	}

	for depth := 0; len(queue) > 0 && depth < maxDepth; depth++ {
		var batch []string
		for _, n := range queue {
			if key := normalizeName(n); !seen[key] {
				seen[key] = true
				batch = append(batch, n)
			}
		}
		queue = nil
		if len(batch) == 0 {
			break
		}

		look, err := nix.LookupPython(ctx, p, batch)
		if err != nil {
			return plan, err
		}
		var fromPypi []string
		for _, f := range look.Found {
			if v := want[normalizeName(f.Requested)]; v != "" && v != f.Version {
				where := "nixpkgs"
				if look.InBase(f.Requested) {
					where = "odoo's packages"
				}
				ui.Debug("%s: nixpkgs has %s, asking PyPI for %s", f.Requested, f.Version, v)
				plan.replaces[normalizeName(f.Requested)] = f.Version + " from " + where
				fromPypi = append(fromPypi, f.Requested)
				continue
			}
			if look.InBase(f.Requested) {
				plan.inBase = append(plan.inBase, f.Requested)
				continue
			}
			plan.nixpkgs = append(plan.nixpkgs, f)
		}
		for _, name := range append(look.Missing, fromPypi...) {
			ui.Debug("%s is not in nixpkgs, asking PyPI", name)
			rel, err := pypi.Get(ctx, name, want[normalizeName(name)])
			if err != nil {
				plan.unresolved[name] = err.Error()
				continue
			}
			plan.wheels = append(plan.wheels, rel)
			for _, dep := range pypi.Dependencies(rel.RequiresDist, look.PythonVersion) {
				if !seen[normalizeName(dep)] {
					plan.neededBy[dep] = rel.Name
					queue = append(queue, dep)
				}
			}
		}
	}
	return plan, nil
}

// print shows the plan, one line per package.
func (plan pkgPlan) print() {
	note := func(name string) string {
		if by := plan.neededBy[name]; by != "" {
			return "  (needed by " + by + ")"
		}
		return ""
	}
	for _, f := range plan.nixpkgs {
		ui.Info("%-22s nixpkgs  %s %s%s", f.Requested, f.Attr, f.Version, note(f.Requested))
	}
	for _, w := range plan.wheels {
		replaces := ""
		if r := plan.replaces[normalizeName(w.Name)]; r != "" {
			replaces = ", replaces " + r
		}
		ui.Info("%-22s pypi     %s (pinned wheel%s)%s", w.Name, w.Version, replaces, note(w.Name))
	}
	for _, w := range plan.wheels {
		if r := plan.replaces[normalizeName(w.Name)]; strings.HasSuffix(r, "odoo's packages") {
			ui.Warn("%s %s replaces the version in Odoo's package set; `godoo pkg remove %s` goes back to it", w.Name, w.Version, w.Name)
		}
	}
	for _, n := range plan.inBase {
		if plan.neededBy[n] == "" { // only mention what the user asked for
			ui.Info("%-22s already part of odoo's packages", n)
		}
	}
	for _, n := range plan.unresolvedNames() {
		ui.Warn("%s: %s%s", n, plan.unresolved[n], note(n))
	}
}

func (plan pkgPlan) unresolvedNames() []string {
	names := make([]string, 0, len(plan.unresolved))
	for n := range plan.unresolved {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// normalizeName makes "Odoo_RPC", "odoo-rpc" and "odoo.rpc" compare equal,
// like PyPI does.
func normalizeName(n string) string {
	return strings.NewReplacer("_", "-", ".", "-").Replace(strings.ToLower(n))
}
