// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package config

// What goes into the nix environment depends on env. Everything in this file
// is included only when env is local, so a prod environment never contains
// mailpit, debuggers or linters.

// DevPython is godoo's default python dev set (nix.dev_tools), local only.
// ipython also makes `odoo shell` use IPython automatically.
var DevPython = []string{
	"watchdog", // --dev=reload
	"debugpy",  // attach VS Code / any DAP debugger
	"ipython",  // better `odoo shell`
	"ipdb",     // breakpoint() with ipython
	"coverage", // `coverage run odoo-bin --test-enable ...`
}

// DevTools is godoo's default set of command-line tools, local only.
var DevTools = []string{
	"ruff",       // python linter/formatter
	"pre-commit", // runs OCA-style pre-commit hooks
	"pgcli",      // psql with autocompletion
}

// DevToolsOn reports whether godoo's default dev set is included.
func (c *Config) DevToolsOn() bool { return c.Local() && *c.Nix.DevTools }

// PythonPackagesForEnv lists the python packages for this env on top of
// odoo's own set: version extras, your packages, and (local only) the dev
// set and your local packages.
func (c *Config) PythonPackagesForEnv() (shared, local []string) {
	shared = append(append(shared, c.Profile.ExtraPython...), c.Nix.PythonPackages...)
	if c.Local() {
		if c.DevToolsOn() {
			local = append(local, DevPython...)
		} else {
			local = append(local, "watchdog") // --dev=reload still needs it
		}
		local = append(local, c.Nix.LocalPythonPackages...)
	}
	return shared, local
}

// PackagesForEnv lists the extra nixpkgs tools for this env.
func (c *Config) PackagesForEnv() (shared, local []string) {
	shared = append(shared, c.Nix.Packages...)
	if c.Local() {
		if c.DevToolsOn() {
			local = append(local, DevTools...)
		}
		local = append(local, c.Nix.LocalPackages...)
	}
	return shared, local
}

// PypiPackagesForEnv drops wheels marked local when env is prod.
func (c *Config) PypiPackagesForEnv() []PypiPackage {
	var out []PypiPackage
	for _, w := range c.Nix.PypiPackages {
		if !w.Local || c.Local() {
			out = append(out, w)
		}
	}
	return out
}
