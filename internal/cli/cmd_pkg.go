// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/nix"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/services"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// godoo pkg add|remove|list - python packages for this project's addons.
// They are stored in godoo.yaml:
//
//	nix.python_packages        nixpkgs attribute names
//	nix.local_python_packages  same, but only for env: local (--local)
//	nix.pypi_packages          pinned wheels from PyPI, for what nixpkgs lacks
//	                           (marked `local: true` with --local)

func cmdPkg(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: godoo pkg add|remove|list [NAME...]")
	}
	switch args[0] {
	case "add":
		return pkgAdd(ctx, args[1:])
	case "remove", "rm":
		return pkgRemove(ctx, args[1:])
	case "list", "ls":
		return pkgList(ctx, args[1:])
	}
	return fmt.Errorf("unknown pkg command %q (add, remove, list)", args[0])
}

func pkgAdd(ctx context.Context, args []string) error {
	fs := newFlags("pkg")
	local := fs.Bool("local", false, "only for env: local (dev tools, test helpers); left out in prod")
	names, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("usage: godoo pkg add NAME... (pip or nixpkgs names)")
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := addPythonPackages(ctx, p, names, false, *local); err != nil {
		return err
	}
	return applyChanges(ctx)
}

// addPythonPackages plans names (see pkgPlan) and writes the result to
// godoo.yaml. With partial=false nothing is written unless every package
// resolves; with partial=true the resolvable ones are added anyway.
// local=true writes to the local-only lists.
func addPythonPackages(ctx context.Context, p *project.Project, names []string, partial, local bool) error {
	if _, err := nix.WriteFlake(p); err != nil {
		return err
	}
	ui.Step("looking up %s (nixpkgs %s, then PyPI)", strings.Join(names, ", "), p.Cfg.Nix.PythonNixpkgs)
	plan, err := planPackages(ctx, p, names)
	if err != nil {
		return err
	}
	plan.print()
	if len(plan.unresolved) > 0 && !partial {
		return fmt.Errorf("nothing added: %s cannot be provided automatically", strings.Join(plan.unresolvedNames(), ", "))
	}
	if len(plan.nixpkgs) == 0 && len(plan.wheels) == 0 {
		return nil
	}

	return config.Edit(p.Root, func(root *yaml.Node) error {
		nixNode := config.MapChild(root, "nix", yaml.MappingNode)

		attrs := make([]string, len(plan.nixpkgs))
		for i, f := range plan.nixpkgs {
			attrs[i] = f.Attr
		}
		listName := "python_packages"
		if local {
			listName = "local_python_packages"
		}
		pkgs := config.MapChild(nixNode, listName, yaml.SequenceNode)
		pkgs.Style = 0 // one package per line, easier to read and diff
		config.SeqAdd(pkgs, attrs...)

		wheels := config.MapChild(nixNode, "pypi_packages", yaml.SequenceNode)
		wheels.Style = 0 // block style, even if it was "pypi_packages: []"
		for _, w := range plan.wheels {
			removeWheel(wheels, w.Name) // replace an older pin of the same package
			entry := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				config.Scalar("name", false), config.Scalar(w.Name, false),
				config.Scalar("version", false), config.Scalar(w.Version, true),
				config.Scalar("url", false), config.Scalar(w.URL, false),
				config.Scalar("hash", false), config.Scalar(w.Hash, false),
			}}
			if local {
				entry.Content = append(entry.Content, config.Scalar("local", false), config.Scalar("true", false))
			}
			wheels.Content = append(wheels.Content, entry)
		}
		return nil
	})
}

