// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package systemd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Dir is where units of a mode are installed.
func Dir(mode Mode) string {
	if mode == System {
		return "/etc/systemd/system"
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user")
}

// Installed reports whether project's units are installed, and where. User
// units are checked first.
func Installed(project string) (Mode, bool) {
	for _, mode := range []Mode{User, System} {
		if _, err := os.Stat(filepath.Join(Dir(mode), Target(project))); err == nil {
			return mode, true
		}
	}
	return "", false
}

// Write saves units into dir.
func Write(dir string, units []Unit) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, u := range units {
		if err := os.WriteFile(filepath.Join(dir, u.Name), []byte(u.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Read loads project's unit files from dir (the ones that exist).
func Read(dir, project string) ([]Unit, error) {
	var units []Unit
	for _, name := range append(serviceUnits(project), Target(project)) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		units = append(units, Unit{name, string(b)})
	}
	if len(units) == 0 {
		return nil, fmt.Errorf("no unit files for %s in %s", project, dir)
	}
	return units, nil
}

func serviceUnits(project string) []string {
	var out []string
	for _, s := range serviceNames {
		out = append(out, ServiceUnit(project, s))
	}
	return out
}

// serviceNames are every service a project can have (services.All). Remove
// deletes their units by exact name: a glob like godoo-shop-*.service would
// also match a project called "shop-x".
var serviceNames = []string{"postgres", "mailpit", "odoo", "nginx"}

// Remove deletes project's unit files from dir.
func Remove(dir, project string) error {
	files := []string{filepath.Join(dir, Target(project))}
	for _, s := range serviceNames {
		files = append(files, filepath.Join(dir, ServiceUnit(project, s)))
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Systemctl runs systemctl (with --user for user units) and returns its
// trimmed output; on failure the output is part of the error.
func Systemctl(ctx context.Context, mode Mode, args ...string) (string, error) {
	if mode == User {
		args = append([]string{"--user"}, args...)
	}
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		return text, fmt.Errorf("systemctl %s: %w\n%s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

// State is a unit's ActiveState ("active", "inactive", "failed", ...) and
// main pid (0 when not running).
func State(ctx context.Context, mode Mode, unit string) (string, int) {
	out, err := Systemctl(ctx, mode, "show", unit, "--property=ActiveState,MainPID")
	if err != nil {
		return "unknown", 0
	}
	state, pid := "unknown", 0
	for _, line := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "ActiveState":
			state = v
		case "MainPID":
			pid, _ = strconv.Atoi(v)
		}
	}
	return state, pid
}

// Command is how a person runs systemctl for mode, for printed hints.
func Command(mode Mode) string {
	if mode == User {
		return "systemctl --user"
	}
	return "sudo systemctl"
}
