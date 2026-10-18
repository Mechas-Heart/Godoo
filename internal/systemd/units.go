// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package systemd turns a project's services into systemd units, so a
// server runs the project at boot without godoo:
//
//	godoo-shop.target              starts everything (enabled at boot)
//	godoo-shop-postgres.service    the same command lines `godoo up` runs,
//	godoo-shop-mailpit.service     with the same config files and logs in
//	godoo-shop-odoo.service        .godoo/ - nothing in a unit calls godoo
//	godoo-shop-nginx.service
//
// User units (~/.config/systemd/user) need no root. System units
// (/etc/systemd/system, with User=) are written into .godoo/systemd and
// installed with sudo commands godoo prints, so godoo never runs as root.
package systemd

import (
	"fmt"
	"strings"
	"syscall"
)

// Mode is where units are installed.
type Mode string

const (
	User   Mode = "user"   // systemctl --user, ~/.config/systemd/user
	System Mode = "system" // systemctl, /etc/systemd/system
)

// Service is what a unit needs to know about one service.
type Service struct {
	Name        string   // postgres, mailpit, odoo, nginx
	Description string   // e.g. "PostgreSQL 16"
	Args        []string // command line, Args[0] is the executable
	Dir         string   // working directory
	Path        string   // PATH for the process (the nix env first)
	LogFile     string
	StopSignal  syscall.Signal
	After       []string // services (by Name) to start first
	Requires    []string // services that must run; stopping them stops this one
}

// Unit is one file to install.
type Unit struct {
	Name    string // e.g. godoo-shop-odoo.service
	Content string
}

// Target is the name of the unit that groups a project's services.
func Target(project string) string { return "godoo-" + project + ".target" }

// ServiceUnit is the unit name of one service of a project.
func ServiceUnit(project, service string) string {
	return "godoo-" + project + "-" + service + ".service"
}

// Owner is the account system units run as (User=, Group=).
type Owner struct{ User, Group string }

// Render returns the units for the services and the target that starts
// them. owner is only used for System mode. header describes where the
// files come from (project, Odoo version, env).
func Render(project, header string, svcs []Service, mode Mode, owner Owner) []Unit {
	var units []Unit
	var names []string
	for _, s := range svcs {
		name := ServiceUnit(project, s.Name)
		names = append(names, name)
		units = append(units, Unit{name, renderService(project, header, s, mode, owner)})
	}

	wantedBy := "default.target" // user: started with the user's systemd
	if mode == System {
		wantedBy = "multi-user.target"
	}
	var b strings.Builder
	b.WriteString(comment(header))
	fmt.Fprintf(&b, "[Unit]\nDescription=godoo project %s\n", project)
	fmt.Fprintf(&b, "Wants=%s\n", strings.Join(names, " "))
	fmt.Fprintf(&b, "\n[Install]\nWantedBy=%s\n", wantedBy)
	units = append(units, Unit{Target(project), b.String()})
	return units
}

func renderService(project, header string, s Service, mode Mode, owner Owner) string {
	var b strings.Builder
	b.WriteString(comment(header))
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s (godoo project %s)\n", s.Description, project)
	fmt.Fprintf(&b, "PartOf=%s\n", Target(project)) // stopping the target stops this
	if len(s.After) > 0 {
		fmt.Fprintf(&b, "After=%s\n", unitList(project, s.After))
	}
	if len(s.Requires) > 0 {
		fmt.Fprintf(&b, "Requires=%s\n", unitList(project, s.Requires))
	}

	b.WriteString("\n[Service]\nType=simple\n")
	if mode == System {
		fmt.Fprintf(&b, "User=%s\nGroup=%s\n", owner.User, owner.Group)
	}
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", quote(s.Dir))
	fmt.Fprintf(&b, "Environment=%s\n", quote("PATH="+s.Path))
	// no packages from ~/.local in the nix python (as `godoo up`)
	b.WriteString("Environment=PYTHONNOUSERSITE=1\n")
	quoted := make([]string, len(s.Args))
	for i, a := range s.Args {
		quoted[i] = quote(a)
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", strings.Join(quoted, " "))
	fmt.Fprintf(&b, "KillSignal=%s\n", signalName(s.StopSignal))
	b.WriteString("TimeoutStopSec=30\nRestart=on-failure\nRestartSec=5\n")
	// the same log file `godoo logs` reads
	fmt.Fprintf(&b, "StandardOutput=append:%s\nStandardError=inherit\n", s.LogFile)
	return b.String()
}

func comment(header string) string {
	var b strings.Builder
	for _, line := range strings.Split(header, "\n") {
		b.WriteString("# " + line + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

func unitList(project string, services []string) string {
	units := make([]string, len(services))
	for i, s := range services {
		units[i] = ServiceUnit(project, s)
	}
	return strings.Join(units, " ")
}

// quote makes one command-line word safe for systemd: "%" is a specifier
// there, and words with spaces, quotes or nothing at all need quotes.
func quote(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if s != "" && !strings.ContainsAny(s, " \t\"'\\;") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	default:
		return "SIGTERM"
	}
}
