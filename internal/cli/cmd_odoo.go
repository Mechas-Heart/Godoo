// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/odoo"
	"github.com/Mechas-Heart/Godoo/internal/proc"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/services"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// prepareOdoo makes sure odoo can run a one-off command: the nix env exists,
// postgres (and mailpit) are up and odoo.conf is current. The odoo server
// itself may or may not be running; one-off commands use --no-http.
func prepareOdoo(ctx context.Context, p *project.Project) error {
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return err
	}
	if err := services.Up(ctx, p, "odoo", "nginx"); err != nil {
		return err
	}
	return odoo.WriteConf(p)
}

// runOdoo runs odoo-bin attached to the terminal and waits for it.
func runOdoo(ctx context.Context, p *project.Project, args ...string) error {
	argv := odoo.Command(p, args...)
	ui.Step("%s", strings.Join(argv, " "))
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = p.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func cmdOdoo(ctx context.Context, args []string) error {
	pos, extra, err := parse(newFlags("odoo"), args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := prepareOdoo(ctx, p); err != nil {
		return err
	}
	return runOdoo(ctx, p, append(pos, extra...)...)
}

func cmdShell(ctx context.Context, args []string) error {
	fs := newFlags("shell")
	db := fs.String("d", "", "database (default: odoo.db)")
	_, extra, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := prepareOdoo(ctx, p); err != nil {
		return err
	}
	return runOdoo(ctx, p, append([]string{"shell", "-d", dbOrDefault(p, *db)}, extra...)...)
}

func cmdInstall(ctx context.Context, args []string) error {
	fs := newFlags("install")
	db := fs.String("d", "", "database (default: odoo.db)")
	mods, extra, err := parseModules(fs, args)
	if err != nil {
		return err
	}
	return runModules(ctx, *db, append([]string{"-i", mods}, extra...))
}

func cmdUpdate(ctx context.Context, args []string) error {
	fs := newFlags("update")
	db := fs.String("d", "", "database (default: odoo.db)")
	mods, extra, err := parseModules(fs, args)
	if err != nil {
		return err
	}
	return runModules(ctx, *db, append([]string{"-u", mods}, extra...))
}

func cmdTest(ctx context.Context, args []string) error {
	fs := newFlags("test")
	db := fs.String("d", "", "database (default: odoo.db)")
	tags := fs.String("tags", "", "--test-tags filter, e.g. /sale:TestSaleOrder")
	mods, extra, err := parseModules(fs, args)
	if err != nil {
		return err
	}
	// -i installs the modules that are missing, -u updates the installed
	// ones; odoo runs the tests of both.
	odooArgs := []string{"-i", mods, "-u", mods, "--test-enable"}
	if *tags != "" {
		odooArgs = append(odooArgs, "--test-tags", *tags)
	}
	return runModules(ctx, *db, append(odooArgs, extra...))
}

// parseModules reads "MODULES [flags] [-- extra odoo args]".
func parseModules(fs *flag.FlagSet, args []string) (mods string, extra []string, err error) {
	pos, extra, err := parse(fs, args)
	if err != nil {
		return "", nil, err
	}
	mods, err = splitModules(pos)
	return mods, extra, err
}

// runModules runs: odoo-bin -d DB <args> --stop-after-init --no-http
func runModules(ctx context.Context, db string, args []string) error {
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := prepareOdoo(ctx, p); err != nil {
		return err
	}
	odooArgs := append([]string{"-d", dbOrDefault(p, db)}, args...)
	return runOdoo(ctx, p, append(odooArgs, odoo.OneShotArgs()...)...)
}

func cmdScaffold(ctx context.Context, args []string) error {
	pos, _, err := parse(newFlags("scaffold"), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: godoo scaffold NAME")
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return err
	}
	dir := p.AddonsPaths()[0]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	before, err := odoo.ResolveAddons(p)
	if err != nil {
		return err
	}
	if err := runOdoo(ctx, p, "scaffold", pos[0], dir); err != nil {
		return err
	}
	ui.Info("created %s", filepath.Join(dir, pos[0]))

	// The first module in a directory adds it to addons_path; a running
	// server only sees that after a restart.
	after, err := odoo.ResolveAddons(p)
	if err != nil {
		return err
	}
	if len(after.Path) != len(before.Path) {
		svc, _ := services.Get(p, "odoo")
		if _, running := proc.Running(svc.Spec().PidFile); running {
			ui.Step("restarting odoo to pick up the new addons directory")
			if err := services.Stop(svc); err != nil {
				return err
			}
			return services.Start(ctx, svc)
		}
	}
	return nil
}

func dbOrDefault(p *project.Project, db string) string {
	if db != "" {
		return db
	}
	return p.Cfg.Odoo.DB
}
