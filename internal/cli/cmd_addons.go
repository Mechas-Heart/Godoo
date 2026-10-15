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
	modulesFlag := fs.String("modules", "", "download and use only these modules (comma separated, wildcards allowed)")
	all := fs.Bool("all", false, "download and use all of the repo's modules")
	deps := fs.Bool("deps", false, "also add the python packages the modules need")
	specs, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return errors.New("usage: godoo addons add REPO... [--modules a,b | --all] (web, OCA/web, owner/repo or a git URL)")
	}
	if len(specs) > 1 && (*path != "" || *modulesFlag != "") {
		return errors.New("--path and --modules only work with a single repository")
	}
	modules := splitList(*modulesFlag)
	if *all {
		if len(modules) > 0 {
			return errors.New("use --modules or --all, not both")
		}
		modules = []string{"*"}
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	sel, err := selection.Load(p.Root, p.Cfg.Env)
	if err != nil {
		return err
	}

	// Adding a repo only records where it comes from (godoo.yaml) and which
	// of its modules to use (addons.yaml). Nothing is downloaded here: `godoo
	// up` (run below) downloads the selected modules, and none when nothing
	// is selected yet.
	var added []config.Repo
	for _, spec := range specs {
		r, err := config.ParseRepoSpec(spec)
		if err != nil {
			return err
		}
		existing, known := p.Cfg.FindRepo(r.Name)
		if known {
			r = existing
		} else {
			if *path != "" {
				r.Path = *path
			}
			ui.Step("adding %s", r.Name)
			if err := chooseBranch(ctx, p, &r, *branch); err != nil {
				return err
			}
		}

		undo, err := selectModules(p, sel, r.Name, modules)
		if err != nil {
			return err
		}
		if known {
			continue
		}
		if err := config.Edit(p.Root, func(root *yaml.Node) error {
			repos := config.MapChild(root, "repos", yaml.SequenceNode)
			repos.Style = 0 // block style, even if it was "repos: []"
			repos.Content = append(repos.Content, repoNode(r))
			return nil
		}); err != nil {
			return undoAnd(undo, err)
		}
		ui.Info("added %s to godoo.yaml (%s, branch %s)", r.Name, r.URL, r.Branch)
		if len(modules) == 0 {
			ui.Info("nothing downloaded yet; pick modules with `godoo addons add %s --modules a,b` (or --all)", r.Name)
		}
		added = append(added, r)
	}

	// Download what is selected, then check the modules' python dependencies.
	if err := applyChanges(ctx); err != nil {
		return err
	}
	var dirs []string
	for _, r := range added {
		if dir := p.Abs(r.Path); len(odoo.ModulesIn(dir)) > 0 {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return nil
	}
	p, err = openProject()
	if err != nil {
		return err
	}
	return handleMissingDeps(ctx, p, dirs, *deps)
}

// chooseBranch sets r.Branch: the given one after checking it exists, or the
// detected one. Both only ask the remote (git ls-remote), so a wrong URL or
// branch is caught here without downloading anything.
func chooseBranch(ctx context.Context, p *project.Project, r *config.Repo, given string) error {
	if given != "" {
		r.Branch = given
		return odoo.CheckBranch(ctx, p, r.URL, given)
	}
	b, err := odoo.DetectBranch(ctx, p, r.URL, p.Cfg.Odoo.Version)
	if err != nil {
		return err
	}
	r.Branch = b.Name
	odoo.WarnFallback(b, r.URL, p.Cfg.Odoo.Version)
	return nil
}

// selectModules writes repo's entry to addons.yaml (all modules when none
// are given) and says what changed. A new addons.yaml keeps every repo
// already in godoo.yaml whole.
func selectModules(p *project.Project, sel *selection.Selection, repo string, modules []string) (func(), error) {
	var whole []string
	for _, r := range p.Cfg.Repos {
		whole = append(whole, r.Name)
	}
	change, undo, err := selection.Select(p.Root, repo, modules, whole)
	if err != nil {
		return nil, err
	}
	what := strings.Join(modules, ", ")
	switch {
	case len(modules) == 0:
		what = "no modules yet"
	case slices.Contains(modules, "*"):
		what = "all modules"
	}
	switch change {
	case selection.Created:
		ui.Info("created %s: %s: %s", selection.FileName, repo, what)
		if len(whole) > 0 {
			ui.Info("the other repos stay whole there (\"*\"): %s", strings.Join(whole, ", "))
		}
	case selection.Added:
		ui.Info("%s: %s: %s", selection.FileName, repo, what)
	case selection.Extended:
		var fresh []string
		for _, m := range modules {
			if sel == nil || !slices.Contains(sel.Patterns[repo], m) {
				fresh = append(fresh, m)
			}
		}
		if slices.Contains(modules, "*") {
			fresh = []string{"all modules"}
		}
		ui.Info("%s: added %s to %s", selection.FileName, strings.Join(fresh, ", "), repo)
	case selection.Unchanged:
		if len(modules) > 0 {
			ui.Info("%s already selects %s from %s", selection.FileName, what, repo)
		} else {
			ui.Info("%s is already added (see `godoo addons list`)", repo)
		}
	}
	return undo, nil
}

// undoAnd restores addons.yaml (when it was changed) and returns err.
func undoAnd(undo func(), err error) error {
	if undo != nil {
		undo()
	}
	return err
}

// splitList splits "a, b,c" into [a b c].
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
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
	del := fs.Bool("delete", false, "also delete the cloned directory (refused when it has work that is not pushed)")
	force := fs.Bool("force", false, "remove even if modules from it are installed or it has unpushed work")
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
	// a repo is in godoo.yaml, or only named in addons.yaml
	all, err := odoo.AllRepos(p)
	if err != nil {
		return err
	}
	var targets []config.Repo
	for _, n := range names {
		i := slices.IndexFunc(all, func(r config.Repo) bool { return strings.EqualFold(r.Name, n) })
		if i < 0 {
			return fmt.Errorf("%q is not in repos or addons.yaml (see `godoo addons list`)", n)
		}
		targets = append(targets, all[i])
	}
	if !*force {
		if err := refuseIfInstalled(ctx, p, targets); err != nil {
			return err
		}
		if *del {
			for _, r := range targets {
				if _, err := os.Stat(filepath.Join(p.Abs(r.Path), ".git")); err != nil {
					continue // not cloned: nothing to lose
				}
				if why := odoo.LocalWork(ctx, p, p.Abs(r.Path)); why != "" {
					return fmt.Errorf("not deleting %s: %s (use --force to delete anyway)", r.Path, why)
				}
			}
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
		if _, inYaml := p.Cfg.FindRepo(r.Name); inYaml {
			ui.Step("removed %s from godoo.yaml", r.Name)
		}
		if removed, err := selection.Deselect(p.Root, r.Name); err != nil {
			return err
		} else if removed {
			ui.Info("removed %s from %s", r.Name, selection.FileName)
		}
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
		if _, err := os.Stat(filepath.Join(p.Abs(r.Path), ".git")); err != nil {
			ui.Info("%s: not downloaded, nothing to update", r.Name)
			continue
		}
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
	sel, err := selection.Load(p.Root, p.Cfg.Env)
	if err != nil {
		return err
	}
	for _, r := range repos {
		st := odoo.StatusOf(ctx, p, r)
		if st.Commit == odoo.NotCloned && !odoo.Wanted(sel, r) {
			st.Commit = "not downloaded (no modules selected)"
		}
		name := r.Name
		if _, inYaml := p.Cfg.FindRepo(r.Name); !inYaml {
			name += " (addons.yaml)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", name, valueOr(r.Branch, "-"), st.Commit, st.ModulesColumn(), active(p, addons, p.Abs(r.Path)), r.Path)
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
