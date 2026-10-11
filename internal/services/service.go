// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package services defines the processes of an instance - postgres, mailpit,
// odoo, nginx - one file each. manager.go starts and stops them in order.
package services

import (
	"context"
	"syscall"
	"time"

	"github.com/Mechas-Heart/Godoo/internal/proc"
	"github.com/Mechas-Heart/Godoo/internal/project"
)

type Service interface {
	Name() string
	Enabled() bool
	// Prepare renders config files and initialises data before each start.
	Prepare(ctx context.Context) error
	// Spec is the command line, environment and signal used to run it.
	Spec() proc.Spec
	// Port is the TCP port that signals "ready" once something listens on it.
	Port() int
	// ReadyTimeout is how long to wait for Port.
	ReadyTimeout() time.Duration
}

// All returns the services in start order.
func All(p *project.Project) []Service {
	return []Service{
		&Postgres{p},
		&Mailpit{p},
		&Odoo{p},
		&Nginx{p},
	}
}

// baseSpec fills the parts every service shares: env, log file, pid file.
func baseSpec(p *project.Project, name string, sig syscall.Signal, args ...string) proc.Spec {
	return proc.Spec{
		Name:       name,
		Args:       args,
		Env:        p.Environ(),
		Dir:        p.Root,
		LogFile:    p.LogFile(name),
		PidFile:    p.PidFile(name),
		StopSignal: sig,
	}
}
