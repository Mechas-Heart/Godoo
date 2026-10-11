// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
)

// exportedVars are the variables `godoo env` prints: what you need to use
// the project's python, odoo-bin, psql etc. from your own shell.
var exportedVars = map[string]bool{
	"PATH": true, "VIRTUAL_ENV": true, "ODOO_RC": true,
	"PGHOST": true, "PGPORT": true, "PGUSER": true, "PGPASSWORD": true,
	"GODOO_PROJECT": true, "GODOO_ROOT": true,
}

func cmdEnv(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("env"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	var lines []string
	for _, kv := range p.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if exportedVars[k] {
			lines = append(lines, fmt.Sprintf("export %s=%s", k, shellQuote(v)))
		}
	}
	sort.Strings(lines)
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

func cmdSh(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("sh"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	fmt.Printf("godoo shell for %s (Odoo %s) - exit to leave\n", p.Cfg.Name, p.Cfg.Odoo.Version)
	return syscall.Exec(shell, []string{shell}, p.Environ())
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
