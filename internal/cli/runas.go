// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/services"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// Running godoo as root. Root only installs systemd units and starts or
// stops them. Everything that writes project files (the nix env, config
// files, the postgres cluster, odoo's filestore) runs as the account the
// services run as: PostgreSQL refuses to run as root, and files root
// creates would be unwritable for the services afterwards.

func isRoot() bool { return os.Geteuid() == 0 }

// owner is the account that owns the project directory.
func owner(dir string) (*user.User, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("cannot read the owner of %s", dir)
	}
	return user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
}

// readOnly commands write nothing, so root may always run them.
var readOnly = map[string]bool{
	"status": true, "logs": true, "list": true, "config": true, "doctor": true, "env": true,
}

// rootAllowed are the commands root may run in a project that belongs to
// another account. The others would write files as root.
var rootAllowed = map[string]bool{
	"service": true,
	// through systemctl only (checked in rootGuard)
	"up": true, "down": true, "restart": true,
}

// rootGuard stops a command run as root that would write project files as
// root. It returns the account to prepare services as (nil when not root).
func rootGuard(command string) (*user.User, error) {
	if !isRoot() || command == "init" || command == "help" || command == "version" || readOnly[command] {
		return nil, nil
	}
	p, err := openProject()
	if err != nil {
		return nil, nil // not in a project: the command reports that itself
	}
	acct, err := owner(p.Root)
	if err != nil {
		return nil, err
	}
	if acct.Uid == "0" {
		if command == "service" {
			return nil, nil // service install moves the project to an account
		}
		return nil, fmt.Errorf("this project belongs to root, and PostgreSQL refuses to run as root: " +
			"run `godoo service install` as root to give it a service account (--run-as NAME, default odoo)")
	}
	if !rootAllowed[command] {
		return nil, fmt.Errorf("this project belongs to %s: run it as that account, e.g. `sudo -u %s godoo %s ...`", acct.Username, acct.Username, command)
	}
	if command == "up" || command == "down" || command == "restart" {
		if _, managed := services.Managed(p); !managed {
			return nil, fmt.Errorf("as root, godoo only starts and stops services through systemd; run `godoo service install` first, or `sudo -u %s godoo %s`", acct.Username, command)
		}
	}
	return acct, nil
}

// prepareAsOwner makes PrepareHook write config files as acct, by running
// `godoo service prepare --only NAME` as that account.
func prepareAsOwner(p *project.Project, acct *user.User) {
	services.PrepareHook = func(ctx context.Context, s services.Service) error {
		return runAs(ctx, acct, p.Root, "service", "prepare", "--only", s.Name())
	}
}

// runAs runs godoo with args, in the project dir, as acct: its uid, gid and
// groups, its HOME, and a PATH that finds nix for it.
func runAs(ctx context.Context, acct *user.User, dir string, args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	uid, _ := strconv.ParseUint(acct.Uid, 10, 32)
	gid, _ := strconv.ParseUint(acct.Gid, 10, 32)
	var groups []uint32
	if ids, err := acct.GroupIds(); err == nil {
		for _, id := range ids {
			if g, err := strconv.ParseUint(id, 10, 32); err == nil {
				groups = append(groups, uint32(g))
			}
		}
	}
	home := acct.HomeDir
	if _, err := os.Stat(home); err != nil {
		home = dir
	}
	path := filepath.Join(home, ".nix-profile", "bin") + ":/nix/var/nix/profiles/default/bin:" + os.Getenv("PATH")

	ui.Debug("as %s: %s %v", acct.Username, exe, args)
	cmd := exec.CommandContext(ctx, exe, append([]string{"-C", dir}, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + home, "USER=" + acct.Username, "LOGNAME=" + acct.Username, "PATH=" + path, "TERM=" + os.Getenv("TERM")}
	// GODOO_ENV and GODOO_ADMIN_PASSWD decide what goes into odoo.conf:
	// without them a prod server would get a local config.
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GODOO_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	if ui.Verbose {
		cmd.Args = append([]string{exe, "-v"}, cmd.Args[1:]...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: groups}}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("godoo %v as %s: %w (godoo must be installed where %s can run it, e.g. /usr/local/bin)", args, acct.Username, err, acct.Username)
	}
	return nil
}

// ensureAccount returns the account name, creating it as a system account
// (no login, home = the project) when it does not exist.
func ensureAccount(name, home string) (*user.User, error) {
	if acct, err := user.Lookup(name); err == nil {
		return acct, nil
	}
	ui.Info("creating system account %s (no login, home %s)", name, home)
	out, err := exec.Command("useradd", "--system", "--user-group", "--home-dir", home,
		"--no-create-home", "--shell", "/usr/sbin/nologin", name).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("useradd %s: %w\n%s", name, err, out)
	}
	return user.Lookup(name)
}
