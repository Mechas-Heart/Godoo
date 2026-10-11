// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package templates renders the files godoo generates (flake.nix, odoo.conf,
// nginx.conf, ...). Each template lives next to this file as a plain .tmpl so
// the generated output is easy to read and change.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed *.tmpl
var files embed.FS

var funcs = template.FuncMap{
	"join": strings.Join,
}

// Render executes the named template (e.g. "nginx.conf.tmpl") with data.
func Render(name string, data any) ([]byte, error) {
	t, err := template.New(name).Funcs(funcs).Option("missingkey=error").ParseFS(files, name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// WriteFile renders a template to path, creating parent directories.
// It reports whether the file content changed.
func WriteFile(name, path string, data any) (changed bool, err error) {
	out, err := Render(name, data)
	if err != nil {
		return false, err
	}
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, out) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, out, 0o644)
}
