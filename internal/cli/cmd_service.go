// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/services"
	"github.com/Mechas-Heart/Godoo/internal/systemd"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// godoo service install|remove|status - run the project under systemd, so
// it starts at boot and restarts on failure without godoo (see package
// systemd).

func cmdService(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: godoo service install [--system] | remove | status")
	}
	switch args[0] {
	case "install":
		return serviceInstall(ctx, args[1:])
	case "remove", "uninstall":
		return serviceRemove(ctx, args[1:])
	case "status":
		return serviceStatus(ctx, args[1:])
	}
	return fmt.Errorf("unknown service command %q (install, remove, status)", args[0])
}

func serviceInstall(ctx context.Context, args []string) error {
	fs := newFlags("service")
	system := fs.Bool("system", false, "system units (/etc/systemd/system, run as you): prints the sudo commands to install them")
	if _, _, err := parse(fs, args); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		return errors.New("run godoo as the project's owner, not as root: system units are installed with the sudo commands godoo prints")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemctl not found: this machine does not run systemd")
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	mode := systemd.User
	if *system {
		mode = systemd.System
	}
	current, managed := services.Managed(p)
	if managed && current != mode {
		return fmt.Errorf("%s units are already installed; run `godoo service remove` first", current)
	}

	// Everything the units use must exist: the nix env, the odoo source,
	// the addon repos, the config files and the postgres cluster.
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return err
	}
	if !managed { // processes started by `godoo up` give their ports to systemd
		if err := services.Down(p); err != nil {
			return err
		}
	}
	ui.Step("preparing config files")
	for _, s := range services.All(p) {
		if s.Enabled() {
			if err := s.Prepare(ctx); err != nil {
				return fmt.Errorf("%s: %w", s.Name(), err)
			}
		}
	}

	owner, err := currentOwner()
	if err != nil {
		return err
	}
	units := systemd.Render(p.Cfg.Name, unitHeader(p), services.SystemdServices(p), mode, owner)
	copies := filepath.Join(p.StateDir(), "systemd")
	if err := systemd.Remove(copies, p.Cfg.Name); err != nil {
		return err
	}
	if err := systemd.Write(copies, units); err != nil {
		return err
	}
	ui.Step("systemd units (%s) in %s", mode, copies)
	for _, u := range units {
		ui.Info("%s", u.Name)
	}

	if mode == systemd.System {
		printSystemInstall(p, copies)
		return nil
	}
	return installUserUnits(ctx, p, units, managed)
}

// installUserUnits copies the units to ~/.config/systemd/user, enables the
// target (start at boot) and starts the services through systemd.
func installUserUnits(ctx context.Context, p *project.Project, units []systemd.Unit, reinstall bool) error {
	dir := systemd.Dir(systemd.User)
	if err := systemd.Remove(dir, p.Cfg.Name); err != nil { // services that were turned off
		return err
	}
	if err := systemd.Write(dir, units); err != nil {
		return err
	}
	target := systemd.Target(p.Cfg.Name)
	for _, args := range [][]string{{"daemon-reload"}, {"enable", target}} {
		if _, err := systemd.Systemctl(ctx, systemd.User, args...); err != nil {
			return err
		}
	}
	ui.Info("installed in %s and enabled %s", dir, target)

	ui.Step("starting services (systemd)")
	if reinstall { // new unit files take effect on restart
		if err := services.Down(p); err != nil {
			return err
		}
	}
	if err := services.Up(ctx, p); err != nil {
		return err
	}
	printURLs(p)

	fmt.Println()
	fmt.Println("systemd now runs this project; godoo up/down/restart/status use systemctl.")
	if linger, _ := exec.Command("loginctl", "show-user", userName(), "--property=Linger", "--value").Output(); strings.TrimSpace(string(linger)) != "yes" {
		fmt.Printf("To start it at boot without logging in first, run once:\n  loginctl enable-linger %s\n", userName())
	}
	return nil
}

