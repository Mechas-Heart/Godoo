// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"reflect"
	"testing"
)

func TestParseMixesFlagsAndArgs(t *testing.T) {
	fs := newFlags("test")
	db := fs.String("d", "", "")
	pos, extra, err := parse(fs, []string{"sale", "-d", "x", "stock", "--", "--log-level=debug", "-d", "y"})
	if err != nil {
		t.Fatal(err)
	}
	if *db != "x" {
		t.Errorf("db = %q", *db)
	}
	if !reflect.DeepEqual(pos, []string{"sale", "stock"}) {
		t.Errorf("positional = %v", pos)
	}
	if !reflect.DeepEqual(extra, []string{"--log-level=debug", "-d", "y"}) {
		t.Errorf("passthrough = %v", extra)
	}
}

func TestSplitModules(t *testing.T) {
	got, err := splitModules([]string{"sale, stock", "crm,"})
	if err != nil || got != "sale,stock,crm" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := splitModules(nil); err == nil {
		t.Error("expected error for no modules")
	}
}

func TestSanitizeName(t *testing.T) {
	for in, want := range map[string]string{"My Shop": "my-shop", "odoo_17": "odoo_17", "!!!": "odoo"} {
		if got := sanitizeName(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
