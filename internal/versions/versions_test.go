// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package versions

import (
	"reflect"
	"testing"
)

func TestProfileFor(t *testing.T) {
	tests := []struct {
		in           string
		branch, odoo string
		pin          string
	}{
		{"17", "17.0", "odoo17", "nixos-25.05"},
		{"17.0", "17.0", "odoo17", "nixos-25.05"},
		{"saas-17.2", "saas-17.2", "odoo17", "nixos-25.05"},
		{"15.0", "15.0", "odoo15", "nixos-24.11"},
		{"18.0", "18.0", "odoo", "nixos-25.05"},
		{"25.0", "25.0", "odoo", "nixos-25.05"}, // newer than the table: newest profile
	}
	for _, tt := range tests {
		p, err := ProfileFor(tt.in)
		if err != nil {
			t.Fatalf("%s: %v", tt.in, err)
		}
		if p.Branch != tt.branch || p.OdooPackage != tt.odoo || p.PythonNixpkgs != tt.pin {
			t.Errorf("%s: got %+v", tt.in, p)
		}
	}
}

func TestProfileForRejects(t *testing.T) {
	for _, in := range []string{"14.0", "abc", ""} {
		if _, err := ProfileFor(in); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestVersionSpecificFlags(t *testing.T) {
	p15, _ := ProfileFor("15")
	p19, _ := ProfileFor("19")
	if p15.WebsocketPath() != "/longpolling" || p15.GeventOption() != "longpolling_port" {
		t.Error("15.0 should use longpolling")
	}
	if p19.WebsocketPath() != "/websocket" || p19.GeventOption() != "gevent_port" {
		t.Error("19.0 should use websocket")
	}
	cases := []struct {
		p    Profile
		demo bool
		want []string
	}{
		{p15, false, []string{"--without-demo=all"}},
		{p15, true, nil},
		{p19, false, nil},
		{p19, true, []string{"--with-demo"}},
	}
	for _, c := range cases {
		if got := c.p.DemoFlags(c.demo); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s demo=%v: got %v want %v", c.p.Branch, c.demo, got, c.want)
		}
	}
}
