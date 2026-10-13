// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package selection

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Mechas-Heart/Godoo/internal/project"
)

const sample = `
web:
  - web_responsive
server-tools:
  - "*date*"
  - auditlog
---
ONLY:
  GODOO_ENV: [local]
web:
  - web_environment_ribbon
---
ONLY:
  GODOO_ENV: [prod]
server-tools:
  - auth_*
---
ENV:
  DEFAULT_REPO_PATTERN: https://github.com/acme/{}.git
  ODOO_VERSION: "18.0-custom"
acme-addons:
  - acme_*
`

func load(t *testing.T, content string) *Selection {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir, "local")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLoadMergesDocumentsAndAppliesOnly(t *testing.T) {
	s := load(t, sample) // env "local", although GODOO_ENV is not set
	if !slices.Equal(s.Patterns["web"], []string{"web_responsive", "web_environment_ribbon"}) {
		t.Errorf("web = %v", s.Patterns["web"])
	}
	if !slices.Equal(s.Patterns["server-tools"], []string{"*date*", "auditlog"}) {
		t.Errorf("server-tools = %v (the prod document must be skipped)", s.Patterns["server-tools"])
	}
	if s.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", s.Skipped)
	}
	d := s.Downloads["acme-addons"]
	if d.Pattern != "https://github.com/acme/{}.git" || d.Branch != "18.0-custom" {
		t.Errorf("download = %+v", d)
	}
	if s.Downloads["web"].Pattern != DefaultRepoPattern {
		t.Errorf("web should use the OCA pattern, got %+v", s.Downloads["web"])
	}
}

func TestLoadWithoutFile(t *testing.T) {
	s, err := Load(t.TempDir(), "local")
	if s != nil || err != nil {
		t.Errorf("no addons.yaml: got %v, %v", s, err)
	}
}

// makeModules creates module directories (with a manifest) in dir.
func makeModules(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, n, "__manifest__.py"), []byte("{}"), 0o644)
	}
}

func TestLink(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "addons")
	web := filepath.Join(root, "web")
	tools := filepath.Join(root, "server-tools")
	other := filepath.Join(root, "other")
	makeModules(t, private, "my_module")
	makeModules(t, web, "web_responsive", "web_widget_x", "auditlog")
	makeModules(t, tools, "date_range", "base_date_utils", "auditlog", "queue_job")
	makeModules(t, other, "something")

	s := load(t, "web:\n  - web_responsive\n  - auditlog\nserver-tools:\n  - \"*date*\"\n  - auditlog\n  - missing_one\n")
	res := MakePlan(s, []Source{
		{Name: Private, Dirs: []string{private}},
		{Name: "web", Dirs: []string{web}},
		{Name: "server-tools", Dirs: []string{tools}},
		{Name: "other", Dirs: []string{other}},
	})
	want := []string{"auditlog", "base_date_utils", "date_range", "web_responsive"}
	if !slices.Equal(res.Linked(), want) {
		t.Errorf("linked = %v, want %v", res.Linked(), want)
	}

	out := filepath.Join(root, ".godoo", "addons")
	if _, err := os.Stat(out); err == nil {
		t.Fatal("MakePlan must not touch the disk")
	}
	if err := Apply(out, res); err != nil {
		t.Fatal(err)
	}
	if got := project.ModulesIn(out); !slices.Equal(got, want) {
		t.Errorf("modules in %s = %v (symlinks must count as modules)", out, got)
	}
	if target, _ := os.Readlink(filepath.Join(out, "auditlog")); target != filepath.Join(web, "auditlog") {
		t.Errorf("auditlog should come from web (first source), got %s", target)
	}
	if len(res.Direct) != 1 || res.Direct[0].Name != Private {
		t.Errorf("private without an entry should stay direct: %+v", res.Direct)
	}
	joined := strings.Join(res.Warnings, "\n")
	for _, w := range []string{`no module matches "missing_one"`, "auditlog is in both web and server-tools", `repo "other" has no entry`} {
		if !strings.Contains(joined, w) {
			t.Errorf("missing warning %q in:\n%s", w, joined)
		}
	}
}
