// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/odoo"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// godoo edition [community|enterprise] - which Odoo edition the project runs.
// Enterprise = community + the enterprise addons repository (odoo/enterprise
// or a company mirror), cloned to ./enterprise and put first in addons_path.

func cmdEdition(ctx context.Context, args []string) error {
	fs := newFlags("edition")
	repo := fs.String("repo", "", "enterprise repository URL (default: godoo.yaml, else "+config.DefaultEnterpriseURL+")")
	branch := fs.String("branch", "", "branch (default: detected - 18.0, then 18, then the repo's default branch)")
	path := fs.String("path", "", "where to clone it (default: "+config.DefaultEnterprisePath+")")
	force := fs.Bool("force", false, "switch to community even if enterprise modules are installed")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		showEdition(ctx, p)
		return nil
	}
	switch pos[0] {
	case config.EditionEnterprise:
		return editionEnterprise(ctx, p, *repo, *branch, *path)
	case config.EditionCommunity:
		return editionCommunity(ctx, p, *force)
	}
	return fmt.Errorf("unknown edition %q (community or enterprise)", pos[0])
}

func showEdition(ctx context.Context, p *project.Project) {
	c := p.Cfg
	fmt.Printf("edition: %s\n", c.Odoo.Edition)
	if !c.EnterpriseOn() {
		fmt.Println("  switch with: godoo edition enterprise [--repo URL]")
		return
	}
	e := c.Odoo.Enterprise
	state := "not cloned yet (godoo up clones it)"
	if st := odoo.StatusOf(ctx, p, c.EnterpriseRepo()); st.Modules > 0 {
		state = fmt.Sprintf("cloned, %d modules, %s", st.Modules, st.Commit)
	}
	fmt.Printf("  repo:   %s\n  branch: %s\n  path:   %s (%s)\n", e.URL, valueOr(e.Branch, "detected on first clone"), e.Path, state)
}

func editionEnterprise(ctx context.Context, p *project.Project, repo, branch, path string) error {
	c := p.Cfg
	e := c.Odoo.Enterprise // current values (defaults filled in)
	if repo != "" && repo != e.URL {
		e.URL, e.Branch = repo, "" // another repository: detect its branch again
	}
	if path != "" {
		e.Path = path
	}
	if branch != "" {
		e.Branch = branch
	}

	ui.Step("enterprise addons from %s", e.URL)
	dir := p.Abs(e.Path)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		ui.Info("using the existing checkout in %s", e.Path)
		if e.Branch == "" {
			current, err := odoo.CurrentBranch(ctx, p, dir)
			if err != nil {
				return err
			}
			e.Branch = current
		}
	} else {
		if e.Branch == "" {
			b, err := odoo.DetectBranch(ctx, p, e.URL, c.Odoo.Version)
			if err != nil {
				return err
			}
			e.Branch = b.Name
			odoo.WarnFallback(b, e.URL, c.Odoo.Version)
			ui.Info("branch %s", e.Branch)
		}
		// Clone first: a wrong URL or missing access never ends up in godoo.yaml.
		repo := config.Repo{Name: "enterprise", URL: e.URL, Branch: e.Branch, Path: e.Path}
		if err := odoo.CloneRepo(ctx, p, repo); err != nil {
			return err
		}
	}

	if err := config.SetEdition(p.Root, config.EditionEnterprise, e); err != nil {
		return err
	}
	ui.Info("godoo.yaml: edition enterprise, branch %s", e.Branch)
	if err := applyChanges(ctx); err != nil {
		return err
	}
	fmt.Printf("\nNew databases (godoo db create) are Enterprise.\n"+
		"An existing Community database: godoo install web_enterprise -d %s\n", c.Odoo.DB)
	return nil
}

func editionCommunity(ctx context.Context, p *project.Project, force bool) error {
	if !p.Cfg.EnterpriseOn() {
		fmt.Println("edition is already community")
		return nil
	}
	if !force {
		// A database with enterprise modules breaks without their code.
		if err := refuseIfInstalled(ctx, p, []config.Repo{p.Cfg.EnterpriseRepo()}); err != nil {
			return err
		}
	}
	if err := config.SetEdition(p.Root, config.EditionCommunity, config.EnterpriseConfig{}); err != nil {
		return err
	}
	ui.Step("edition community")
	ui.Info("kept %s on disk; `godoo edition enterprise` switches back", p.Cfg.Odoo.Enterprise.Path)
	return applyChanges(ctx)
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
