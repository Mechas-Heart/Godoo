// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/odoo"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/selection"
	"github.com/Mechas-Heart/Godoo/internal/services"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// godoo addons add|remove|update|list|deps - addon git repositories (OCA or
// any other), stored in godoo.yaml under `repos:`.

func cmdAddons(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: godoo addons add|remove|update|list|deps")
	}
	switch args[0] {
	case "add":
		return addonsAdd(ctx, args[1:])
	case "remove", "rm":
		return addonsRemove(ctx, args[1:])
	case "update", "pull":
		return addonsUpdate(ctx, args[1:])
	case "list", "ls":
		return addonsList(ctx, args[1:])
	case "deps":
		return addonsDeps(ctx, args[1:])
	}
	return fmt.Errorf("unknown addons command %q (add, remove, update, list, deps)", args[0])
}

func addonsAdd(ctx context.Context, args []string) error {
	fs := newFlags("addons")
	branch := fs.String("branch", "", "git branch (default: detected - 18.0, then 18, then the repo's default branch)")
	path := fs.String("path", "", "where to clone it (default: ./repos/<owner>/<name>)")
	deps := fs.Bool("deps", false, "also add the python packages the modules need")
	specs, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return errors.New("usage: godoo addons add REPO... (web, OCA/web, owner/repo or a git URL)")
	}
	if len(specs) > 1 && *path != "" {
		return errors.New("--path only works with a single repository")
	}
	p, err := openProject()
	if err != nil {
		return err
	}

	var added []config.Repo
	for _, spec := range specs {
		r, err := config.ParseRepoSpec(spec)
		if err != nil {
			return err
		}
		if existing, ok := p.Cfg.FindRepo(r.Name); ok {
			ui.Info("%s is already in godoo.yaml (%s)", existing.Name, existing.Path)
			continue
		}
		if *path != "" {
			r.Path = *path
		}
		ui.Step("adding %s", r.Name)
		r.Branch = *branch
		if r.Branch == "" {
			b, err := odoo.DetectBranch(ctx, p, r.URL, p.Cfg.Odoo.Version)
			if err != nil {
				return err
			}
			r.Branch = b.Name
			odoo.WarnFallback(b, r.URL, p.Cfg.Odoo.Version)
			ui.Info("branch %s", r.Branch)
		}
		// Clone first: a typo or missing branch never ends up in godoo.yaml.
		if err := odoo.CloneRepo(ctx, p, r); err != nil {
			return err
		}
		if err := config.Edit(p.Root, func(root *yaml.Node) error {
			repos := config.MapChild(root, "repos", yaml.SequenceNode)
			repos.Style = 0 // block style, even if it was "repos: []"
			repos.Content = append(repos.Content, repoNode(r))
			return nil
		}); err != nil {
			return err
		}
		ui.Info("%d modules in %s", len(odoo.ModulesIn(p.Abs(r.Path))), r.Path)
		added = append(added, r)
	}
	if len(added) == 0 {
		return nil
	}
	if sel, err := selection.Load(p.Root, p.Cfg.Env); err != nil {
		return err
	} else if sel != nil {
		for _, r := range added {
			if !sel.Has(r.Name) {
				ui.Warn("%s is in use: %s stays inactive until you list it there, e.g.\n  %s:\n    - \"*\"", selection.FileName, r.Name, r.Name)
			}
		}
	}

	// Make sure the env exists, then check the modules' python dependencies.
	p, err = openProject()
	if err != nil {
		return err
	}
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return err
	}
	dirs := make([]string, len(added))
	for i, r := range added {
		dirs[i] = p.Abs(r.Path)
	}
	if err := handleMissingDeps(ctx, p, dirs, *deps); err != nil {
		return err
	}
	return applyChanges(ctx)
}

func repoNode(r config.Repo) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		config.Scalar("name", false), config.Scalar(r.Name, false),
		config.Scalar("url", false), config.Scalar(r.URL, false),
		config.Scalar("branch", false), config.Scalar(r.Branch, true), // "17.0", not 17.0
		config.Scalar("path", false), config.Scalar(r.Path, false),
	}}
}

