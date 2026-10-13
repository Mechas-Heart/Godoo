// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"os"

	"github.com/Mechas-Heart/Godoo/internal/nix"
	"github.com/Mechas-Heart/Godoo/internal/odoo"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

func cmdSetup(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("setup"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	return setup(ctx, p, setupOptions{rebuildEnv: true})
}

func cmdNixUpdate(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("nix-update"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	ui.Step("updating nix/flake.lock")
	if err := nix.Update(ctx, p); err != nil {
		return err
	}
	return setup(ctx, p, setupOptions{rebuildEnv: true})
}

type setupOptions struct {
	rebuildEnv bool // run `nix build` even if flake.nix is unchanged
}

// setup brings a project to a runnable state. Each step is skipped when it is
// already up to date, so `godoo up` can call it every time cheaply.
//
//  1. nix/flake.nix rendered from godoo.yaml
//  2. nix build -> .godoo/env (python with odoo's packages, postgres, nginx, ...)
//  3. odoo source cloned into odoo.source
//  4. enterprise addons cloned (edition: enterprise)
//  5. addon repos from `repos:` cloned into their paths
func setup(ctx context.Context, p *project.Project, o setupOptions) error {
	if err := p.EnsureDirs(); err != nil {
		return err
	}

	ui.Step("nix environment")
	changed, err := nix.WriteFlake(p)
	if err != nil {
		return err
	}
	if changed {
		ui.Info("nix/flake.nix updated from godoo.yaml")
	}
	if _, statErr := os.Stat(p.EnvDir()); changed || o.rebuildEnv || statErr != nil {
		ui.Info("building (downloads only what is not in /nix/store yet)")
		if err := nix.BuildEnv(ctx, p); err != nil {
			return err
		}
	}
	ui.Info("env: %s", p.EnvDir())

	ui.Step("odoo source")
	if err := odoo.EnsureSource(ctx, p); err != nil {
		return err
	}
	if p.Cfg.EnterpriseOn() {
		ui.Step("enterprise addons")
		if err := odoo.EnsureEnterprise(ctx, p); err != nil {
			return err
		}
	}
	repos, err := odoo.AllRepos(p)
	if err != nil {
		return err
	}
	if len(repos) > 0 {
		ui.Step("addon repos")
		return odoo.EnsureRepos(ctx, p)
	}
	return nil
}
