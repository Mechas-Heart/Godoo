// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"slices"
	"testing"
)

const withPackages = `
nix:
  python_packages: [pandas]
  local_python_packages: [pytest-odoo]
  packages: [imagemagick]
  local_packages: [httpie]
  pypi_packages:
    - {name: openupgradelib, version: "1", url: "https://x/a.whl", hash: "sha256-x"}
    - {name: odoo-test-helper, version: "1", url: "https://x/b.whl", hash: "sha256-y", local: true}
`

func TestLocalGetsDevTools(t *testing.T) {
	c, err := load(t, "name: shop\nodoo: {version: '17'}\n"+ports+withPackages)
	if err != nil {
		t.Fatal(err)
	}
	shared, local := c.PythonPackagesForEnv()
	if !slices.Equal(shared, []string{"pandas"}) {
		t.Errorf("shared python = %v", shared)
	}
	for _, want := range []string{"watchdog", "debugpy", "ipython", "pytest-odoo"} {
		if !slices.Contains(local, want) {
			t.Errorf("local python is missing %s: %v", want, local)
		}
	}
	_, localTools := c.PackagesForEnv()
	for _, want := range []string{"ruff", "pre-commit", "pgcli", "httpie"} {
		if !slices.Contains(localTools, want) {
			t.Errorf("local tools missing %s: %v", want, localTools)
		}
	}
	if len(c.PypiPackagesForEnv()) != 2 {
		t.Errorf("local env should keep both wheels")
	}
}

func TestProdLeavesLocalOnlyOut(t *testing.T) {
	c, err := load(t, "name: shop\nenv: prod\nodoo: {version: '17', admin_passwd: s3cret}\n"+ports+withPackages)
	if err != nil {
		t.Fatal(err)
	}
	shared, local := c.PythonPackagesForEnv()
	tools, localTools := c.PackagesForEnv()
	if len(local) != 0 || len(localTools) != 0 {
		t.Errorf("prod got local-only packages: %v %v", local, localTools)
	}
	if !slices.Equal(shared, []string{"pandas"}) || !slices.Equal(tools, []string{"imagemagick"}) {
		t.Errorf("prod lost shared packages: %v %v", shared, tools)
	}
	if w := c.PypiPackagesForEnv(); len(w) != 1 || w[0].Name != "openupgradelib" {
		t.Errorf("prod wheels = %v", w)
	}
	if c.MailpitOn() {
		t.Error("prod should not run mailpit")
	}
}

func TestDevToolsOff(t *testing.T) {
	c, err := load(t, "name: shop\nodoo: {version: '17'}\n"+ports+"nix: {dev_tools: false}\n")
	if err != nil {
		t.Fatal(err)
	}
	_, local := c.PythonPackagesForEnv()
	_, localTools := c.PackagesForEnv()
	if !slices.Equal(local, []string{"watchdog"}) || len(localTools) != 0 {
		t.Errorf("dev_tools: false should keep only watchdog: %v %v", local, localTools)
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	t.Setenv(EnvVarEnv, "prod")
	if _, err := load(t, "name: shop\nodoo: {version: '17', admin_passwd: admin}\n"+ports); err == nil {
		t.Error("GODOO_ENV=prod with admin_passwd admin should be refused")
	}

	t.Setenv(EnvVarAdminPasswd, "from-env")
	c, err := load(t, "name: shop\nodoo: {version: '17', admin_passwd: admin}\n"+ports)
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != EnvProd || c.EnvSource != EnvVarEnv || c.Odoo.AdminPasswd != "from-env" {
		t.Errorf("overrides not applied: env=%s source=%s passwd=%s", c.Env, c.EnvSource, c.Odoo.AdminPasswd)
	}
}
