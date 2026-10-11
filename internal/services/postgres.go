// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Mechas-Heart/Godoo/internal/proc"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// Postgres runs a private cluster in .godoo/data/postgres, TCP only on
// 127.0.0.1 (no unix socket, so long project paths are never a problem).
type Postgres struct{ p *project.Project }

func (s *Postgres) Name() string                { return "postgres" }
func (s *Postgres) Enabled() bool               { return true }
func (s *Postgres) Port() int                   { return s.p.Cfg.Ports.Postgres }
func (s *Postgres) ReadyTimeout() time.Duration { return 30 * time.Second }
func (s *Postgres) dataDir() string             { return filepath.Join(s.p.DataDir(), "postgres") }

// Spec: settings are passed as flags so `ps` shows exactly what runs.
// SIGINT is postgres' "fast shutdown".
func (s *Postgres) Spec() proc.Spec {
	return baseSpec(s.p, s.Name(), syscall.SIGINT,
		s.p.EnvBin("postgres"),
		"-D", s.dataDir(),
		"-p", fmt.Sprint(s.Port()),
		"-h", "127.0.0.1",
		"-k", "", // no unix socket
	)
}

func (s *Postgres) Prepare(ctx context.Context) error {
	versionFile := filepath.Join(s.dataDir(), "PG_VERSION")
	b, err := os.ReadFile(versionFile)
	if err == nil {
		have, want := strings.TrimSpace(string(b)), s.p.Cfg.Postgres.Version
		if have != want {
			return fmt.Errorf("%s was created by PostgreSQL %s but godoo.yaml asks for %s; "+
				"dump your databases and remove the directory, or set postgres.version: %q", s.dataDir(), have, want, have)
		}
		return nil
	}
	return s.initdb(ctx)
}

func (s *Postgres) initdb(ctx context.Context) error {
	c := s.p.Cfg.Postgres
	ui.Info("initialising PostgreSQL %s cluster in %s", c.Version, s.dataDir())
	pwfile := filepath.Join(s.p.RunDir(), "pgpass.tmp")
	if err := os.WriteFile(pwfile, []byte(c.Password+"\n"), 0o600); err != nil {
		return err
	}
	defer os.Remove(pwfile)

	cmd := exec.CommandContext(ctx, s.p.EnvBin("initdb"),
		"-D", s.dataDir(),
		"-U", c.User, // the superuser odoo connects as
		"--pwfile", pwfile,
		"--auth", "scram-sha-256",
		"--encoding", "UTF8",
		"--locale", "C", // avoids host locale issues; odoo creates DBs from template0
	)
	cmd.Env = s.p.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("initdb failed: %w\n%s", err, out)
	}
	return nil
}
