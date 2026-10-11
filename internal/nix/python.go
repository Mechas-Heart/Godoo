// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

// PythonPackage is a nixpkgs python attribute found for a requested name.
type PythonPackage struct {
	Requested string // what the user typed, e.g. "PyYAML"
	Attr      string // nixpkgs attribute, e.g. "pyyaml"
	Version   string
}

var packageNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// System is the nix system string of this machine, e.g. "x86_64-linux".
func System() string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	return arch + "-" + runtime.GOOS
}

// candidates lists the nixpkgs spellings to try for a pip-style name, in order:
// "PyYAML" -> PyYAML, pyyaml; "python_dateutil" -> ..., python-dateutil.
func candidates(name string) []string {
	lower := strings.ToLower(name)
	dashed := strings.NewReplacer("_", "-", ".", "-").Replace(lower)
	list := []string{name, lower, dashed}
	if rest, ok := strings.CutPrefix(dashed, "python-"); ok {
		list = append(list, rest)
	} else {
		list = append(list, "python-"+dashed)
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range list {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// Lookup is the answer to "which of these names does nixpkgs have?" for the
// project's pinned python package set.
type Lookup struct {
	Found         []PythonPackage
	Missing       []string // not in nixpkgs under any spelling
	PythonVersion string   // e.g. "3.12.12", for evaluating dependency markers
	base          map[string]bool
}

// InBase reports whether name is already part of Odoo's package set.
func (l Lookup) InBase(name string) bool {
	for _, c := range candidates(name) {
		if l.base[c] {
			return true
		}
	}
	return false
}

// LookupPython checks names against the project's pinned python package set
// (the flake's legacyPackages output) in a single nix evaluation.
func LookupPython(ctx context.Context, p *project.Project, names []string) (Lookup, error) {
	var all []string
	for _, n := range names {
		if !packageNameRe.MatchString(n) {
			return Lookup{}, fmt.Errorf("invalid package name %q", n)
		}
		all = append(all, candidates(n)...)
	}
	quoted := make([]string, len(all))
	for i, c := range all {
		quoted[i] = strconv.Quote(c)
	}
	// versions: candidate -> version, or null when the attribute does not
	// exist (or is a removed alias that throws).
	apply := `lp: {
	  python = lp.pythonPackages.python.version;
	  base = lp.basePythonPackages;
	  versions = builtins.listToAttrs (map (n: {
	    name = n;
	    value = let r = builtins.tryEval (if lp.pythonPackages ? ${n} then (lp.pythonPackages.${n}.version or "") else null);
	            in if r.success then r.value else null;
	  }) [ ` + strings.Join(quoted, " ") + ` ]);
	}`
	installable := fmt.Sprintf("%s#legacyPackages.%s", flakeRef(p), System())
	args := append(append([]string{}, baseArgs...), "eval", "--json", installable, "--apply", apply)
	ui.Debug("%s %s", bin(), strings.Join(args, " "))
	out, err := exec.CommandContext(ctx, bin(), args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return Lookup{}, fmt.Errorf("nix eval failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return Lookup{}, err
	}
	var result struct {
		Python   string             `json:"python"`
		Base     []string           `json:"base"`
		Versions map[string]*string `json:"versions"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return Lookup{}, err
	}

	l := Lookup{PythonVersion: result.Python, base: map[string]bool{}}
	for _, b := range result.Base {
		l.base[strings.ToLower(b)] = true
	}
	for _, n := range names {
		ok := false
		for _, c := range candidates(n) {
			if v := result.Versions[c]; v != nil {
				l.Found = append(l.Found, PythonPackage{Requested: n, Attr: c, Version: *v})
				ok = true
				break
			}
		}
		if !ok {
			l.Missing = append(l.Missing, n)
		}
	}
	return l, nil
}