// printSystemInstall shows the commands that install system units: godoo
// never runs as root, so the copy and enable are left to sudo.
func printSystemInstall(p *project.Project, copies string) {
	target := systemd.Target(p.Cfg.Name)
	fmt.Println()
	fmt.Println("Install them with sudo (they run as " + userName() + " and start at boot):")
	fmt.Printf("  sudo cp %s/* /etc/systemd/system/\n", copies)
	fmt.Println("  sudo systemctl daemon-reload")
	fmt.Printf("  sudo systemctl enable --now %s\n", target)
	fmt.Println()
	fmt.Println("Then godoo up/down/restart/status use systemctl (which asks for your password).")
}

func serviceRemove(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("service"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	mode, ok := services.Managed(p)
	if !ok {
		return errors.New("no systemd units are installed for this project")
	}
	name, target := p.Cfg.Name, systemd.Target(p.Cfg.Name)
	if mode == systemd.System {
		fmt.Println("Remove the system units with sudo:")
		fmt.Printf("  sudo systemctl disable --now %s\n", target)
		files := []string{"/etc/systemd/system/" + target}
		for _, s := range services.SystemdServices(p) {
			files = append(files, "/etc/systemd/system/"+systemd.ServiceUnit(name, s.Name))
		}
		fmt.Printf("  sudo rm -f %s\n", strings.Join(files, " "))
		fmt.Println("  sudo systemctl daemon-reload")
		fmt.Println("Then `godoo up` runs the services itself again.")
		return nil
	}
	// One by one and waiting, unlike stopping the target (which returns
	// while its services are still stopping): no unit file is deleted
	// under a running service.
	if err := services.Down(p); err != nil {
		return err
	}
	if _, err := systemd.Systemctl(ctx, mode, "disable", "--now", target); err != nil {
		return err
	}
	if err := systemd.Remove(systemd.Dir(mode), name); err != nil {
		return err
	}
	if _, err := systemd.Systemctl(ctx, mode, "daemon-reload"); err != nil {
		return err
	}
	if err := systemd.Remove(filepath.Join(p.StateDir(), "systemd"), name); err != nil {
		return err
	}
	ui.Step("removed the systemd units of %s; services are stopped", name)
	ui.Info("`godoo up` runs them itself again")
	return nil
}

func serviceStatus(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("service"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	mode, ok := services.Managed(p)
	if !ok {
		fmt.Println("not installed: godoo runs the services itself (`godoo service install` to use systemd)")
		return nil
	}
	target := systemd.Target(p.Cfg.Name)
	enabled, _ := systemd.Systemctl(ctx, mode, "is-enabled", target)
	fmt.Printf("%s units in %s - %s is %s\n\n", mode, systemd.Dir(mode), target, enabled)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "UNIT\tSTATE\tPID")
	for _, s := range services.SystemdServices(p) {
		unit := systemd.ServiceUnit(p.Cfg.Name, s.Name)
		state, pid := systemd.State(ctx, mode, unit)
		fmt.Fprintf(w, "%s\t%s\t%d\n", unit, state, pid)
	}
	w.Flush()
	fmt.Printf("\nDetails: %s status %s\n", systemd.Command(mode), target)
	return nil
}

func unitHeader(p *project.Project) string {
	c := p.Cfg
	return fmt.Sprintf(`Generated by `+"`godoo service install`"+` from godoo.yaml: project %s, Odoo %s, env %s.
It runs without godoo: every file it uses is in %s.
After changing ports or services in godoo.yaml, run `+"`godoo service install`"+` again.`, c.Name, c.Odoo.Version, c.Env, p.Root)
}

func currentOwner() (systemd.Owner, error) {
	u, err := user.Current()
	if err != nil {
		return systemd.Owner{}, err
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return systemd.Owner{}, err
	}
	return systemd.Owner{User: u.Username, Group: g.Name}, nil
}

func userName() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}
