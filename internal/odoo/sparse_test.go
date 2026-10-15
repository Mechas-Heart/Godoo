// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/selection"
)

// gitRepo makes a repo on branch 17.0 with three modules and a README.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	dir := t.TempDir()
	for _, m := range []string{"web_a", "web_b", "other_c"} {
		os.MkdirAll(filepath.Join(dir, m), 0o755)
		os.WriteFile(filepath.Join(dir, m, "__manifest__.py"), []byte("{}"), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644)
	for _, args := range [][]string{
		{"init", "-q", "-b", "17.0"},
		{"add", "."},
		{"commit", "-q", "-m", "init"},
		{"config", "uploadpack.allowFilter", "true"}, // let --filter=blob:none work over file://
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestSparseCheckoutFollowsAddonsYaml(t *testing.T) {
	ctx := context.Background()
	p := testProject(t)
	r := config.Repo{Name: "x", URL: "file://" + gitRepo(t), Branch: "17.0", Path: "./repos/x"}
	p.Cfg.Repos = []config.Repo{r}
	dir := p.Abs(r.Path)

	// writes addons.yaml, syncs the checkout and returns the modules on disk
	sync := func(content string) []string {
		t.Helper()
		os.WriteFile(filepath.Join(p.Root, selection.FileName), []byte(content), 0o644)
		sel, err := selection.Load(p.Root, "local")
		if err != nil {
			t.Fatal(err)
		}
		if err := SyncRepoCheckout(ctx, p, sel, r); err != nil {
			t.Fatal(err)
		}
		return ModulesIn(dir)
	}

	// 1. a fresh clone gets only the selected modules (and top-level files)
	os.WriteFile(filepath.Join(p.Root, selection.FileName), []byte("x:\n  - web_*\n"), 0o644)
	if err := CloneAddonRepo(ctx, p, r); err != nil {
		t.Fatal(err)
	}
	if got := ModulesIn(dir); !slices.Equal(got, []string{"web_a", "web_b"}) {
		t.Errorf("after clone: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Error("top-level files should be checked out")
	}
	if st := StatusOf(ctx, p, r); st.ModulesColumn() != "2 of 3" {
		t.Errorf("addons list column = %q, want \"2 of 3\"", st.ModulesColumn())
	}

	// 2. another selection swaps the folders
	if got := sync("x:\n  - other_c\n"); !slices.Equal(got, []string{"other_c"}) {
		t.Errorf("after changing addons.yaml: %v", got)
	}
	// 3. "*" checks out everything again
	if got := sync("x: [\"*\"]\n"); !slices.Equal(got, []string{"other_c", "web_a", "web_b"}) {
		t.Errorf("with \"*\": %v", got)
	}
	if st := StatusOf(ctx, p, r); st.ModulesColumn() != "3" {
		t.Errorf("full checkout column = %q", st.ModulesColumn())
	}
	// 4. and back to partial
	if got := sync("x:\n  - web_b\n"); !slices.Equal(got, []string{"web_b"}) {
		t.Errorf("back to partial: %v", got)
	}
}

func TestPartial(t *testing.T) {
	r := config.Repo{Name: "web"}
	cases := []struct {
		yaml string
		want bool
	}{
		{"web:\n  - web_responsive\n", true},
		{"web:\n  - \"*\"\n", false},
		{"other:\n  - x\n", false}, // no entry: whole repo
	}
	for _, c := range cases {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, selection.FileName), []byte(c.yaml), 0o644)
		sel, err := selection.Load(dir, "local")
		if err != nil {
			t.Fatal(err)
		}
		if got := partial(sel, r); got != c.want {
			t.Errorf("%q: partial = %v, want %v", c.yaml, got, c.want)
		}
	}
	if partial(nil, r) {
		t.Error("no addons.yaml: whole repo")
	}
}

func TestFullCloneIsReplacedWhenSafe(t *testing.T) {
	ctx := context.Background()
	origin := gitRepo(t)

	// setup: a whole clone of origin (as before addons.yaml), with a marker
	// file inside .git that only survives if the clone is kept
	setup := func(t *testing.T) (r config.Repo, dir string, sync func() error) {
		p := testProject(t)
		r = config.Repo{Name: "x", URL: "file://" + origin, Branch: "17.0", Path: "./repos/x"}
		p.Cfg.Repos = []config.Repo{r}
		if err := CloneRepo(ctx, p, r); err != nil {
			t.Fatal(err)
		}
		dir = p.Abs(r.Path)
		os.WriteFile(filepath.Join(dir, ".git", "marker"), nil, 0o644)
		os.WriteFile(filepath.Join(p.Root, selection.FileName), []byte("x:\n  - web_a\n"), 0o644)
		sel, err := selection.Load(p.Root, "local")
		if err != nil {
			t.Fatal(err)
		}
		return r, dir, func() error { return SyncRepoCheckout(ctx, p, sel, r) }
	}
	kept := func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, ".git", "marker"))
		return err == nil
	}
	run := func(t *testing.T, dir string, args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	t.Run("clean clone is replaced", func(t *testing.T) {
		_, dir, sync := setup(t)
		if err := sync(); err != nil {
			t.Fatal(err)
		}
		if kept(dir) {
			t.Error("a clean full clone should be cloned again")
		}
		if got := ModulesIn(dir); !slices.Equal(got, []string{"web_a"}) {
			t.Errorf("modules = %v", got)
		}
		if _, err := os.Stat(dir + ".godoo-old"); err == nil {
			t.Error("the old clone should be deleted")
		}
	})

	t.Run("local changes keep the clone", func(t *testing.T) {
		_, dir, sync := setup(t)
		os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed"), 0o644)
		if err := sync(); err != nil {
			t.Fatal(err)
		}
		if !kept(dir) {
			t.Fatal("a clone with local changes must not be replaced")
		}
		if got := ModulesIn(dir); !slices.Equal(got, []string{"web_a"}) {
			t.Errorf("should still be narrowed in place: %v", got)
		}
	})

	t.Run("narrowed in place is replaced once it is safe", func(t *testing.T) {
		_, dir, sync := setup(t)
		os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed"), 0o644)
		if err := sync(); err != nil { // kept, narrowed in place
			t.Fatal(err)
		}
		run(t, dir, "checkout", "README.md") // the change is dropped
		if err := sync(); err != nil {
			t.Fatal(err)
		}
		if kept(dir) {
			t.Error("a repo narrowed in place should be cloned again once clean")
		}
		if err := sync(); err != nil { // and then left alone
			t.Fatal(err)
		}
		if got := ModulesIn(dir); !slices.Equal(got, []string{"web_a"}) {
			t.Errorf("modules = %v", got)
		}
	})

	t.Run("unpushed commits keep the clone", func(t *testing.T) {
		_, dir, sync := setup(t)
		os.WriteFile(filepath.Join(dir, "web_a", "x.py"), []byte("x"), 0o644)
		run(t, dir, "add", ".")
		run(t, dir, "commit", "-q", "-m", "local work")
		if err := sync(); err != nil {
			t.Fatal(err)
		}
		if !kept(dir) {
			t.Fatal("a clone with unpushed commits must not be replaced")
		}
	})

	t.Run("failed clone puts the old one back", func(t *testing.T) {
		r, dir, _ := setup(t)
		p, _ := project.Open(filepath.Dir(filepath.Dir(dir)))
		r.URL = "file:///nonexistent/repo.git"
		sel, _ := selection.Load(p.Root, "local")
		if err := SyncRepoCheckout(ctx, p, sel, r); err == nil {
			t.Fatal("expected the clone to fail")
		}
		if !kept(dir) || len(ModulesIn(dir)) != 3 {
			t.Errorf("the full clone should be back untouched (marker kept: %v, modules: %v)", kept(dir), ModulesIn(dir))
		}
	})
}

func TestWanted(t *testing.T) {
	r := config.Repo{Name: "web"}
	if !Wanted(nil, r) {
		t.Error("no addons.yaml: every repo is downloaded whole")
	}
	for yaml, want := range map[string]bool{
		"web: []\n":                  false, // registered by `addons add`, nothing picked
		"other:\n  - x\n":            false, // no entry
		"web:\n  - web_responsive\n": true,
		"web: [\"*\"]\n":             true,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, selection.FileName), []byte(yaml), 0o644)
		sel, err := selection.Load(dir, "local")
		if err != nil {
			t.Fatal(err)
		}
		if got := Wanted(sel, r); got != want {
			t.Errorf("%q: Wanted = %v, want %v", yaml, got, want)
		}
	}
}
