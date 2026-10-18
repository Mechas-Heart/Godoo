// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package systemd

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func testServices() []Service {
	return []Service{
		{Name: "postgres", Description: "PostgreSQL 16", Args: []string{"/p/.godoo/env/bin/postgres", "-D", "/p/data", "-k", ""},
			Dir: "/p", Path: "/p/.godoo/env/bin:/usr/bin:/bin", LogFile: "/p/.godoo/logs/postgres.log", StopSignal: syscall.SIGINT},
		{Name: "odoo", Description: "Odoo 18.0", Args: []string{"/p/.godoo/env/bin/python3", "/my odoo/odoo-bin", "--dev=reload,qweb"},
			Dir: "/p", Path: "/p/.godoo/env/bin:/usr/bin:/bin", LogFile: "/p/.godoo/logs/odoo.log", StopSignal: syscall.SIGTERM,
			After: []string{"postgres"}, Requires: []string{"postgres"}},
	}
}

func TestRenderUserUnits(t *testing.T) {
	units := Render("shop", "header line", testServices(), User, Owner{})
	if len(units) != 3 || units[2].Name != "godoo-shop.target" {
		t.Fatalf("units = %v", units)
	}
	pg, odoo, target := units[0].Content, units[1].Content, units[2].Content

	for _, want := range []string{
		"# header line",
		`ExecStart=/p/.godoo/env/bin/postgres -D /p/data -k ""`, // the empty -k argument survives
		"KillSignal=SIGINT",
		"PartOf=godoo-shop.target",
		"StandardOutput=append:/p/.godoo/logs/postgres.log",
		"Environment=PATH=/p/.godoo/env/bin:/usr/bin:/bin",
	} {
		if !strings.Contains(pg, want) {
			t.Errorf("postgres unit misses %q:\n%s", want, pg)
		}
	}
	for _, want := range []string{
		`"/my odoo/odoo-bin"`, // a path with a space is quoted
		"After=godoo-shop-postgres.service",
		"Requires=godoo-shop-postgres.service",
	} {
		if !strings.Contains(odoo, want) {
			t.Errorf("odoo unit misses %q:\n%s", want, odoo)
		}
	}
	if strings.Contains(pg+odoo, "User=") {
		t.Error("user units must not set User=")
	}
	for _, want := range []string{"Wants=godoo-shop-postgres.service godoo-shop-odoo.service", "WantedBy=default.target"} {
		if !strings.Contains(target, want) {
			t.Errorf("target misses %q:\n%s", want, target)
		}
	}
}

func TestRenderSystemUnits(t *testing.T) {
	units := Render("shop", "h", testServices(), System, Owner{User: "odoo", Group: "staff"})
	if !strings.Contains(units[0].Content, "User=odoo\nGroup=staff\n") {
		t.Errorf("system units run as the owner:\n%s", units[0].Content)
	}
	if !strings.Contains(units[2].Content, "WantedBy=multi-user.target") {
		t.Errorf("system target starts at boot:\n%s", units[2].Content)
	}
}

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"plain":       "plain",
		"":            `""`,
		"a b":         `"a b"`,
		`say "hi"`:    `"say \"hi\""`,
		"100%":        "100%%",
		`C:\dir`:      `"C:\\dir"`,
		"--dev=xml,a": "--dev=xml,a",
	} {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestWriteInstalledRemove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	units := Render("shop", "h", testServices(), User, Owner{})
	if _, ok := Installed("shop"); ok {
		t.Fatal("nothing installed yet")
	}
	if err := Write(Dir(User), units); err != nil {
		t.Fatal(err)
	}
	if mode, ok := Installed("shop"); !ok || mode != User {
		t.Fatalf("Installed = %v %v", mode, ok)
	}
	os.WriteFile(filepath.Join(Dir(User), "godoo-shop-x-odoo.service"), nil, 0o644) // project "shop-x"
	if err := Remove(Dir(User), "shop"); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(Dir(User), "*"))
	if len(left) != 1 || filepath.Base(left[0]) != "godoo-shop-x-odoo.service" {
		t.Errorf("Remove must only delete this project's units, left %v", left)
	}
}
