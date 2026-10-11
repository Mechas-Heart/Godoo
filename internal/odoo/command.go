// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"path/filepath"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/project"
)

// Command builds the argv for odoo-bin using the nix python and odoo.conf.
//
//	Command(p)                        -> python odoo-bin -c odoo.conf
//	Command(p, "-u", "sale")          -> python odoo-bin -c odoo.conf -u sale
//	Command(p, "shell", "-d", "db")   -> python odoo-bin shell -c odoo.conf -d db
//
// A subcommand must come first for odoo-bin, so -c is inserted after it.
// scaffold does not read a config file and gets no -c.
func Command(p *project.Project, args ...string) []string {
	argv := []string{p.EnvBin("python3"), filepath.Join(p.SourceDir(), "odoo-bin")}
	conf := []string{"-c", p.ConfFile("odoo.conf")}

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub := args[0]
		argv = append(argv, sub)
		if sub != "scaffold" && sub != "help" {
			argv = append(argv, conf...)
		}
		return append(argv, args[1:]...)
	}
	return append(append(argv, conf...), args...)
}

// ServerArgs are the extra flags for the long-running server.
func ServerArgs(p *project.Project) []string {
	if dev := *p.Cfg.Odoo.Dev; dev != "" {
		return []string{"--dev=" + dev}
	}
	return nil
}

// OneShotArgs are appended to maintenance runs (-i/-u/tests) so they do not
// fight the running server for the http port, and exit when done.
func OneShotArgs() []string { return []string{"--stop-after-init", "--no-http"} }
