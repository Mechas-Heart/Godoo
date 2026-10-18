// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Mechas-Heart/Godoo/internal/proc"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/templates"
)

// Nginx is the front door: nginx.listen:ports.http -> odoo. It runs in the
// foreground (daemon off) like every other service, with all of its files
// (pid, temp dirs, logs) inside the project.
type Nginx struct{ p *project.Project }

func (s *Nginx) proj() *project.Project      { return s.p }
func (s *Nginx) Name() string                { return "nginx" }
func (s *Nginx) Enabled() bool               { return true }
func (s *Nginx) Port() int                   { return s.p.Cfg.Ports.HTTP }
func (s *Nginx) ReadyTimeout() time.Duration { return 15 * time.Second }
func (s *Nginx) prefix() string              { return filepath.Join(s.p.RunDir(), "nginx") }

// SIGQUIT is nginx' graceful shutdown.
func (s *Nginx) Spec() proc.Spec {
	return baseSpec(s.p, s.Name(), syscall.SIGQUIT,
		s.p.EnvBin("nginx"),
		"-p", s.prefix(),
		"-c", s.p.ConfFile("nginx.conf"),
		"-e", "stderr", // startup errors go to .godoo/logs/nginx.log
	)
}

type nginxData struct {
	Prefix, MimeTypes, AccessLog string
	Listen, ServerName, MaxBody  string
	HTTPPort, OdooPort, Gevent   int
	Workers                      int
	WebsocketPath                string
}

func (s *Nginx) Prepare(ctx context.Context) error {
	if err := os.MkdirAll(s.prefix(), 0o755); err != nil {
		return err
	}
	c := s.p.Cfg
	mime := filepath.Join(s.p.EnvDir(), "conf", "mime.types")
	if _, err := os.Stat(mime); err != nil {
		mime = ""
	}
	data := nginxData{
		Prefix:        s.prefix(),
		MimeTypes:     mime,
		AccessLog:     s.p.LogFile("nginx-access"),
		Listen:        c.Nginx.Listen,
		ServerName:    c.Nginx.ServerName,
		MaxBody:       c.Nginx.MaxBodySize,
		HTTPPort:      c.Ports.HTTP,
		OdooPort:      c.Ports.Odoo,
		Gevent:        c.Ports.Gevent,
		Workers:       *c.Odoo.Workers,
		WebsocketPath: c.Profile.WebsocketPath(),
	}
	_, err := templates.WriteFile("nginx.conf.tmpl", s.p.ConfFile("nginx.conf"), data)
	return err
}
