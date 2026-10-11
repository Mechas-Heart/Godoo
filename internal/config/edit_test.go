// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const editable = `# my project
name: shop
odoo:
  version: "17.0"   # keep me
` + ports + `
nix:
  python_packages: []   # extra packages
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEditKeepsCommentsAndAddsValues(t *testing.T) {
	dir := writeConfig(t, editable)
	err := Edit(dir, func(root *yaml.Node) error {
		pkgs := MapChild(MapChild(root, "nix", yaml.MappingNode), "python_packages", yaml.SequenceNode)
		SeqAdd(pkgs, "pandas", "paramiko", "pandas")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, FileName))
	for _, want := range []string{"# my project", "# keep me", `version: "17.0"`, "pandas", "paramiko"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.Nix.PythonPackages, ",") != "pandas,paramiko" {
		t.Errorf("python_packages = %v", c.Nix.PythonPackages)
	}
}

func TestEditRollsBackInvalidConfig(t *testing.T) {
	dir := writeConfig(t, editable)
	err := Edit(dir, func(root *yaml.Node) error {
		odoo := MapChild(root, "odoo", yaml.MappingNode)
		MapChild(odoo, "version", yaml.ScalarNode).Value = "10.0" // unsupported
		return nil
	})
	if err == nil {
		t.Fatal("expected the edit to be rejected")
	}
	out, _ := os.ReadFile(filepath.Join(dir, FileName))
	if string(out) != editable {
		t.Errorf("file was not restored:\n%s", out)
	}
}

func TestSeqRemove(t *testing.T) {
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	SeqAdd(seq, "a", "b", "c")
	if removed := SeqRemove(seq, "b", "x"); strings.Join(removed, ",") != "b" {
		t.Errorf("removed = %v", removed)
	}
	if got := strings.Join(SeqStrings(seq), ","); got != "a,c" {
		t.Errorf("left = %v", got)
	}
}
