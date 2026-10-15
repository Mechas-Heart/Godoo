// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/selection"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// Partial checkouts. When addons.yaml picks some modules of a repo:
//
//	web:
//	  - web_responsive
//
// only those module folders (plus the repo's top-level files) are checked
// out, with git's sparse checkout in cone mode. Because repos are cloned
// with --filter=blob:none, the other modules' files are never downloaded.
//
// A repo checks out everything when there is no addons.yaml, when it has no
// entry there, or when its entry has "*". Changing addons.yaml changes the
// checkout on the next `godoo up` / `godoo setup` (see SyncRepoCheckout).

// partial reports whether addons.yaml selects only some modules of r.
func partial(sel *selection.Selection, r config.Repo) bool {
	if sel == nil {
		return false
	}
	patterns, ok := sel.Patterns[r.Name]
	return ok && !slices.Contains(patterns, "*")
}

// wantedModules lists the modules of the repo in dir that addons.yaml
// selects, from git's file list (no file contents needed).
func wantedModules(ctx context.Context, p *project.Project, sel *selection.Selection, r config.Repo, dir string) ([]string, error) {
	files, err := git(ctx, p, dir, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%s: list files: %w", r.Name, err)
	}
	var mods []string
	for _, module := range gitModules(files) {
		if selection.Matches(sel.Patterns[r.Name], module) {
			mods = append(mods, module)
		}
	}
	return mods, nil
}

// CloneAddonRepo clones an addon repo. When addons.yaml selects only some
// of its modules, only those are downloaded and checked out.
func CloneAddonRepo(ctx context.Context, p *project.Project, r config.Repo) error {
	sel, err := selection.Load(p.Root, p.Cfg.Env)
	if err != nil {
		return err
	}
	if !partial(sel, r) {
		return CloneRepo(ctx, p, r)
	}
	dir := p.Abs(r.Path)
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", dir)
	}
	ui.Info("cloning %s (branch %s) into %s, only the modules in %s", r.URL, r.Branch, r.Path, selection.FileName)
	return cloneSparse(ctx, p, sel, r)
}

// cloneSparse clones r into its (empty or missing) path with only the
// selected modules checked out.
func cloneSparse(ctx context.Context, p *project.Project, sel *selection.Selection, r config.Repo) error {
	dir := p.Abs(r.Path)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if _, err := git(ctx, p, "", "clone", "--filter=blob:none", "--no-checkout", "--single-branch", "--branch", r.Branch, r.URL, dir); err != nil {
		return fmt.Errorf("clone %s: %w (does branch %s exist?)", r.Name, err, r.Branch)
	}
	// marks a clone that only ever downloaded the selected modules
	if _, err := git(ctx, p, dir, "config", "godoo.partial", "true"); err != nil {
		return err
	}
	if err := setSparse(ctx, p, sel, r, false); err != nil {
		return err
	}
	// --no-checkout left the working tree empty: fill it (sparse) now.
	_, err := git(ctx, p, dir, "checkout", "--quiet", r.Branch)
	return err
}

// SyncRepoCheckout makes a cloned repo's working tree match addons.yaml:
// only the selected module folders, or everything (see partial).
//
// A repo that should be partial but holds the whole repo in .git (cloned
// before addons.yaml, or narrowed in place) is cloned again (see reclone),
// so the other modules' files leave the disk too. Clones made partial from
// the start carry `godoo.partial = true` in their git config.
func SyncRepoCheckout(ctx context.Context, p *project.Project, sel *selection.Selection, r config.Repo) error {
	dir := p.Abs(r.Path)
	sparse, _ := git(ctx, p, dir, "config", "--get", "core.sparseCheckout")
	on := sparse == "true"

	if !partial(sel, r) {
		if on {
			ui.Info("%s: checking out all modules", r.Name)
			if _, err := git(ctx, p, dir, "sparse-checkout", "disable"); err != nil {
				return err
			}
			// now everything gets downloaded: no longer a small clone
			_, _ = git(ctx, p, dir, "config", "--unset", "godoo.partial")
		}
		return nil
	}
	if small, _ := git(ctx, p, dir, "config", "--get", "godoo.partial"); small != "true" {
		if done, err := reclone(ctx, p, sel, r); done || err != nil {
			return err
		}
	}
	return setSparse(ctx, p, sel, r, on)
}

// setSparse checks out only the selected modules of the repo (on: sparse
// checkout is already enabled, so nothing to do when the list is the same).
func setSparse(ctx context.Context, p *project.Project, sel *selection.Selection, r config.Repo, on bool) error {
	dir := p.Abs(r.Path)
	mods, err := wantedModules(ctx, p, sel, r, dir)
	if err != nil {
		return err
	}
	if on {
		current, err := git(ctx, p, dir, "sparse-checkout", "list")
		if err != nil {
			return err
		}
		if slices.Equal(strings.Fields(current), mods) {
			return nil // already right
		}
	}
	if len(mods) == 0 {
		ui.Info("%s: no module matches %s, checking out none", r.Name, selection.FileName)
	} else {
		ui.Info("%s: checking out only %s", r.Name, strings.Join(mods, ", "))
	}
	_, err = git(ctx, p, dir, append([]string{"sparse-checkout", "set", "--cone"}, mods...)...)
	return err
}

// reclone replaces a whole clone by a fresh partial one, like an update:
// the new clone is at the latest commit of the same branch. It only does so
// when nothing local would be lost; otherwise it returns false and the repo
// is narrowed in place (its .git keeps the files already downloaded).
//
// The old clone is moved aside first and put back if the new clone fails
// (no network, no access), so a failure never leaves the repo missing.
func reclone(ctx context.Context, p *project.Project, sel *selection.Selection, r config.Repo) (bool, error) {
	dir := p.Abs(r.Path)
	if why := localWork(ctx, p, dir); why != "" {
		ui.Info("%s: keeping the full clone (%s); delete %s and run `godoo up` for the smaller download", r.Name, why, r.Path)
		return false, nil
	}
	branch, err := CurrentBranch(ctx, p, dir)
	if err != nil {
		return false, err
	}
	old := dir + ".godoo-old"
	if err := os.RemoveAll(old); err != nil { // left over from an interrupted run
		return false, err
	}
	if err := os.Rename(dir, old); err != nil {
		return false, err
	}
	ui.Info("%s: replacing the full clone with one of only the selected modules", r.Name)
	rr := r
	rr.Branch = branch
	if err := cloneSparse(ctx, p, sel, rr); err != nil {
		os.RemoveAll(dir)
		if restoreErr := os.Rename(old, dir); restoreErr != nil {
			return false, fmt.Errorf("%w; the old clone is in %s", err, old)
		}
		return false, fmt.Errorf("%w (kept the full clone)", err)
	}
	return true, os.RemoveAll(old)
}

// localWork says why a repo must not be deleted: local changes, commits not
// pushed, or stashes. "" means a fresh clone loses nothing.
func localWork(ctx context.Context, p *project.Project, dir string) string {
	if out, err := git(ctx, p, dir, "status", "--porcelain"); err != nil || out != "" {
		return "it has local changes"
	}
	if out, err := git(ctx, p, dir, "rev-list", "--count", "@{upstream}..HEAD"); err != nil {
		return "its branch has no upstream to compare with"
	} else if out != "0" {
		return "it has commits that are not pushed"
	}
	if out, err := git(ctx, p, dir, "stash", "list"); err != nil || out != "" {
		return "it has stashed changes"
	}
	return ""
}
