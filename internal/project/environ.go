// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Environ is the environment every godoo-started process gets: the nix env
// first on PATH, plus libpq variables so psql, pg_dump and
// friends connect to this project's postgres without extra flags.
//
// PGDATABASE is deliberately not set (and dropped from your shell): Odoo 19+
// reads it as the default db_name and would then serve only that database.
//
// PYTHONPATH, PYTHONHOME, VIRTUAL_ENV and LD_LIBRARY_PATH from your shell are
// dropped: they would make the nix python load packages or libraries from
// outside the environment.
func (p *Project) Environ() []string {
	c := p.Cfg
	vars := map[string]string{
		"PATH":             filepath.Join(p.EnvDir(), "bin") + ":" + os.Getenv("PATH"),
		"PGHOST":           "127.0.0.1",
		"PGPORT":           fmt.Sprint(c.Ports.Postgres),
		"PGUSER":           c.Postgres.User,
		"PGPASSWORD":       c.Postgres.Password,
		"ODOO_RC":          p.ConfFile("odoo.conf"),
		"GODOO_PROJECT":    c.Name,
		"GODOO_ROOT":       p.Root,
		"PYTHONNOUSERSITE": "1",
	}

	dropped := map[string]bool{"PYTHONPATH": true, "PYTHONHOME": true, "VIRTUAL_ENV": true, "LD_LIBRARY_PATH": true, "PGDATABASE": true}
	out := make([]string, 0, len(os.Environ())+len(vars))
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, override := vars[k]; !override && !dropped[k] {
			out = append(out, kv)
		}
	}
	for k, v := range vars {
		out = append(out, k+"="+v)
	}
	return out
}
