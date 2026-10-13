// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/nix"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/registry"
	"github.com/Mechas-Heart/Godoo/internal/templates"
	"github.com/Mechas-Heart/Godoo/internal/ui"
	"github.com/Mechas-Heart/Godoo/internal/versions"
)

const gitignore = `# godoo: generated state (env link, data, logs), the odoo checkout, addon
# repos and nix/flake.nix (it differs per env) - all recreated from
# godoo.yaml by ` + "`godoo up`" + `. Do commit nix/flake.lock: it pins the versions.
.godoo/
odoo/
repos/
nix/flake.nix

# Odoo Enterprise is proprietary: never commit it, especially to a public repo.
enterprise/
`

type initData struct {
	Name, Env, Version, Source string
	Edition, EnterpriseRepo    string
	AdminPasswd, PGPassword    string
	Postgres                   string
	Nixpkgs, PythonNixpkgs     string
	OdooPackage                string
	Ports                      config.Ports
}

func cmdInit(ctx context.Context, args []string) error {
	fs := newFlags("init")
	version := fs.String("version", "18.0", "odoo version ("+strings.Join(versions.Supported(), ", ")+")")
	name := fs.String("name", "", "project name (default: directory name)")
	env := fs.String("env", config.EnvLocal, "local or prod")
	source := fs.String("source", "./odoo", "path of the odoo checkout (cloned there if missing)")
	edition := fs.String("edition", config.EditionCommunity, "community or enterprise")
	entRepo := fs.String("enterprise-repo", config.DefaultEnterpriseURL, "enterprise addons repository (with --edition enterprise)")
	pos, _, err := parse(fs, args)
	if err != nil {
		return err
	}

	dir := workDir
	if len(pos) > 0 {
		dir = filepath.Join(workDir, pos[0])
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, config.FileName)); err == nil {
		return fmt.Errorf("%s already exists in %s", config.FileName, dir)
	}
	if *name == "" {
		*name = sanitizeName(filepath.Base(dir))
	}
	if *env != config.EnvLocal && *env != config.EnvProd {
		return fmt.Errorf("--env must be %s or %s", config.EnvLocal, config.EnvProd)
	}
	if *edition != config.EditionCommunity && *edition != config.EditionEnterprise {
		return fmt.Errorf("--edition must be %s or %s", config.EditionCommunity, config.EditionEnterprise)
	}
	profile, err := versions.ProfileFor(*version)
	if err != nil {
		return err
	}
	ports, err := config.AllocatePorts(reservedPorts())
	if err != nil {
		return err
	}

	data := initData{
		Name: *name, Env: *env, Version: profile.Branch, Source: *source,
		Edition: *edition, EnterpriseRepo: *entRepo,
		AdminPasswd: "admin", PGPassword: "odoo",
		Postgres: profile.Postgres, Ports: ports,
		Nixpkgs: versions.ServicesNixpkgs, PythonNixpkgs: profile.PythonNixpkgs, OdooPackage: profile.OdooPackage,
	}
	if *env == config.EnvProd {
		data.AdminPasswd, data.PGPassword = randomSecret(), randomSecret()
	}

	ui.Step("creating project %s (Odoo %s, %s) in %s", data.Name, data.Version, data.Env, dir)
	if err := os.MkdirAll(filepath.Join(dir, "addons"), 0o755); err != nil {
		return err
	}
	if _, err := templates.WriteFile("godoo.yaml.tmpl", filepath.Join(dir, config.FileName), data); err != nil {
		return err
	}
	writeIfMissing(filepath.Join(dir, ".gitignore"), gitignore)

	p, err := project.Open(dir)
	if err != nil {
		return fmt.Errorf("generated config is invalid (bug): %w", err)
	}
	if _, err := nix.WriteFlake(p); err != nil {
		return err
	}
	if err := registry.Add(p.Cfg.Name, p.Root); err != nil {
		ui.Warn("could not register project: %v", err)
	}

	ui.Info("wrote godoo.yaml, nix/flake.nix, addons/")
	ui.Info("ports: http %d, postgres %d, mailpit %d", ports.HTTP, ports.Postgres, ports.Mailpit)
	fmt.Printf("\nNext:\n  cd %s\n  godoo up        # first run builds the env and clones odoo\n", dir)
	return nil
}

// reservedPorts collects the ports of all registered projects, so a stopped
// project keeps its ports and a new one does not steal them.
func reservedPorts() map[int]bool {
	reserved := map[int]bool{}
	entries, _ := registry.All()
	for _, e := range entries {
		cfg, err := config.Load(e.Path)
		if err != nil {
			continue
		}
		for _, port := range cfg.Ports.List() {
			reserved[port] = true
		}
	}
	return reserved
}

var invalidName = regexp.MustCompile(`[^a-z0-9_-]+`)

func sanitizeName(s string) string {
	s = strings.Trim(invalidName.ReplaceAllString(strings.ToLower(s), "-"), "-_")
	if s == "" {
		return "odoo"
	}
	return s
}

func writeIfMissing(path, content string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			ui.Warn("write %s: %v", path, err)
		}
	}
}

func randomSecret() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
