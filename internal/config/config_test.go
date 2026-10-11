// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

const ports = `
ports: {http: 8069, odoo: 20000, gevent: 20002, postgres: 5433, smtp: 1025, mailpit: 8025}
`

func load(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(dir)
}

func TestLocalDefaults(t *testing.T) {
	c, err := load(t, "name: shop\nodoo: {version: '17'}\n"+ports)
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != EnvLocal || !c.MailpitOn() || *c.Odoo.Workers != 0 || !*c.Odoo.ListDB {
		t.Errorf("unexpected local defaults: %+v", c)
	}
	if c.Odoo.Version != "17.0" || c.Postgres.Version != "16" || c.Nix.OdooPackage != "odoo17" {
		t.Errorf("version defaults not applied: %+v", c)
	}
	if c.Odoo.DB != "shop" || *c.Odoo.Dev == "" {
		t.Errorf("db/dev defaults not applied: %+v", c.Odoo)
	}
}

func TestProdDefaults(t *testing.T) {
	c, err := load(t, "name: shop\nenv: prod\nodoo: {version: '17', admin_passwd: x}\n"+ports)
	if err != nil {
		t.Fatal(err)
	}
	if c.MailpitOn() || *c.Odoo.Workers != 2 || *c.Odoo.ListDB || *c.Odoo.Dev != "" {
		t.Errorf("unexpected prod defaults: %+v", c.Odoo)
	}
}

func TestInvalid(t *testing.T) {
	cases := map[string]string{
		"prod needs admin_passwd": "name: shop\nenv: prod\nodoo: {version: '17'}\n" + ports,
		"bad name":                "name: Shop!\nodoo: {version: '17'}\n" + ports,
		"missing ports":           "name: shop\nodoo: {version: '17'}\n",
		"duplicate port":          "name: shop\nodoo: {version: '17'}\nports: {http: 1, odoo: 1, gevent: 2, postgres: 3, smtp: 4, mailpit: 5}\n",
		"old odoo":                "name: shop\nodoo: {version: '14.0'}\n" + ports,
	}
	for name, yaml := range cases {
		if _, err := load(t, yaml); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestPortsForSlotDoNotOverlap(t *testing.T) {
	seen := map[int]int{} // port -> slot
	for n := range 20 {
		for _, port := range PortsForSlot(n).List() {
			if prev, dup := seen[port]; dup {
				t.Fatalf("port %d used by slot %d and slot %d", port, prev, n)
			}
			seen[port] = n
		}
	}
}
