// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package pypi

import (
	"reflect"
	"testing"
)

func TestDependencies(t *testing.T) {
	requires := []string{
		"lxml",
		"cssselect>=1.0",
		`importlib_metadata; python_version < "3.8"`,
		`tomli; python_version < "3.11"`,
		`pywin32; sys_platform == "win32"`,
		`pytest; extra == "test"`,
		`typing-extensions (>=4.0)`,
	}
	got := Dependencies(requires, "3.12.9")
	want := []string{"lxml", "cssselect", "typing-extensions"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("3.12: got %v want %v", got, want)
	}
	got = Dependencies(requires, "3.10")
	want = []string{"lxml", "cssselect", "tomli", "typing-extensions"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("3.10: got %v want %v", got, want)
	}
}

func TestSRI(t *testing.T) {
	// sha256 of the empty string, in hex and in nix's SRI form
	got, err := sri("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	if want := "sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="; err != nil || got != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
	if _, err := sri("nothex"); err == nil {
		t.Error("expected error")
	}
}

func TestIsPureWheel(t *testing.T) {
	for name, want := range map[string]bool{
		"OdooRPC-0.10.1-py2.py3-none-any.whl":              true,
		"odoo_test_helper-2.1.3-py3-none-any.whl":          true,
		"lxml-5.2.1-cp312-cp312-manylinux_2_28_x86_64.whl": false,
		"something-1.0-py2-none-any.whl":                   false,
	} {
		if got := isPureWheel(name); got != want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}
