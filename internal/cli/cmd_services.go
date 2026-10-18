// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/Mechas-Heart/Godoo/internal/odoo"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/registry"
	"github.com/Mechas-Heart/Godoo/internal/services"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

func cmdUp(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("up"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return err
	}
	ui.Step("starting services")
	if err := services.Up(ctx, p); err != nil {
		return err
	}
	printURLs(p)
	return nil
}

func cmdDown(ctx context.Context, args []string) error {
	fs := newFlags("down")
	all := fs.Bool("all", false, "stop the services of every project on this machine (godoo list)")
	if _, _, err := parse(fs, args); err != nil {
		return err
	}
	if *all {
		return downAll()
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	ui.Step("stopping services")
	return services.Down(p)
}

// downAll stops every registered project that has something running.
// Used by `install.sh --uninstall`, so nothing keeps running without godoo.
func downAll() error {
	entries, err := registry.All()
	if err != nil {
		return err
	}
	var failed []string
	for _, e := range entries {
		p, err := project.Open(e.Path)
		if err != nil || !services.AnyRunning(p) {
			continue // missing project, or nothing to stop
		}
		ui.Step("stopping %s (%s)", e.Name, e.Path)
		if err := services.Down(p); err != nil {
			ui.Warn("%s: %v", e.Name, err)
			failed = append(failed, e.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not stop: %s", strings.Join(failed, ", "))
	}
	return nil
}

func cmdRestart(ctx context.Context, args []string) error {
	pos, _, err := parse(newFlags("restart"), args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	name := "odoo"
	if len(pos) > 0 {
		name = pos[0]
	}
	s, err := services.Get(p, name)
	if err != nil {
		return err
	}
	if err := services.Stop(s); err != nil {
		return err
	}
	return services.Start(ctx, s)
}

func cmdStatus(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("status"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	c := p.Cfg
	fmt.Printf("%s - Odoo %s %s - env %s (from %s) - %s\n", c.Name, c.Odoo.Version, c.Odoo.Edition, c.Env, c.EnvSource, p.Root)
	if mode, ok := services.Managed(p); ok {
		fmt.Printf("run by systemd (%s units, `godoo service status`)\n", mode)
	}
	fmt.Println()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SERVICE\tSTATE\tPID\tPORT\tLOG")
	for _, st := range services.Status(p) {
		state, pid := "stopped", "-"
		switch {
		case !st.Enabled:
			state = "disabled"
		case st.Running:
			state, pid = "running", fmt.Sprint(st.Pid)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", st.Name, state, pid, st.Port, st.Log)
	}
	w.Flush()
	if services.AnyRunning(p) {
		fmt.Println()
		printURLs(p)
	}
	return nil
}

func cmdLogs(ctx context.Context, args []string) error {
	fs := newFlags("logs")
	follow := fs.Bool("f", false, "follow")
	lines := fs.Int("n", 100, "number of lines")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	name := "odoo"
	if len(pos) > 0 {
		name = pos[0]
	}
	var files []string
	if name == "all" {
		for _, s := range services.All(p) {
			if s.Enabled() {
				files = append(files, p.LogFile(s.Name()))
			}
		}
	} else {
		if _, err := services.Get(p, name); err != nil {
			return err
		}
		files = []string{p.LogFile(name)}
	}
	// tail is simple and does exactly what you expect (incl. -F across rotations).
	tailArgs := []string{"-n", fmt.Sprint(*lines)}
	if *follow {
		tailArgs = append(tailArgs, "-F")
	}
	cmd := exec.CommandContext(ctx, "tail", append(tailArgs, files...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// cmdRun replaces godoo with odoo-bin in the foreground, so breakpoints,
// pdb and Ctrl-C work as if you had typed the odoo-bin command yourself.
func cmdRun(ctx context.Context, args []string) error {
	fs := newFlags("run")
	debugpy := fs.Bool("debugpy", false, "run under debugpy so VS Code (or any DAP client) can attach")
	debugPort := fs.Int("debugpy-port", 5678, "port debugpy listens on (127.0.0.1)")
	wait := fs.Bool("wait", false, "with --debugpy: wait for the debugger before starting odoo")
	_, extra, err := parse(fs, args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if *debugpy && !p.Cfg.DevToolsOn() {
		return errors.New("--debugpy needs the local dev tools (env: local, nix.dev_tools: true)")
	}
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return err
	}
	ui.Step("starting services (except odoo)")
	if err := services.Up(ctx, p, "odoo"); err != nil {
		return err
	}
	odooSvc, _ := services.Get(p, "odoo")
	if err := services.Stop(odooSvc); err != nil { // the background one, if any
		return err
	}
	if err := odooSvc.Prepare(ctx); err != nil {
		return err
	}
	printURLs(p)
	argv := odoo.Command(p, append(odoo.ServerArgs(p), extra...)...)
	if *debugpy {
		// python -m debugpy --listen 127.0.0.1:5678 [--wait-for-client] odoo-bin ...
		dbg := []string{"-m", "debugpy", "--listen", fmt.Sprintf("127.0.0.1:%d", *debugPort)}
		if *wait {
			dbg = append(dbg, "--wait-for-client")
		}
		argv = append(append([]string{argv[0]}, dbg...), argv[1:]...)
		ui.Info("debugpy on 127.0.0.1:%d - attach your editor's debugger there", *debugPort)
	}
	ui.Step("exec %s", strings.Join(argv, " "))
	return syscall.Exec(argv[0], argv, p.Environ())
}

func printURLs(p *project.Project) {
	c := p.Cfg
	host := c.Nginx.Listen
	if host == "0.0.0.0" || host == "127.0.0.1" {
		host = "localhost"
	}
	fmt.Printf("\n  Odoo     http://%s:%d\n", host, c.Ports.HTTP)
	if c.MailpitOn() {
		fmt.Printf("  Mailpit  http://%s:%d\n", host, c.Ports.Mailpit)
	}
	fmt.Printf("  Postgres 127.0.0.1:%d (user %s)\n\n", c.Ports.Postgres, c.Postgres.User)
}
