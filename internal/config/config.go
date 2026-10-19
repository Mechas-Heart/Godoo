// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package config loads and validates godoo.yaml.
//
// Anything left empty in godoo.yaml is filled in by resolve() from the Odoo
// version profile, so `godoo config` always shows the complete picture.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Mechas-Heart/Godoo/internal/versions"
)

const (
	FileName = "godoo.yaml"

	EnvLocal = "local"
	EnvProd  = "prod"
)

type Config struct {
	Name string `yaml:"name"`
	Env  string `yaml:"env"`
	// GodooVersion is the godoo release the project is made with (`godoo
	// pin`); another version warns, and refuses to install prod services.
	GodooVersion string         `yaml:"godoo_version"`
	Odoo         OdooConfig     `yaml:"odoo"`
	Ports        Ports          `yaml:"ports"`
	Postgres     PostgresConfig `yaml:"postgres"`
	Nginx        NginxConfig    `yaml:"nginx"`
	Mailpit      MailpitConfig  `yaml:"mailpit"`
	SMTP         SMTPConfig     `yaml:"smtp"`
	Nix          NixConfig      `yaml:"nix"`
	Repos        []Repo         `yaml:"repos"`

	// EnvSource says where Env came from: godoo.yaml or GODOO_ENV.
	EnvSource string `yaml:"-"`

	// Profile holds the version defaults this config was resolved with.
	Profile versions.Profile `yaml:"-"`
}

type OdooConfig struct {
	Version        string            `yaml:"version"`
	Repo           string            `yaml:"repo"`
	Source         string            `yaml:"source"`
	Addons         []string          `yaml:"addons"`
	DB             string            `yaml:"db"`
	Dev            *string           `yaml:"dev"`
	Workers        *int              `yaml:"workers"`
	MaxCronThreads *int              `yaml:"max_cron_threads"`
	AdminPasswd    string            `yaml:"admin_passwd"`
	DBFilter       string            `yaml:"dbfilter"`
	ListDB         *bool             `yaml:"list_db"`
	Options        map[string]string `yaml:"options"`

	Edition    string           `yaml:"edition"`    // community | enterprise (see edition.go)
	Enterprise EnterpriseConfig `yaml:"enterprise"` // where the enterprise addons come from
}

type PostgresConfig struct {
	Version  string `yaml:"version"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type NginxConfig struct {
	Listen      string `yaml:"listen"`
	ServerName  string `yaml:"server_name"`
	MaxBodySize string `yaml:"max_body_size"`
}

type MailpitConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// SMTPConfig is used when mailpit is disabled (e.g. env: prod).
type SMTPConfig struct {
	Server   string `yaml:"server"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	SSL      bool   `yaml:"ssl"`
	From     string `yaml:"from"`
}

type NixConfig struct {
	Nixpkgs        string        `yaml:"nixpkgs"`         // pin for postgres, nginx, mailpit, ...
	PythonNixpkgs  string        `yaml:"python_nixpkgs"`  // pin python + odoo's packages come from
	OdooPackage    string        `yaml:"odoo_package"`    // nixpkgs odoo package whose deps are used
	PythonPackages []string      `yaml:"python_packages"` // extra python packages (nixpkgs names) for your addons
	PypiPackages   []PypiPackage `yaml:"pypi_packages"`   // not in nixpkgs: pure-python wheels nix builds
	Packages       []string      `yaml:"packages"`        // extra nixpkgs packages for the env

	// Only included when env is local (see devtools.go).
	DevTools            *bool    `yaml:"dev_tools"`             // godoo's default dev set; default true
	LocalPythonPackages []string `yaml:"local_python_packages"` // `godoo pkg add --local`
	LocalPackages       []string `yaml:"local_packages"`        // extra tools for development
}

// PypiPackage is a pinned wheel from PyPI, written by `godoo pkg add` when
// nixpkgs lacks a package. Nix fetches it by hash; nothing runs pip.
type PypiPackage struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	URL     string `yaml:"url"`
	Hash    string `yaml:"hash"`
	Local   bool   `yaml:"local,omitempty"` // only for env: local
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Find walks up from dir to the directory containing godoo.yaml.
func Find(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, FileName)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found here or in any parent directory (run `godoo init`)", FileName)
		}
		dir = parent
	}
}

// Load reads <root>/godoo.yaml and resolves defaults.
func Load(root string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		return nil, err
	}
	c := &Config{}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	c.applyEnvironment()
	if err := c.resolve(); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return c, nil
}

// Environment variables that override godoo.yaml, so one committed project
// can run as local on a laptop and as prod on a server without editing it.
const (
	EnvVarEnv         = "GODOO_ENV"          // local | prod
	EnvVarAdminPasswd = "GODOO_ADMIN_PASSWD" // odoo master password
)

