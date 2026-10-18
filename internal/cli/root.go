// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package cli implements the godoo commands. Each cmd_*.go file holds one
// group of commands; this file holds dispatch and shared helpers.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

type command struct {
	name    string
	usage   string // arguments, shown in help
	summary string
	run     func(ctx context.Context, args []string) error
}

// commands in the order they appear in `godoo help`.
func commands() []command {
	return []command{
		{"init", "[dir] [--version 17.0] [--name N] [--env local|prod] [--source PATH]", "create a new project", cmdInit},
		{"setup", "", "build the nix env (python, postgres, nginx, ...) and clone odoo", cmdSetup},
		{"up", "", "start postgres, mailpit, odoo and nginx in the background", cmdUp},
		{"down", "[--all]", "stop all services (--all: of every project)", cmdDown},
		{"restart", "[service]", "restart one service (default: odoo)", cmdRestart},
		{"status", "", "show services, pids and ports", cmdStatus},
		{"service", "install [--system] | remove | status", "run the project under systemd: starts at boot, without godoo", cmdService},
		{"logs", "[service|all] [-f] [-n 100]", "show service logs (default: odoo)", cmdLogs},
		{"run", "[--debugpy [--wait]] [-- odoo args]", "run odoo in the foreground (pdb, breakpoint(), debugpy)", cmdRun},
		{"odoo", "-- ARGS", "run odoo-bin with the project config", cmdOdoo},
		{"shell", "[-d DB]", "open an odoo shell", cmdShell},
		{"install", "MODULES [-d DB]", "install modules (comma or space separated)", cmdInstall},
		{"update", "MODULES [-d DB]", "update modules", cmdUpdate},
		{"test", "MODULES [-d DB] [--tags T]", "install/update modules and run their tests", cmdTest},
		{"scaffold", "NAME", "create a new module in the first addons directory", cmdScaffold},
		{"edition", "[community|enterprise] [--repo URL] [--branch B]", "show or switch the Odoo edition (enterprise addons repo)", cmdEdition},
		{"pkg", "add|remove|list [NAME...]", "python packages for your addons (nixpkgs, else a pinned PyPI wheel)", cmdPkg},
		{"addons", "add|remove|update|list|deps [REPO...]", "addon git repos, e.g. `godoo addons add web` (OCA)", cmdAddons},
		{"db", "list|create|drop [NAME]", "manage databases", cmdDB},
		{"psql", "[DB] [-- psql args]", "open psql on the project's postgres", cmdPsql},
		{"env", "", "print shell exports for this project (eval \"$(godoo env)\")", cmdEnv},
		{"sh", "", "start a shell with the project environment", cmdSh},
		{"list", "[--prune]", "list all godoo projects on this machine", cmdList},
		{"config", "", "print the resolved configuration", cmdConfig},
		{"nix-update", "", "update flake.lock to the latest pinned nixpkgs and rebuild", cmdNixUpdate},
		{"doctor", "[--versions]", "check nix, odoo's python requirements and ports", cmdDoctor},
		{"destroy", "[--yes]", "stop services and delete .godoo (data, logs)", cmdDestroy},
		{"version", "", "print the godoo version", cmdVersion},
	}
}

// workDir is set by the global -C flag.
var workDir = "."

func Main(args []string) int {
	args, err := parseGlobal(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printHelp(os.Stdout)
		return 0
	}
	for _, c := range commands() {
		if c.name != args[0] {
			continue
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		// As root, only commands that go through systemd run (see runas.go).
		acct, err := rootGuard(c.name)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if acct != nil {
			if p, err := openProject(); err == nil {
				prepareAsOwner(p, acct)
			}
		}
		if err := c.run(ctx, args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown command %q; see `godoo help`\n", args[0])
	return 2
}

// parseGlobal handles flags placed before the command: -v and -C DIR.
func parseGlobal(args []string) ([]string, error) {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-h" && args[0] != "--help" {
		switch args[0] {
		case "-v", "--verbose":
			ui.Verbose = true
			args = args[1:]
		case "-C":
			if len(args) < 2 {
				return nil, errors.New("-C needs a directory")
			}
			workDir, args = args[1], args[2:]
		default:
			return nil, fmt.Errorf("unknown global flag %s; see `godoo help`", args[0])
		}
	}
	return args, nil
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `godoo - Odoo development environments on Nix

Usage: godoo [-v] [-C DIR] <command> [args]

  -v       verbose: print every command godoo runs
  -C DIR   run as if started in DIR

Commands:
`)
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-11s %s\n", c.name, c.summary)
	}
	fmt.Fprint(w, "\nRun `godoo <command> -h` for the options of a command.\n")
}

func openProject() (*project.Project, error) { return project.Open(workDir) }

// newFlags creates a FlagSet whose -h output shows the command's usage line.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		for _, c := range commands() {
			if c.name == name {
				fmt.Fprintf(fs.Output(), "Usage: godoo %s %s\n\n%s\n", c.name, c.usage, c.summary)
			}
		}
		fs.PrintDefaults()
	}
	return fs
}

// parse lets flags and positional arguments be mixed ("init shop -version
// 17"), which the standard flag package does not. Everything after "--" is
// returned untouched as passthrough.
func parse(fs *flag.FlagSet, args []string) (positional, passthrough []string, err error) {
	for i, a := range args {
		if a == "--" {
			args, passthrough = args[:i], args[i+1:]
			break
		}
	}
	for {
		if err := fs.Parse(args); err != nil {
			return nil, nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, passthrough, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// splitModules turns ["sale,stock", "crm"] into "sale,stock,crm".
func splitModules(args []string) (string, error) {
	var mods []string
	for _, a := range args {
		for _, m := range strings.Split(a, ",") {
			if m = strings.TrimSpace(m); m != "" {
				mods = append(mods, m)
			}
		}
	}
	if len(mods) == 0 {
		return "", errors.New("no modules given")
	}
	return strings.Join(mods, ","), nil
}
