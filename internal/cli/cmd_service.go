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

	"github.com/Mechas-Heart/Godoo/internal/config"
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
		return errors.New("usage: godoo service install [--user | --system] [--run-as NAME] | remove | status")
	}
	switch args[0] {
	case "install":
		return serviceInstall(ctx, args[1:])
	case "remove", "uninstall":
		return serviceRemove(ctx, args[1:])
	case "status":
		return serviceStatus(ctx, args[1:])
	case "prepare": // internal: run as the service account by `service install` as root
		return servicePrepare(ctx, args[1:])
	}
	return fmt.Errorf("unknown service command %q (install, remove, status)", args[0])
}

// Where units go:
//
//	as root                 system units, installed and started right away
//	--system, or env prod   system units, installed with the sudo commands godoo prints
//	--user, or env local    user units in your account, no root
func serviceInstall(ctx context.Context, args []string) error {
	fs := newFlags("service")
	userUnits := fs.Bool("user", false, "user units in your account (~/.config/systemd/user), no root; the default in env local")
	system := fs.Bool("system", false, "system units (/etc/systemd/system), started at boot; the default in env prod and as root")
	runAsFlag := fs.String("run-as", "", "as root: the account the services run as (default: the project's owner, or odoo when root owns it)")
	yes := fs.Bool("yes", false, "as root: do not ask before giving the project to the --run-as account")
	if _, _, err := parse(fs, args); err != nil {
		return err
	}
	if *userUnits && *system {
		return errors.New("use --user or --system, not both")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemctl not found: this machine does not run systemd")
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if err := refusePinMismatch(p); err != nil {
		return err
	}

	mode := systemd.User
	switch {
	case isRoot() && *userUnits:
		return errors.New("user units belong to an account, not root: run `godoo service install --user` as that account")
	case isRoot(), *system, !*userUnits && p.Cfg.Env == config.EnvProd:
		mode = systemd.System
	}
	current, managed := services.Managed(p)
	if managed && current != mode {
		return fmt.Errorf("%s units are already installed; run `godoo service remove` first", current)
	}

	if isRoot() {
		return installAsRoot(ctx, p, *runAsFlag, *yes, managed)
	}
	if *runAsFlag != "" {
		return errors.New("--run-as needs root: system units installed with sudo run as you")
	}
	units, err := prepareUnits(ctx, p, mode, managed)
	if err != nil {
		return err
	}
	if mode == systemd.System {
		printSystemInstall(p, filepath.Join(p.StateDir(), "systemd"))
		return nil
	}
	return installUserUnits(ctx, p, units, managed)
}

// prepareUnits does everything before installing, as the account the
// services run as: the nix env, odoo source and addon repos (setup), the
// config files and postgres cluster (Prepare), and the unit files, written
// to .godoo/systemd.
func prepareUnits(ctx context.Context, p *project.Project, mode systemd.Mode, managed bool) ([]systemd.Unit, error) {
	if err := setup(ctx, p, setupOptions{}); err != nil {
		return nil, err
	}
	if !managed { // processes started by `godoo up` give their ports to systemd
		if err := services.Down(p); err != nil {
			return nil, err
		}
	}
	ui.Step("preparing config files")
	for _, s := range services.All(p) {
		if s.Enabled() {
			if err := s.Prepare(ctx); err != nil {
				return nil, fmt.Errorf("%s: %w", s.Name(), err)
			}
		}
	}

	owner, err := currentOwner()
	if err != nil {
		return nil, err
	}
	units := systemd.Render(p.Cfg.Name, unitHeader(p), services.SystemdServices(p), mode, owner)
	copies := filepath.Join(p.StateDir(), "systemd")
	if err := systemd.Remove(copies, p.Cfg.Name); err != nil {
		return nil, err
	}
	if err := systemd.Write(copies, units); err != nil {
		return nil, err
	}
	ui.Step("systemd units (%s) in %s", mode, copies)
	for _, u := range units {
		ui.Info("%s", u.Name)
	}
	return units, nil
}

// servicePrepare is `godoo service prepare`, run as the service account by
// root: all of prepareUnits, or with --only NAME the config files of one
// service before systemd starts it.
func servicePrepare(ctx context.Context, args []string) error {
	fs := newFlags("service")
	only := fs.String("only", "", "prepare one service")
	if _, _, err := parse(fs, args); err != nil {
		return err
	}
	if isRoot() {
		return errors.New("`godoo service prepare` runs as the service account, not root")
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if *only != "" {
		s, err := services.Get(p, *only)
		if err != nil {
			return err
		}
		return s.Prepare(ctx)
	}
	_, managed := services.Managed(p)
	_, err = prepareUnits(ctx, p, systemd.System, managed)
	return err
}

// installAsRoot installs system units directly. The services run as an
// account, never as root (PostgreSQL refuses root): the project's owner, or
// the --run-as account, created if needed and given the project.
func installAsRoot(ctx context.Context, p *project.Project, account string, yes, managed bool) error {
	own, err := owner(p.Root)
	if err != nil {
		return err
	}
	if account == "" {
		account = "odoo"
		if own.Uid != "0" {
			account = own.Username
		}
	}
	if account == "root" {
		return errors.New("the services cannot run as root: PostgreSQL refuses it; pick another --run-as account")
	}
	acct, err := ensureAccount(account, p.Root)
	if err != nil {
		return err
	}
	if own.Uid != acct.Uid {
		if !yes && !confirm(fmt.Sprintf("Give %s to %s (chown -R), so the services can run as %s?", p.Root, acct.Username, acct.Username)) {
			return errors.New("aborted")
		}
		group, err := user.LookupGroupId(acct.Gid)
		if err != nil {
			return err
		}
		ui.Info("chown -R %s:%s %s", acct.Username, group.Name, p.Root)
		if out, err := exec.Command("chown", "-R", acct.Username+":"+group.Name, p.Root).CombinedOutput(); err != nil {
			return fmt.Errorf("chown: %w\n%s", err, out)
		}
	}

	// Everything that writes project files, as the account.
	ui.Step("preparing as %s", acct.Username)
	if err := runAs(ctx, acct, p.Root, "service", "prepare"); err != nil {
		return err
	}

	// The root part: install, enable and start the units.
	copies := filepath.Join(p.StateDir(), "systemd")
	units, err := systemd.Read(copies, p.Cfg.Name)
	if err != nil {
		return err
	}
	dir, target := systemd.Dir(systemd.System), systemd.Target(p.Cfg.Name)
	if err := systemd.Remove(dir, p.Cfg.Name); err != nil {
		return err
	}
	if err := systemd.Write(dir, units); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", target}} {
		if _, err := systemd.Systemctl(ctx, systemd.System, args...); err != nil {
			return err
		}
	}
	ui.Info("installed in %s and enabled %s (starts at boot)", dir, target)

	ui.Step("starting services (systemd, as %s)", acct.Username)
	services.PrepareHook = func(context.Context, services.Service) error { return nil } // done above
	if managed {
		if err := services.Down(p); err != nil {
			return err
		}
	}
	if err := services.Up(ctx, p); err != nil {
		return err
	}
	printURLs(p)
	fmt.Println()
	fmt.Printf("systemd now runs this project as %s; it starts at boot and restarts on failure.\n", acct.Username)
	fmt.Println("  sudo godoo up | down | restart | status     through systemctl")
	fmt.Printf("  sudo -u %s godoo install MODULE ...       everything else, as %s\n", acct.Username, acct.Username)
	return nil
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
	fmt.Println("(or run `sudo godoo service install`, which does all of this)")
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
	if mode == systemd.System && !isRoot() {
		fmt.Println("Remove the system units with sudo:")
		fmt.Printf("  sudo systemctl disable --now %s\n", target)
		files := []string{"/etc/systemd/system/" + target}
		for _, s := range services.SystemdServices(p) {
			files = append(files, "/etc/systemd/system/"+systemd.ServiceUnit(name, s.Name))
		}
		fmt.Printf("  sudo rm -f %s\n", strings.Join(files, " "))
		fmt.Println("  sudo systemctl daemon-reload")
		fmt.Println("(or run `sudo godoo service remove`)")
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
	if p.Cfg.Env == config.EnvProd {
		ui.Info("env prod runs only under systemd: `godoo service install` to install them again")
	} else {
		ui.Info("`godoo up` runs them itself again")
	}
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
