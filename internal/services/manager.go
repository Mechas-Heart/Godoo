// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/proc"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/systemd"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

const stopTimeout = 30 * time.Second

// Get returns a service by name.
func Get(p *project.Project, name string) (Service, error) {
	var names []string
	for _, s := range All(p) {
		if s.Name() == name {
			return s, nil
		}
		names = append(names, s.Name())
	}
	return nil, fmt.Errorf("unknown service %q (one of: %s)", name, strings.Join(names, ", "))
}

// Up starts every enabled service that is not already running, in order.
// Services named in skip are left alone (used by `godoo run` for odoo).
func Up(ctx context.Context, p *project.Project, skip ...string) error {
	if err := p.EnsureDirs(); err != nil {
		return err
	}
	for _, s := range All(p) {
		if !s.Enabled() || contains(skip, s.Name()) {
			continue
		}
		if err := Start(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// Start prepares and starts one service, then waits until its port answers.
// Under systemd (see systemd.go) systemctl starts it.
func Start(ctx context.Context, s Service) error {
	spec := s.Spec()
	if mode, ok := Managed(s.proj()); ok {
		if state, pid := systemd.State(ctx, mode, unit(s)); state == "active" {
			ui.Info("%-8s already running (systemd, pid %d)", s.Name(), pid)
			return nil
		}
		if err := startUnit(ctx, mode, s); err != nil {
			return err
		}
		if err := proc.WaitPort(s.Port(), nil, s.ReadyTimeout()); err != nil {
			return fmt.Errorf("%s did not start: %w\n--- last lines of %s ---\n%s",
				s.Name(), err, spec.LogFile, proc.Tail(spec.LogFile, 20))
		}
		_, pid := systemd.State(ctx, mode, unit(s))
		ui.Info("%-8s started (systemd, pid %d, port %d)", s.Name(), pid, s.Port())
		return nil
	}
	if s.proj().Cfg.Env == config.EnvProd {
		return ErrProdNeedsSystemd
	}
	if pid, ok := proc.Running(spec.PidFile); ok {
		ui.Info("%-8s already running (pid %d)", s.Name(), pid)
		return nil
	}
	if !config.PortFree(s.Port()) {
		return fmt.Errorf("%s: port %d is already in use by another program; change it under `ports:` in godoo.yaml", s.Name(), s.Port())
	}
	if err := s.Prepare(ctx); err != nil {
		return fmt.Errorf("%s: %w", s.Name(), err)
	}
	h, err := proc.Start(spec)
	if err != nil {
		return err
	}
	if err := proc.WaitPort(s.Port(), h, s.ReadyTimeout()); err != nil {
		return fmt.Errorf("%s did not start: %w\n--- last lines of %s ---\n%s",
			s.Name(), err, spec.LogFile, proc.Tail(spec.LogFile, 20))
	}
	ui.Info("%-8s started (pid %d, port %d)", s.Name(), h.Pid, s.Port())
	return nil
}

// IsRunning reports whether the service's process is alive.
func IsRunning(s Service) bool {
	if mode, ok := Managed(s.proj()); ok {
		state, _ := systemd.State(context.Background(), mode, unit(s))
		return state == "active"
	}
	_, ok := proc.Running(s.Spec().PidFile)
	return ok
}

// Stop stops one service; it is a no-op when the service is not running.
func Stop(s Service) error {
	if mode, ok := Managed(s.proj()); ok {
		if !IsRunning(s) {
			return nil
		}
		if _, err := systemd.Systemctl(context.Background(), mode, "stop", unit(s)); err != nil {
			return fmt.Errorf("%w\n(try: %s stop %s)", err, systemd.Command(mode), unit(s))
		}
		ui.Info("%-8s stopped (systemd)", s.Name())
		return nil
	}
	spec := s.Spec()
	if _, ok := proc.Running(spec.PidFile); !ok {
		return nil
	}
	if err := proc.Stop(spec.PidFile, spec.StopSignal, stopTimeout); err != nil {
		return fmt.Errorf("stop %s: %w", s.Name(), err)
	}
	ui.Info("%-8s stopped", s.Name())
	return nil
}

// Down stops all services in reverse start order.
func Down(p *project.Project) error {
	all := All(p)
	for i := len(all) - 1; i >= 0; i-- {
		if err := Stop(all[i]); err != nil {
			return err
		}
	}
	return nil
}

type State struct {
	Name    string
	Enabled bool
	Pid     int
	Running bool
	Port    int
	Log     string
}

func Status(p *project.Project) []State {
	var out []State
	mode, managed := Managed(p)
	for _, s := range All(p) {
		spec := s.Spec()
		pid, running := proc.Running(spec.PidFile)
		if managed {
			var state string
			state, pid = systemd.State(context.Background(), mode, unit(s))
			running = state == "active"
		}
		out = append(out, State{s.Name(), s.Enabled(), pid, running, s.Port(), spec.LogFile})
	}
	return out
}

// AnyRunning reports whether at least one service of the project is up.
func AnyRunning(p *project.Project) bool {
	for _, st := range Status(p) {
		if st.Running {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