// applyEnvironment applies the GODOO_* overrides and remembers where env
// came from (shown by `godoo status` and `godoo config`).
func (c *Config) applyEnvironment() {
	if v := os.Getenv(EnvVarEnv); v != "" {
		c.Env, c.EnvSource = v, EnvVarEnv
	} else {
		c.EnvSource = FileName
	}
	if v := os.Getenv(EnvVarAdminPasswd); v != "" {
		c.Odoo.AdminPasswd = v
	}
}

// YAML returns the fully resolved config, for `godoo config`.
func (c *Config) YAML() ([]byte, error) { return yaml.Marshal(c) }

func (c *Config) Local() bool     { return c.Env == EnvLocal }
func (c *Config) MailpitOn() bool { return *c.Mailpit.Enabled }

func (c *Config) resolve() error {
	if !nameRe.MatchString(c.Name) {
		return fmt.Errorf("name %q must match %s", c.Name, nameRe)
	}
	if c.Env == "" {
		c.Env = EnvLocal
	}
	if c.Env != EnvLocal && c.Env != EnvProd {
		return fmt.Errorf("env must be %q or %q", EnvLocal, EnvProd)
	}
	profile, err := versions.ProfileFor(c.Odoo.Version)
	if err != nil {
		return err
	}
	c.Profile = profile
	c.Odoo.Version = profile.Branch

	if err := c.resolveOdoo(); err != nil {
		return err
	}
	if err := c.resolveEdition(); err != nil {
		return err
	}
	if err := c.Ports.validate(); err != nil {
		return err
	}
	c.resolvePostgres()
	c.resolveNginx()
	if err := c.resolveNix(); err != nil {
		return err
	}
	if err := c.resolveRepos(); err != nil {
		return err
	}
	if c.Mailpit.Enabled == nil {
		c.Mailpit.Enabled = ptr(c.Local())
	}
	return nil
}

func (c *Config) resolveOdoo() error {
	o := &c.Odoo
	if o.Repo == "" {
		o.Repo = "https://github.com/odoo/odoo.git"
	}
	if o.Source == "" {
		o.Source = "./odoo"
	}
	if len(o.Addons) == 0 {
		o.Addons = []string{"./addons"}
	}
	if o.DB == "" {
		o.DB = c.Name
	}
	if o.Dev == nil {
		dev := ""
		if c.Local() {
			dev = "reload,qweb,xml"
		}
		o.Dev = &dev
	}
	if o.Workers == nil {
		workers := 0
		if !c.Local() {
			workers = 2
		}
		o.Workers = &workers
	}
	if o.MaxCronThreads == nil {
		o.MaxCronThreads = ptr(1)
	}
	if o.ListDB == nil {
		o.ListDB = ptr(c.Local())
	}
	if o.AdminPasswd == "" {
		if !c.Local() {
			return fmt.Errorf("odoo.admin_passwd (or %s) is required when env is prod", EnvVarAdminPasswd)
		}
		o.AdminPasswd = "admin"
	}
	if !c.Local() && o.AdminPasswd == "admin" {
		return fmt.Errorf("env is prod but admin_passwd is still \"admin\"; set a real one in odoo.admin_passwd or %s", EnvVarAdminPasswd)
	}
	return nil
}

func (c *Config) resolvePostgres() {
	pg := &c.Postgres
	if pg.Version == "" {
		pg.Version = c.Profile.Postgres
	}
	if pg.User == "" {
		pg.User = "odoo"
	}
	if pg.Password == "" {
		pg.Password = "odoo"
	}
}

func (c *Config) resolveNginx() {
	n := &c.Nginx
	if n.Listen == "" {
		n.Listen = "127.0.0.1"
	}
	if n.ServerName == "" {
		n.ServerName = "_"
	}
	if n.MaxBodySize == "" {
		n.MaxBodySize = "512m"
	}
}

func (c *Config) resolveNix() error {
	n := &c.Nix
	if n.Nixpkgs == "" {
		n.Nixpkgs = versions.ServicesNixpkgs
	}
	if n.PythonNixpkgs == "" {
		n.PythonNixpkgs = c.Profile.PythonNixpkgs
	}
	if n.OdooPackage == "" {
		n.OdooPackage = c.Profile.OdooPackage
	}
	if n.DevTools == nil {
		n.DevTools = ptr(true)
	}
	for i, w := range n.PypiPackages {
		if w.Name == "" || w.Version == "" || w.URL == "" || !strings.HasPrefix(w.Hash, "sha256-") {
			return fmt.Errorf("nix.pypi_packages[%d]: name, version, url and a sha256- hash are required", i)
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
