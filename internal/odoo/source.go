// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package odoo prepares and runs Odoo itself: the source checkout, odoo.conf,
// the odoo-bin command line and the requirements check.
package odoo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// EnsureSource clones odoo.repo at the version branch into odoo.source,
// unless a checkout is already there. Point odoo.source at an existing
// checkout to share one clone between several projects.
func EnsureSource(ctx context.Context, p *project.Project) error {
	dir := p.SourceDir()
	if _, err := os.Stat(filepath.Join(dir, "odoo-bin")); err == nil {
		ui.Info("odoo source: %s", dir)
		return nil
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s exists but has no odoo-bin; fix or remove it", dir)
	}

	c := p.Cfg
	ui.Info("cloning %s (branch %s) into %s", c.Odoo.Repo, c.Odoo.Version, dir)
	ui.Info("shallow clone (--depth 1); this can take a few minutes")
	git := p.EnvBin("git")
	if _, err := os.Stat(git); err != nil {
		git = "git"
	}
	args := []string{"clone", "--depth", "1", "--branch", c.Odoo.Version, c.Odoo.Repo, dir}
	ui.Debug("%s %v", git, args)
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}