// handleMissingDeps reports python packages the modules in dirs need but the
// nix python lacks; with add=true it adds them via `godoo pkg add`.
func handleMissingDeps(ctx context.Context, p *project.Project, dirs []string, add bool) error {
	missing, err := odoo.MissingPythonDeps(ctx, p, dirs)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		ui.Info("all python dependencies of these modules are available")
		return nil
	}
	names := make([]string, 0, len(missing))
	for name := range missing {
		names = append(names, name)
	}
	sort.Strings(names)
	ui.Step("python packages these modules need")
	for _, name := range names {
		ui.Info("%-20s needed by %s", name, strings.Join(missing[name], ", "))
	}
	if !add {
		fmt.Printf("\nAdd them with:  godoo pkg add %s\n(or rerun with --deps)\n\n", strings.Join(names, " "))
		return nil
	}
	// Add what can be provided; report the rest instead of failing.
	return addPythonPackages(ctx, p, names, true, false)
}

func addonsRemove(ctx context.Context, args []string) error {
	fs := newFlags("addons")
	del := fs.Bool("delete", false, "also delete the cloned directory")
	force := fs.Bool("force", false, "remove even if modules from it are installed")
	names, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("usage: godoo addons remove NAME... [--delete]")
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	var targets []config.Repo
	for _, n := range names {
		r, ok := p.Cfg.FindRepo(n)
		if !ok {
			return fmt.Errorf("%q is not in repos (see `godoo addons list`)", n)
		}
		targets = append(targets, r)
	}
	if !*force {
		if err := refuseIfInstalled(ctx, p, targets); err != nil {
			return err
		}
	}
	err = config.Edit(p.Root, func(root *yaml.Node) error {
		repos := config.MapChild(root, "repos", yaml.SequenceNode)
		kept := repos.Content[:0]
		for _, entry := range repos.Content {
			if !matchesAny(entry, targets) {
				kept = append(kept, entry)
			}
		}
		repos.Content = kept
		return nil
	})
	if err != nil {
		return err
	}
	for _, r := range targets {
		ui.Step("removed %s from godoo.yaml", r.Name)
		if *del {
			ui.Info("deleting %s", r.Path)
			if err := os.RemoveAll(p.Abs(r.Path)); err != nil {
				return err
			}
		} else {
			ui.Info("kept %s on disk (use --delete to remove it)", r.Path)
		}
	}
	return applyChanges(ctx)
}

// refuseIfInstalled stops a removal that would leave installed modules
// without their code (checked in the default database, odoo.db).
func refuseIfInstalled(ctx context.Context, p *project.Project, repos []config.Repo) error {
	pg, _ := services.Get(p, "postgres")
	if !services.IsRunning(pg) {
		ui.Warn("postgres is not running, so installed modules were not checked")
		return nil
	}
	var mods []string
	for _, r := range repos {
		mods = append(mods, odoo.ModulesIn(p.Abs(r.Path))...)
	}
	if len(mods) == 0 {
		return nil
	}
	db := p.Cfg.Odoo.DB
	sql := fmt.Sprintf("SELECT name FROM ir_module_module WHERE state = 'installed' AND name IN ('%s') ORDER BY 1",
		strings.Join(mods, "','")) // module names are directory names: [a-z0-9_]
	installed, err := queryDB(ctx, p, db, sql)
	if err != nil {
		ui.Warn("could not check installed modules in %s: %v", db, err)
		return nil
	}
	if len(installed) > 0 {
		return fmt.Errorf("installed in database %s: %s\n  uninstall them first (Apps menu), or use --force", db, strings.Join(installed, ", "))
	}
	return nil
}

// active says how many modules of an addons directory Odoo can see:
// "all" when the whole directory is on addons_path, "N" when addons.yaml
// selects N of its modules, "none" otherwise.
func active(p *project.Project, addons odoo.Addons, dir string) string {
	if slices.Contains(addons.Path, dir) {
		return "all"
	}
	n := 0
	for _, m := range odoo.ModulesIn(dir) {
		if addons.Links[m] == filepath.Join(dir, m) {
			n++
		}
	}
	if n == 0 {
		return "none"
	}
	return fmt.Sprint(n)
}

