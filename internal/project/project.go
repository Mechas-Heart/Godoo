// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package project ties a loaded godoo.yaml to its directory on disk.
//
// Layout of a project (everything godoo generates lives in .godoo/):
//
//	godoo.yaml           your settings
//	addons/              your addons
//	enterprise/          odoo enterprise addons (edition: enterprise)
//	repos/<owner>/<name> addon repositories from `repos:` (godoo addons add)
//	odoo/                odoo source checkout (odoo.source)
//	nix/flake.nix        generated nix environment (+ flake.lock pins)
//	.godoo/env           -> /nix/store/...  python + odoo's packages, postgres,
//	                        nginx, mailpit (no virtualenv: python comes from nix)
//	.godoo/conf/         generated odoo.conf, nginx.conf, postgresql.conf
//	.godoo/data/         postgres cluster, odoo filestore, mailpit db
//	.godoo/logs/         one log file per service
//	.godoo/run/          pid files and nginx temp dirs
package project

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/config"
)

type Project struct {
	Root string
	Cfg  *config.Config
}

// Open finds godoo.yaml starting at dir and loads it.
func Open(dir string) (*Project, error) {
	root, err := config.Find(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	return &Project{Root: root, Cfg: cfg}, nil
}

// Abs resolves a path from godoo.yaml relative to the project root.
func (p *Project) Abs(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(p.Root, path)
}

func (p *Project) state(parts ...string) string {
	return filepath.Join(append([]string{p.Root, ".godoo"}, parts...)...)
}

func (p *Project) StateDir() string { return p.state() }
func (p *Project) NixDir() string   { return filepath.Join(p.Root, "nix") }
func (p *Project) EnvDir() string   { return p.state("env") }
func (p *Project) ConfDir() string  { return p.state("conf") }
func (p *Project) DataDir() string  { return p.state("data") }
func (p *Project) LogDir() string   { return p.state("logs") }
func (p *Project) RunDir() string   { return p.state("run") }

// LinkedAddonsDir holds symlinks to the modules selected in addons.yaml
// (doodba: /opt/odoo/auto/addons).
func (p *Project) LinkedAddonsDir() string { return p.state("addons") }

func (p *Project) SourceDir() string        { return p.Abs(p.Cfg.Odoo.Source) }
func (p *Project) ConfFile(n string) string { return filepath.Join(p.ConfDir(), n) }
func (p *Project) LogFile(svc string) string {
	return filepath.Join(p.LogDir(), svc+".log")
}
func (p *Project) PidFile(svc string) string {
	return filepath.Join(p.RunDir(), svc+".pid")
}

// EnvBin returns the path of a binary inside the nix environment.
func (p *Project) EnvBin(name string) string { return filepath.Join(p.EnvDir(), "bin", name) }

// EnsureDirs creates the .godoo directory skeleton.
func (p *Project) EnsureDirs() error {
	for _, d := range []string{p.ConfDir(), p.DataDir(), p.LogDir(), p.RunDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// EnterpriseDir is the enterprise addons checkout, or "" for community.
func (p *Project) EnterpriseDir() string {
	if !p.Cfg.EnterpriseOn() {
		return ""
	}
	return p.Abs(p.Cfg.Odoo.Enterprise.Path)
}

// AddonsPaths returns the addons directories as absolute paths: odoo.addons
// first (your own modules win), then every entry of repos.
func (p *Project) AddonsPaths() []string {
	out := make([]string, 0, len(p.Cfg.Odoo.Addons)+len(p.Cfg.Repos))
	for _, a := range p.Cfg.Odoo.Addons {
		out = append(out, p.Abs(a))
	}
	for _, r := range p.Cfg.Repos {
		out = append(out, p.Abs(r.Path))
	}
	return out
}
