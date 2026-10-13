// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/services"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

func cmdDB(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: godoo db list|create|drop [NAME]")
	}
	switch args[0] {
	case "list", "ls":
		return dbList(ctx, args[1:])
	case "create":
		return dbCreate(ctx, args[1:])
	case "drop", "rm":
		return dbDrop(ctx, args[1:])
	}
	return fmt.Errorf("unknown db command %q (list, create, drop)", args[0])
}

// startPostgres makes sure the env exists and postgres is running.
func startPostgres(ctx context.Context, p *project.Project) error {
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return err
	}
	if err := p.EnsureDirs(); err != nil {
		return err
	}
	pg, _ := services.Get(p, "postgres")
	return services.Start(ctx, pg)
}

func dbList(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("db"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := startPostgres(ctx, p); err != nil {
		return err
	}
	return runTool(ctx, p, "psql", "-d", "postgres", "-At", "-c",
		"SELECT datname FROM pg_database WHERE NOT datistemplate AND datname <> 'postgres' ORDER BY 1")
}

func dbCreate(ctx context.Context, args []string) error {
	fs := newFlags("db")
	demo := fs.Bool("demo", false, "load demo data")
	lang := fs.String("lang", "", "language to load, e.g. fr_FR")
	modules := fs.String("i", "", "extra modules to install, comma separated")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	name := p.Cfg.Odoo.DB
	if len(pos) > 0 {
		name = pos[0]
	}
	if err := prepareOdoo(ctx, p); err != nil {
		return err
	}
	install := "base"
	if p.Cfg.EnterpriseOn() {
		install += ",web_enterprise" // makes the new database an Enterprise one
	}
	if *modules != "" {
		install += "," + *modules
	}
	odooArgs := []string{"-d", name, "-i", install}
	odooArgs = append(odooArgs, p.Cfg.Profile.DemoFlags(*demo)...)
	if *lang != "" {
		odooArgs = append(odooArgs, "--load-language", *lang)
	}
	ui.Step("creating database %s", name)
	return runOdoo(ctx, p, append(odooArgs, "--stop-after-init", "--no-http")...)
}

func dbDrop(ctx context.Context, args []string) error {
	fs := newFlags("db")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: godoo db drop NAME [--yes]")
	}
	name := pos[0]
	p, err := openProject()
	if err != nil {
		return err
	}
	if !*yes && !confirm(fmt.Sprintf("Drop database %q and its filestore?", name)) {
		return errors.New("aborted")
	}
	if err := startPostgres(ctx, p); err != nil {
		return err
	}
	// --force disconnects odoo's open connections first (PostgreSQL 13+)
	if err := runTool(ctx, p, "dropdb", "--force", "--if-exists", name); err != nil {
		return err
	}
	filestore := filepath.Join(p.DataDir(), "odoo", "filestore", name)
	ui.Info("removing %s", filestore)
	return os.RemoveAll(filestore)
}

func cmdPsql(ctx context.Context, args []string) error {
	pos, extra, err := parse(newFlags("psql"), args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := startPostgres(ctx, p); err != nil {
		return err
	}
	db := p.Cfg.Odoo.DB
	if len(pos) > 0 {
		db = pos[0]
	}
	psql := p.EnvBin("psql")
	return syscall.Exec(psql, append([]string{psql, "-d", db}, extra...), p.Environ())
}

// queryDB runs one SQL query with psql and returns the rows (first column).
func queryDB(ctx context.Context, p *project.Project, db, sql string) ([]string, error) {
	cmd := exec.CommandContext(ctx, p.EnvBin("psql"), "-d", db, "-At", "-c", sql)
	cmd.Env = p.Environ()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var rows []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			rows = append(rows, line)
		}
	}
	return rows, nil
}

// runTool runs a binary from the nix env (psql, dropdb, ...) with PG* set.
func runTool(ctx context.Context, p *project.Project, name string, args ...string) error {
	ui.Debug("%s %s", name, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, p.EnvBin(name), args...)
	cmd.Env = p.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func confirm(question string) bool {
	fmt.Printf("%s [y/N] ", question)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
