// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"fmt"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Mechas-Heart/Godoo/internal/proc"
	"github.com/Mechas-Heart/Godoo/internal/project"
)

// Mailpit catches every email odoo sends (local env only by default).
// SMTP on 127.0.0.1:ports.smtp, web UI on nginx.listen:ports.mailpit.
type Mailpit struct{ p *project.Project }

func (s *Mailpit) proj() *project.Project            { return s.p }
func (s *Mailpit) Name() string                      { return "mailpit" }
func (s *Mailpit) Enabled() bool                     { return s.p.Cfg.MailpitOn() }
func (s *Mailpit) Port() int                         { return s.p.Cfg.Ports.Mailpit }
func (s *Mailpit) ReadyTimeout() time.Duration       { return 15 * time.Second }
func (s *Mailpit) Prepare(ctx context.Context) error { return nil }

func (s *Mailpit) Spec() proc.Spec {
	c := s.p.Cfg
	return baseSpec(s.p, s.Name(), syscall.SIGTERM,
		s.p.EnvBin("mailpit"),
		"--listen", fmt.Sprintf("%s:%d", c.Nginx.Listen, c.Ports.Mailpit),
		"--smtp", fmt.Sprintf("127.0.0.1:%d", c.Ports.SMTP),
		"--database", filepath.Join(s.p.DataDir(), "mailpit.db"),
		// accept whatever credentials odoo's outgoing mail server has
		"--smtp-auth-accept-any",
		"--smtp-auth-allow-insecure",
	)
}
