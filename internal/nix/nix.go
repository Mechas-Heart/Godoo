// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package nix generates the project's flake and drives the nix CLI.
//
// The flake's packages.default is everything a project runs: a Python with
// all of Odoo's packages, postgres, nginx, mailpit, ... It is built and
// linked to .godoo/env; that link is a GC root, so `nix-collect-garbage`
// leaves it alone. Projects with the same Odoo version and flake.lock share
// the same store paths.
package nix

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/templates"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// Flags enable flakes even if the user's nix.conf does not.
var baseArgs = []string{"--extra-experimental-features", "nix-command flakes"}

func bin() string {
	if b := os.Getenv("GODOO_NIX"); b != "" {
		return b
	}
	return "nix"
}

// flakeRef uses "path:" so the flake works even when nix/ is not committed
// to git (a "git+file:" flake would only see tracked files).
func flakeRef(p *project.Project) string { return "path:" + p.NixDir() }

func command(ctx context.Context, args ...string) *exec.Cmd {
	all := append(append([]string{}, baseArgs...), args...)
	ui.Debug("%s %s", bin(), strings.Join(all, " "))
	cmd := exec.CommandContext(ctx, bin(), all...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}

// Version returns `nix --version`, or an error if nix is not installed.
func Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, bin(), "--version").Output()
	if err != nil {
		return "", fmt.Errorf("nix not found: install it from https://nixos.org/download (%w)", err)
	}
	return strings.TrimSpace(string(out)), nil
}

type flakeData struct {
	Name, Version, Env     string
	Nixpkgs, PythonNixpkgs string
	SamePin                bool
	OdooPackage            string
	PythonPackages         []string
	LocalPythonPackages    []string // empty unless env is local
	PypiPackages           []config.PypiPackage
	Postgres               string
	Mailpit                bool
	Packages               []string
	LocalPackages          []string // empty unless env is local
}

// WriteFlake renders nix/flake.nix from godoo.yaml.
func WriteFlake(p *project.Project) (changed bool, err error) {
	c := p.Cfg
	python, localPython := c.PythonPackagesForEnv()
	tools, localTools := c.PackagesForEnv()

	data := flakeData{
		Name:                c.Name,
		Version:             c.Odoo.Version,
		Env:                 c.Env,
		Nixpkgs:             c.Nix.Nixpkgs,
		PythonNixpkgs:       c.Nix.PythonNixpkgs,
		SamePin:             c.Nix.Nixpkgs == c.Nix.PythonNixpkgs,
		OdooPackage:         c.Nix.OdooPackage,
		PythonPackages:      python,
		LocalPythonPackages: localPython,
		PypiPackages:        c.PypiPackagesForEnv(),
		Postgres:            c.Postgres.Version,
		Mailpit:             c.MailpitOn(),
		Packages:            tools,
		LocalPackages:       localTools,
	}
	return templates.WriteFile("flake.nix.tmpl", filepath.Join(p.NixDir(), "flake.nix"), data)
}

// BuildEnv builds packages.default and points .godoo/env at it.
func BuildEnv(ctx context.Context, p *project.Project) error {
	args := []string{"build", flakeRef(p), "--out-link", p.EnvDir()}
	if ui.Verbose {
		args = append(args, "--print-build-logs")
	}
	return command(ctx, args...).Run()
}

// Update refreshes flake.lock to the newest revisions of the pinned channels.
func Update(ctx context.Context, p *project.Project) error {
	return command(ctx, "flake", "update", "--flake", flakeRef(p)).Run()
}
