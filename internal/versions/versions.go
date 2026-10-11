// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package versions knows what each Odoo series needs: which nixpkgs provides
// its Python packages, which PostgreSQL to use, and which CLI flags it has.
//
// To support a new Odoo release, add a row to the profiles table.
package versions

import (
	"fmt"
	"strconv"
	"strings"
)

// ServicesNixpkgs is the nixpkgs pin for postgres, nginx, mailpit, git,
// wkhtmltopdf and rtlcss.
const ServicesNixpkgs = "nixos-25.05"

// MinMajor is the oldest supported Odoo series: the oldest one nixpkgs has
// packaged, so a tested set of Python packages exists for it.
const MinMajor = 15

type Profile struct {
	Major  int
	Branch string // git branch, e.g. "17.0"

	// Python and every Python package come from nixpkgs: we take the
	// dependency list of nixpkgs' own Odoo package (OdooPackage, in
	// PythonNixpkgs), including the version fixes nixpkgs applies to it.
	PythonNixpkgs string
	OdooPackage   string
	// ExtraPython are nixpkgs python attributes this series needs on top of
	// OdooPackage's list (for series nixpkgs has not packaged yet).
	ExtraPython []string

	Postgres string // default PostgreSQL major
}

// Found with `godoo doctor`, which compares requirements.txt to the nix python.
var odoo18Additions = []string{"asn1crypto", "cbor2", "openpyxl"}

var profiles = []Profile{
	{Major: 15, PythonNixpkgs: "nixos-24.11", OdooPackage: "odoo15", Postgres: "14"},
	{Major: 16, PythonNixpkgs: "nixos-25.05", OdooPackage: "odoo16", Postgres: "15"},
	{Major: 17, PythonNixpkgs: "nixos-25.05", OdooPackage: "odoo17", Postgres: "16"},
	// nixpkgs' odoo 18 snapshot predates these additions to 18.0's requirements.txt
	{Major: 18, PythonNixpkgs: "nixos-25.05", OdooPackage: "odoo", ExtraPython: odoo18Additions, Postgres: "16"},
	// Not in nixpkgs yet: 18's packages + what 19's requirements.txt adds.
	{Major: 19, PythonNixpkgs: "nixos-25.05", OdooPackage: "odoo", ExtraPython: []string{"asn1crypto", "cbor2", "openpyxl", "python-magic"}, Postgres: "17"},
}

// Supported lists the series with a known profile, for help texts.
func Supported() []string {
	out := make([]string, len(profiles))
	for i, p := range profiles {
		out[i] = fmt.Sprintf("%d.0", p.Major)
	}
	return out
}

// ProfileFor accepts "17", "17.0" or "saas-17.2". Series newer than the
// table reuse the newest profile.
func ProfileFor(version string) (Profile, error) {
	branch, major, err := parse(version)
	if err != nil {
		return Profile{}, err
	}
	if major < MinMajor {
		return Profile{}, fmt.Errorf("odoo %s is not supported: nixpkgs never packaged it, so there is no tested set of python packages (oldest supported: %d.0)", version, MinMajor)
	}
	p := profiles[len(profiles)-1]
	for _, candidate := range profiles {
		if candidate.Major == major {
			p = candidate
		}
	}
	p.Major, p.Branch = major, branch
	return p, nil
}

func parse(v string) (branch string, major int, err error) {
	v = strings.TrimSpace(v)
	num := strings.TrimPrefix(v, "saas-")
	head, _, hasMinor := strings.Cut(num, ".")
	major, err = strconv.Atoi(head)
	if err != nil {
		return "", 0, fmt.Errorf("invalid odoo version %q (expected something like 17.0)", v)
	}
	if !hasMinor {
		return fmt.Sprintf("%d.0", major), major, nil
	}
	return v, major, nil
}

// WebsocketPath is the URL nginx must route to the gevent port when workers > 0.
func (p Profile) WebsocketPath() string {
	if p.Major >= 16 {
		return "/websocket"
	}
	return "/longpolling"
}

// GeventOption is the odoo.conf key for the websocket/longpolling port.
func (p Profile) GeventOption() string {
	if p.Major >= 16 {
		return "gevent_port"
	}
	return "longpolling_port"
}

// DemoFlags returns the flags that create a database with or without demo
// data. Odoo 19 flipped the default from "with demo" to "without demo".
func (p Profile) DemoFlags(demo bool) []string {
	switch {
	case p.Major >= 19 && demo:
		return []string{"--with-demo"}
	case p.Major < 19 && !demo:
		return []string{"--without-demo=all"}
	}
	return nil
}
