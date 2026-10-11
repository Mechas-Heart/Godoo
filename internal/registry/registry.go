// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package registry remembers every project created on this machine so that
// `godoo list` can show them and `godoo init` can hand out free ports.
//
// Stored at $XDG_STATE_HOME/godoo/instances.json (~/.local/state/...).
package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func file() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "godoo", "instances.json"), nil
}

// All returns registered projects sorted by name. Missing file = empty list.
func All() ([]Entry, error) {
	f, err := file()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(f)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// Add registers (or renames) the project at path.
func Add(name, path string) error {
	entries, err := All()
	if err != nil {
		return err
	}
	entries = without(entries, path)
	return save(append(entries, Entry{Name: name, Path: path}))
}

// Remove forgets the project at path.
func Remove(path string) error {
	entries, err := All()
	if err != nil {
		return err
	}
	return save(without(entries, path))
}

func without(entries []Entry, path string) []Entry {
	out := entries[:0]
	for _, e := range entries {
		if e.Path != path {
			out = append(out, e)
		}
	}
	return out
}

func save(entries []Entry) error {
	f, err := file()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f, append(b, '\n'), 0o644)
}
