// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/selection"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// Addon repositories are cloned with --filter=blob:none: the full commit
// history (so `git pull --ff-only` and `git log` work as usual) but file
// contents are only downloaded for the checked-out branch.

// AllRepos is godoo.yaml `repos:` plus the repositories only named in
// addons.yaml, which doodba downloads automatically (from OCA by default).
func AllRepos(p *project.Project) ([]config.Repo, error) {
	sel, err := selection.Load(p.Root, p.Cfg.Env)
	if err != nil || sel == nil {
		return p.Cfg.Repos, err
	}
	implicit, err := sel.ImplicitRepos(p.Cfg)
	if err != nil {
		return nil, err
	}
	return append(slices.Clone(p.Cfg.Repos), implicit...), nil
}

// EnsureRepos clones every repo (see AllRepos) that is not there yet. A repo
// without a branch (only named in addons.yaml) gets one detected.
func EnsureRepos(ctx context.Context, p *project.Project) error {
	repos, err := AllRepos(p)
	if err != nil {
		return err
	}
	for _, r := range repos {
		if _, err := os.Stat(filepath.Join(p.Abs(r.Path), ".git")); err == nil {
			continue
		}
		if r.Branch == "" {
			b, err := DetectBranch(ctx, p, r.URL, p.Cfg.Odoo.Version)
			if err != nil {
				return err
			}
			r.Branch = b.Name
			WarnFallback(b, r.URL, p.Cfg.Odoo.Version)
		}
		if err := CloneRepo(ctx, p, r); err != nil {
			return err
		}
	}
	return nil
}

// EnsureEnterprise clones the enterprise addons when the edition is
// enterprise and they are not there yet. Without a branch in godoo.yaml it
// detects one (see DetectBranch) and saves it, so teammates get the same.
func EnsureEnterprise(ctx context.Context, p *project.Project) error {
	c := p.Cfg
	r := c.EnterpriseRepo()
	if _, err := os.Stat(filepath.Join(p.Abs(r.Path), ".git")); err == nil {
		return nil
	}
	detected := r.Branch == ""
	if detected {
		b, err := DetectBranch(ctx, p, r.URL, c.Odoo.Version)
		if err != nil {
			return err
		}
		r.Branch = b.Name
		WarnFallback(b, r.URL, c.Odoo.Version)
	}
	if err := CloneRepo(ctx, p, r); err != nil {
		return err
	}
	if detected { // saved only after a successful clone
		if err := config.SetEdition(p.Root, config.EditionEnterprise, config.EnterpriseConfig{Branch: r.Branch}); err != nil {
			return err
		}
		ui.Info("saved enterprise branch %s to godoo.yaml", r.Branch)
	}
	return nil
}

// WarnFallback tells the user when DetectBranch had to use the default branch.
func WarnFallback(b Branch, url, version string) {
	if b.Fallback {
		ui.Warn("%s has no %s branch; using its default branch %q - make sure it supports Odoo %s",
			url, strings.Join(branchCandidates(version), " or "), b.Name, version)
	}
}

// CloneRepo clones one repo at its branch into its path.
func CloneRepo(ctx context.Context, p *project.Project, r config.Repo) error {
	dir := p.Abs(r.Path)
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", dir)
	}
	ui.Info("cloning %s (branch %s) into %s", r.URL, r.Branch, r.Path)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	_, err := git(ctx, p, "", "clone", "--filter=blob:none", "--single-branch", "--branch", r.Branch, r.URL, dir)
	if err != nil {
		return fmt.Errorf("clone %s: %w (does branch %s exist?)", r.Name, err, r.Branch)
	}
	return nil
}

// UpdateRepo fast-forwards a repo to the latest commit of its branch. It
// refuses when there are local changes rather than overwrite them.
func UpdateRepo(ctx context.Context, p *project.Project, r config.Repo) error {
	dir := p.Abs(r.Path)
	if dirty, err := git(ctx, p, dir, "status", "--porcelain"); err != nil {
		return err
	} else if dirty != "" {
		return fmt.Errorf("%s has local changes; commit or stash them first", r.Path)
	}
	branch := r.Branch
	if branch == "" { // only named in addons.yaml: the branch it was cloned with
		current, err := CurrentBranch(ctx, p, dir)
		if err != nil {
			return err
		}
		branch = current
	}
	ui.Info("updating %s (%s)", r.Name, branch)
	_, err := git(ctx, p, dir, "pull", "--ff-only", "origin", branch)
	return err
}

// CurrentBranch returns the branch checked out in dir.
func CurrentBranch(ctx context.Context, p *project.Project, dir string) (string, error) {
	return git(ctx, p, dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// RepoStatus is a one-line summary for `godoo addons list`.
type RepoStatus struct {
	Commit  string // short hash and date, or "not cloned"
	Modules int
}

func StatusOf(ctx context.Context, p *project.Project, r config.Repo) RepoStatus {
	dir := p.Abs(r.Path)
	commit, err := git(ctx, p, dir, "log", "-1", "--format=%h %cs")
	if err != nil {
		return RepoStatus{Commit: "not cloned (run godoo setup)"}
	}
	return RepoStatus{Commit: commit, Modules: len(ModulesIn(dir))}
}

// ModulesIn lists the module directories directly in dir (see project.ModulesIn).
func ModulesIn(dir string) []string { return project.ModulesIn(dir) }

// git runs git from the nix env (falling back to the system git) and
// returns its trimmed stdout. Progress output goes to the terminal.
func git(ctx context.Context, p *project.Project, dir string, args ...string) (string, error) {
	bin := p.EnvBin("git")
	if _, err := os.Stat(bin); err != nil {
		bin = "git"
	}
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	ui.Debug("git %s", strings.Join(args, " "))
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = &out, os.Stderr
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}
