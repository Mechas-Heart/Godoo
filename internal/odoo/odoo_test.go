// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/project"
)

func testProject(t *testing.T) *project.Project {
	t.Helper()
	dir := t.TempDir()
	yaml := "name: t\nodoo: {version: '17', addons: [./addons, ./empty]}\n" +
		"ports: {http: 8069, odoo: 20000, gevent: 20002, postgres: 5433, smtp: 1025, mailpit: 8025}\n"
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	// ./addons has one module, ./empty has none
	if err := os.MkdirAll(filepath.Join(dir, "addons", "my_mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "addons", "my_mod", "__manifest__.py"), []byte("{}"), 0o644)
	os.MkdirAll(filepath.Join(dir, "empty"), 0o755)

	p, err := project.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCommandPutsConfigAfterSubcommand(t *testing.T) {
	p := testProject(t)
	conf := p.ConfFile("odoo.conf")
	bin := filepath.Join(p.SourceDir(), "odoo-bin")

	got := Command(p, "shell", "-d", "db")
	want := []string{p.EnvBin("python3"), bin, "shell", "-c", conf, "-d", "db"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shell:\n got %v\nwant %v", got, want)
	}
	got = Command(p, "-u", "sale")
	want = []string{p.EnvBin("python3"), bin, "-c", conf, "-u", "sale"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("update:\n got %v\nwant %v", got, want)
	}
	if got := Command(p, "scaffold", "x", "/tmp"); strings.Contains(strings.Join(got, " "), "-c") {
		t.Errorf("scaffold must not get -c: %v", got)
	}
}

func TestAddonsPathSkipsEmptyDirs(t *testing.T) {
	p := testProject(t)
	got := AddonsPath(p)
	want := []string{filepath.Join(p.SourceDir(), "addons"), filepath.Join(p.Root, "addons")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestConfOptionsOverrides(t *testing.T) {
	p := testProject(t)
	p.Cfg.Odoo.Options = map[string]string{"workers": "4", "limit_time_real": "600"}
	opts := map[string]string{}
	for _, o := range confOptions(p) {
		opts[o.Key] = o.Value
	}
	if opts["workers"] != "4" || opts["limit_time_real"] != "600" {
		t.Errorf("options not applied: %v", opts)
	}
	if opts["smtp_port"] != "1025" || opts["gevent_port"] != "20002" || opts["proxy_mode"] != "True" {
		t.Errorf("unexpected defaults: %v", opts)
	}
}