// matchesAny reports whether a `repos:` yaml entry is one of targets. Entries
// may omit name, so compare the name derived the same way config does.
func matchesAny(entry *yaml.Node, targets []config.Repo) bool {
	var r config.Repo
	if err := entry.Decode(&r); err != nil {
		return false
	}
	if r.Name == "" {
		if parsed, err := config.ParseRepoSpec(r.URL); err == nil {
			r.Name = parsed.Name
		}
	}
	for _, t := range targets {
		if strings.EqualFold(r.Name, t.Name) {
			return true
		}
	}
	return false
}

func addonsUpdate(ctx context.Context, args []string) error {
	names, _, err := parse(newFlags("addons"), args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	all, err := odoo.AllRepos(p)
	if err != nil {
		return err
	}
	repos := all
	if len(names) > 0 {
		repos = nil
		for _, n := range names {
			i := slices.IndexFunc(all, func(r config.Repo) bool { return strings.EqualFold(r.Name, n) })
			if i < 0 {
				return fmt.Errorf("%q is not in repos or addons.yaml (see `godoo addons list`)", n)
			}
			repos = append(repos, all[i])
		}
	}
	if len(repos) == 0 {
		return errors.New("no repos yet; add one with `godoo addons add NAME`")
	}
	ui.Step("updating addon repos")
	var failed []string
	for _, r := range repos {
		if err := odoo.UpdateRepo(ctx, p, r); err != nil {
			ui.Warn("%s: %v", r.Name, err)
			failed = append(failed, r.Name)
		}
	}
	fmt.Println("\nCode changes are live after the restart; run `godoo update MODULES` for data/view changes.")
	if err := applyChanges(ctx); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("not updated: %s", strings.Join(failed, ", "))
	}
	return nil
}

func addonsList(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("addons"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	addons, err := odoo.ResolveAddons(p)
	if err != nil {
		return err
	}
	repos, err := odoo.AllRepos(p)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tBRANCH\tCOMMIT\tMODULES\tACTIVE\tPATH")
	for _, dir := range p.Cfg.Odoo.Addons {
		abs := p.Abs(dir)
		fmt.Fprintf(w, "(local)\t-\t-\t%d\t%s\t%s\n", len(odoo.ModulesIn(abs)), active(p, addons, abs), dir)
	}
	if dir := p.EnterpriseDir(); dir != "" {
		st := odoo.StatusOf(ctx, p, p.Cfg.EnterpriseRepo())
		fmt.Fprintf(w, "(enterprise)\t%s\t%s\t%d\t%s\t%s\n", p.Cfg.Odoo.Enterprise.Branch, st.Commit, st.Modules, active(p, addons, dir), p.Cfg.Odoo.Enterprise.Path)
	}
	for _, r := range repos {
		st := odoo.StatusOf(ctx, p, r)
		name := r.Name
		if _, inYaml := p.Cfg.FindRepo(r.Name); !inYaml {
			name += " (addons.yaml)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n", name, valueOr(r.Branch, "-"), st.Commit, st.Modules, active(p, addons, p.Abs(r.Path)), r.Path)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if addons.Selected {
		fmt.Printf("\nModule selection: %s (%d modules active, linked into .godoo/addons on `godoo up`)\n", selection.FileName, len(addons.Links))
	}
	return nil
}

func addonsDeps(ctx context.Context, args []string) error {
	fs := newFlags("addons")
	add := fs.Bool("add", false, "add the missing python packages")
	if _, _, err := parse(fs, args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return err
	}
	addons, err := odoo.ResolveAddons(p)
	if err != nil {
		return err
	}
	if err := handleMissingDeps(ctx, p, addons.ScanDirs(p), *add); err != nil {
		return err
	}
	if *add {
		return applyChanges(ctx)
	}
	return nil
}
