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
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// Addon repositories are cloned with --filter=blob:none: the full commit
// history (so `git pull --ff-only` and `git log` work as usual) but file
// contents are only downloaded for the checked-out branch.

// EnsureRepos clones every repo from godoo.yaml that is not there yet.
func EnsureRepos(ctx context.Context, p *project.Project) error {
	for _, r := range p.Cfg.Repos {
		if _, err := os.Stat(filepath.Join(p.Abs(r.Path), ".git")); err == nil {
			continue
		}
		if err := CloneRepo(ctx, p, r); err != nil {
			return err
		}
	}
	return nil
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
	ui.Info("updating %s (%s)", r.Name, r.Branch)
	_, err := git(ctx, p, dir, "pull", "--ff-only", "origin", r.Branch)
	return err
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

// ModulesIn lists the module directories (with a manifest) directly in dir.
func ModulesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var mods []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, manifest := range []string{"__manifest__.py", "__openerp__.py"} {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), manifest)); err == nil {
				mods = append(mods, e.Name())
				break
			}
		}
	}
	return mods
}

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
