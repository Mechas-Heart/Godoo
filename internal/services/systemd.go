// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/systemd"
)

// When `godoo service install` has installed a project's systemd units,
// systemd owns its processes: Start, Stop, IsRunning and Status below go
// through systemctl instead of pid files, so godoo and systemd never start
// the same service twice.

// Managed reports whether p's services run under systemd, and in which mode.
func Managed(p *project.Project) (systemd.Mode, bool) {
	return systemd.Installed(p.Cfg.Name)
}

// SystemdServices describes the enabled services for systemd.Render: the
// same command lines and log files `godoo up` uses.
func SystemdServices(p *project.Project) []systemd.Service {
	c := p.Cfg
	describe := map[string]string{
		"postgres": "PostgreSQL " + c.Postgres.Version,
		"mailpit":  "Mailpit",
		"odoo":     "Odoo " + c.Odoo.Version,
		"nginx":    "nginx",
	}
	var out []systemd.Service
	var enabled []string
	for _, s := range All(p) {
		if !s.Enabled() {
			continue
		}
		spec := s.Spec()
		svc := systemd.Service{
			Name:        s.Name(),
			Description: describe[s.Name()],
			Args:        spec.Args,
			Dir:         spec.Dir,
			// the nix env first, then only the system's basic directories:
			// nothing from the shell godoo happened to run in
			Path:       filepath.Join(p.EnvDir(), "bin") + ":/usr/local/bin:/usr/bin:/bin",
			LogFile:    spec.LogFile,
			StopSignal: spec.StopSignal,
		}
		switch s.Name() {
		case "odoo":
			svc.After = enabled // postgres and mailpit
			svc.Requires = []string{"postgres"}
		case "nginx":
			svc.After = []string{"odoo"}
		}
		enabled = append(enabled, s.Name())
		out = append(out, svc)
	}
	return out
}

// unit is the systemd unit name of s.
func unit(s Service) string { return systemd.ServiceUnit(s.proj().Cfg.Name, s.Name()) }

// startUnit starts s with systemctl, after Prepare (config files, initdb)
// as `godoo up` does, and waits for its port.
func startUnit(ctx context.Context, mode systemd.Mode, s Service) error {
	if err := s.Prepare(ctx); err != nil {
		return fmt.Errorf("%s: %w", s.Name(), err)
	}
	if _, err := systemd.Systemctl(ctx, mode, "start", unit(s)); err != nil {
		return fmt.Errorf("%w\n(try: %s start %s)", err, systemd.Command(mode), unit(s))
	}
	return nil
}
