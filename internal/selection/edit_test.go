// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package selection

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const existing = `# my comment
web:
  - web_responsive # keep me
---
ONLY:
  GODOO_ENV: [prod]
web:
  - web_prod_only
`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func read(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSelectEditsThePlainDocument(t *testing.T) {
	dir := writeFile(t, existing)

	if c, _, err := Select(dir, "server-ux", []string{"date_range", "base_tier_*"}, nil); err != nil || c != Added {
		t.Fatalf("add: %v %v", c, err)
	}
	if c, _, err := Select(dir, "web", []string{"web_refresher"}, nil); err != nil || c != Extended {
		t.Fatalf("extend: %v %v", c, err)
	}
	if c, _, err := Select(dir, "web", []string{"web_refresher"}, nil); err != nil || c != Unchanged {
		t.Fatalf("again: %v %v", c, err)
	}
	if c, _, err := Select(dir, "mine", []string{"*"}, nil); err != nil || c != Added {
		t.Fatalf("all: %v %v", c, err)
	}
	if c, _, err := Select(dir, "later", nil, nil); err != nil || c != Added {
		t.Fatalf("no modules: %v %v", c, err)
	}
	if c, _, err := Select(dir, "later", nil, nil); err != nil || c != Unchanged {
		t.Fatalf("no modules again: %v %v", c, err)
	}

	got := read(t, dir)
	for _, want := range []string{"# my comment", "# keep me", `"base_tier_*"`, "web_prod_only"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	s, err := Load(dir, "local")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Patterns["web"], []string{"web_responsive", "web_refresher"}) {
		t.Errorf("web = %v (the prod-only document must not change)", s.Patterns["web"])
	}
	if !slices.Equal(s.Patterns["server-ux"], []string{"date_range", "base_tier_*"}) {
		t.Errorf("server-ux = %v", s.Patterns["server-ux"])
	}
	if !slices.Equal(s.Patterns["mine"], []string{"*"}) {
		t.Errorf("mine = %v, want all", s.Patterns["mine"])
	}
	if !s.Has("later") || len(s.Patterns["later"]) != 0 || !strings.Contains(got, "later: []") {
		t.Errorf("later should be an empty entry `later: []`:\n%s", got)
	}
	if c, _, _ := Select(dir, "web", []string{"*"}, nil); c != Extended {
		t.Error("--all on a list should replace it with \"*\"")
	}
	if s, _ := Load(dir, "local"); !slices.Equal(s.Patterns["web"], []string{"*"}) {
		t.Errorf("web = %v, want only \"*\"", s.Patterns["web"])
	}
	if c, _, _ := Select(dir, "mine", []string{"x"}, nil); c != Unchanged {
		t.Error(`a "*" entry already has every module`)
	}
}

func TestSelectCreatesFileKeepingReposWhole(t *testing.T) {
	dir := writeFile(t, "")
	c, undo, err := Select(dir, "server-ux", []string{"date_range"}, []string{"web", "server-ux"})
	if err != nil || c != Created {
		t.Fatalf("%v %v", c, err)
	}
	s, err := Load(dir, "local")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Patterns["web"], []string{"*"}) || !slices.Equal(s.Patterns["server-ux"], []string{"date_range"}) {
		t.Errorf("patterns = %v", s.Patterns)
	}
	undo()
	if _, err := os.Stat(filepath.Join(dir, FileName)); err == nil {
		t.Error("undo should remove a file Select created")
	}
}

func TestSelectUndoRestores(t *testing.T) {
	dir := writeFile(t, existing)
	_, undo, err := Select(dir, "server-ux", []string{"date_range"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	undo()
	if read(t, dir) != existing {
		t.Errorf("undo should restore the file exactly:\n%s", read(t, dir))
	}
}

func TestDeselectRemovesFromEveryDocument(t *testing.T) {
	dir := writeFile(t, existing+"other:\n  - x\n")
	removed, err := Deselect(dir, "web")
	if err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	if strings.Contains(read(t, dir), "web") {
		t.Errorf("web should be gone:\n%s", read(t, dir))
	}
	if removed, _ := Deselect(dir, "nothere"); removed {
		t.Error("nothing to remove")
	}
}
