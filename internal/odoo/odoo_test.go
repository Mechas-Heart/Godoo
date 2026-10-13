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
	addons, err := ResolveAddons(p)
	if err != nil {
		t.Fatal(err)
	}
	got := addons.Path
	want := []string{filepath.Join(p.SourceDir(), "addons"), filepath.Join(p.Root, "addons")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestConfOptionsOverrides(t *testing.T) {
	p := testProject(t)
	p.Cfg.Odoo.Options = map[string]string{"workers": "4", "limit_time_real": "600"}
	opts := map[string]string{}
	for _, o := range confOptions(p, nil) {
		opts[o.Key] = o.Value
	}
	if opts["workers"] != "4" || opts["limit_time_real"] != "600" {
		t.Errorf("options not applied: %v", opts)
	}
	if opts["smtp_port"] != "1025" || opts["gevent_port"] != "20002" || opts["proxy_mode"] != "True" {
		t.Errorf("unexpected defaults: %v", opts)
	}
}

func TestResolveAddonsWithSelection(t *testing.T) {
	p := testProject(t)
	// a repo with two modules, of which addons.yaml selects one
	repo := filepath.Join(p.Root, "repos", "x")
	for _, m := range []string{"wanted", "unwanted"} {
		os.MkdirAll(filepath.Join(repo, m), 0o755)
		os.WriteFile(filepath.Join(repo, m, "__manifest__.py"), []byte("{}"), 0o644)
	}
	p.Cfg.Repos = []config.Repo{{Name: "x", URL: "https://example.com/x.git", Branch: "17.0", Path: "./repos/x"}}
	os.WriteFile(filepath.Join(p.Root, "addons.yaml"), []byte("x:\n  - wanted\n"), 0o644)

	a, err := ResolveAddons(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(p.Root, "addons"),        // private: no entry, so whole and first
		p.LinkedAddonsDir(),                    // the selected modules
		filepath.Join(p.SourceDir(), "addons"), // core odoo last
	}
	if !reflect.DeepEqual(a.Path, want) {
		t.Errorf("path:\n got %v\nwant %v", a.Path, want)
	}
	if !a.Selected || len(a.Links) != 1 || a.Links["wanted"] != filepath.Join(repo, "wanted") {
		t.Errorf("links = %v", a.Links)
	}
	if _, err := os.Stat(p.LinkedAddonsDir()); err == nil {
		t.Error("ResolveAddons must not write the links (ApplyLinks does)")
	}
	wantScan := []string{filepath.Join(p.Root, "addons"), filepath.Join(repo, "wanted")}
	if !reflect.DeepEqual(a.ScanDirs(p), wantScan) {
		t.Errorf("scan dirs = %v, want %v", a.ScanDirs(p), wantScan)
	}
}