// removeWheel drops entries named name from a pypi_packages node.
func removeWheel(wheels *yaml.Node, name string) (removed bool) {
	kept := wheels.Content[:0]
	for _, entry := range wheels.Content {
		var w config.PypiPackage
		if entry.Decode(&w) == nil && normalizeName(w.Name) == normalizeName(name) {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	wheels.Content = kept
	return removed
}

func pkgRemove(ctx context.Context, args []string) error {
	names, _, err := parse(newFlags("pkg"), args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("usage: godoo pkg remove NAME...")
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	var removed []string
	err = config.Edit(p.Root, func(root *yaml.Node) error {
		nixNode := config.MapChild(root, "nix", yaml.MappingNode)
		for _, list := range []string{"python_packages", "local_python_packages"} {
			removed = append(removed, config.SeqRemove(config.MapChild(nixNode, list, yaml.SequenceNode), names...)...)
		}
		wheels := config.MapChild(nixNode, "pypi_packages", yaml.SequenceNode)
		for _, n := range names {
			if removeWheel(wheels, n) {
				removed = append(removed, n)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(removed) == 0 {
		return fmt.Errorf("none of %s is in godoo.yaml (see `godoo pkg list`)", strings.Join(names, ", "))
	}
	ui.Step("removed %s", strings.Join(removed, ", "))
	return applyChanges(ctx)
}

func pkgList(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("pkg"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	c := p.Cfg
	fmt.Printf("Odoo %s python packages (env: %s):\n", c.Odoo.Version, c.Env)
	fmt.Printf("  base     the set nixpkgs uses for its %q package (%s)\n", c.Nix.OdooPackage, c.Nix.PythonNixpkgs)
	if len(c.Profile.ExtraPython) > 0 {
		fmt.Printf("  godoo    %s (missing from that set for Odoo %s)\n", strings.Join(c.Profile.ExtraPython, ", "), c.Odoo.Version)
	}
	switch {
	case c.DevToolsOn():
		fmt.Printf("  dev      %s + tools %s (local only; nix.dev_tools)\n",
			strings.Join(config.DevPython, ", "), strings.Join(config.DevTools, ", "))
	case c.Local():
		fmt.Println("  dev      watchdog only (nix.dev_tools: false)")
	default:
		fmt.Println("  dev      none - env is prod")
	}

	shared, local := c.Nix.PythonPackages, c.Nix.LocalPythonPackages
	if len(shared)+len(local)+len(c.Nix.PypiPackages)+len(c.Nix.Packages)+len(c.Nix.LocalPackages) == 0 {
		fmt.Println("\nYour packages: none yet - add some with `godoo pkg add NAME` (or --local)")
		return nil
	}

	fmt.Println("\nYour packages:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if len(shared)+len(local) > 0 {
		if _, err := nix.WriteFlake(p); err != nil {
			return err
		}
		look, err := nix.LookupPython(ctx, p, append(append([]string{}, shared...), local...))
		if err != nil {
			return err
		}
		for _, f := range look.Found {
			fmt.Fprintf(w, "  %s\t%s\tnixpkgs%s\n", f.Attr, f.Version, localNote(slices.Contains(local, f.Requested), c))
		}
		for _, m := range look.Missing {
			fmt.Fprintf(w, "  %s\t-\tNOT FOUND in nixpkgs %s\n", m, c.Nix.PythonNixpkgs)
		}
	}
	for _, wheel := range c.Nix.PypiPackages {
		fmt.Fprintf(w, "  %s\t%s\tpypi wheel%s\n", wheel.Name, wheel.Version, localNote(wheel.Local, c))
	}
	for _, t := range c.Nix.Packages {
		fmt.Fprintf(w, "  %s\t\ttool\n", t)
	}
	for _, t := range c.Nix.LocalPackages {
		fmt.Fprintf(w, "  %s\t\ttool%s\n", t, localNote(true, c))
	}
	return w.Flush()
}

// localNote marks local-only entries, and says when prod leaves them out.
func localNote(local bool, c *config.Config) string {
	switch {
	case !local:
		return ""
	case c.Local():
		return ", local only"
	default:
		return ", local only (not in this prod env)"
	}
}

// applyChanges reloads godoo.yaml after an edit, rebuilds what changed
// (nix env, repos) and restarts odoo if it is running so it picks them up.
func applyChanges(ctx context.Context) error {
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return err
	}
	odooSvc, _ := services.Get(p, "odoo")
	if !services.IsRunning(odooSvc) {
		ui.Info("odoo is not running; changes apply on the next `godoo up`")
		return nil
	}
	ui.Step("restarting odoo")
	if err := services.Stop(odooSvc); err != nil {
		return err
	}
	return services.Start(ctx, odooSvc)
}
