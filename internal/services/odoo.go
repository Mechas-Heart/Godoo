// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Mechas-Heart/Godoo/internal/odoo"
	"github.com/Mechas-Heart/Godoo/internal/proc"
	"github.com/Mechas-Heart/Godoo/internal/project"
)

// Odoo is the odoo-bin server, listening on 127.0.0.1:ports.odoo behind nginx.
type Odoo struct{ p *project.Project }

func (s *Odoo) proj() *project.Project      { return s.p }
func (s *Odoo) Name() string                { return "odoo" }
func (s *Odoo) Enabled() bool               { return true }
func (s *Odoo) Port() int                   { return s.p.Cfg.Ports.Odoo }
func (s *Odoo) ReadyTimeout() time.Duration { return 60 * time.Second }

func (s *Odoo) Spec() proc.Spec {
	argv := odoo.Command(s.p, odoo.ServerArgs(s.p)...)
	return baseSpec(s.p, s.Name(), syscall.SIGTERM, argv...)
}

func (s *Odoo) Prepare(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Join(s.p.DataDir(), "odoo"), 0o755); err != nil {
		return err
	}
	return odoo.WriteConf(s.p)
}
