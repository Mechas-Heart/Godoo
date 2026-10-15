// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package selection

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Editing addons.yaml from `godoo addons add/remove`, keeping comments.
// Entries go into the first document without ONLY or ENV: a document with
// ONLY applies only in some envs, and ENV changes where repos download from.

// header starts an addons.yaml created by `godoo addons add --modules`.
const header = `# Modules to activate from the addon repos (doodba format). Only these are
# symlinked into .godoo/addons/ and downloaded; "*" means all of a repo's
# modules. See the "Selecting modules: addons.yaml" section of the godoo README.
`

// Change says what Select did.
type Change int

const (
	Unchanged Change = iota // the entry already selects these modules
	Added                   // new entry
	Extended                // modules added to an existing entry
	Created                 // addons.yaml created
)

// Select makes addons.yaml select modules of repo: ["*"] for all of them, no
// modules for an empty entry (`repo: []`: registered, nothing downloaded).
// When there is no addons.yaml yet, it creates one in which every repo of
// keepWhole is selected whole, so nothing that worked before turns off.
// undo restores the file as it was.
func Select(root, repo string, modules, keepWhole []string) (change Change, undo func(), err error) {
	file := filepath.Join(root, FileName)
	original, readErr := os.ReadFile(file)
	switch {
	case errors.Is(readErr, os.ErrNotExist):
		undo = func() { os.Remove(file) }
	case readErr != nil:
		return Unchanged, nil, readErr
	default:
		undo = func() { os.WriteFile(file, original, 0o644) }
	}

	var docs []*yaml.Node
	if readErr == nil {
		if docs, err = decodeAll(original); err != nil {
			return Unchanged, nil, err
		}
	} else {
		change = Created
	}
	target := plainDocument(&docs)
	if change == Created {
		target.HeadComment = strings.TrimSpace(header)
		for _, name := range keepWhole {
			if name != repo {
				setEntry(target, name, []string{"*"})
			}
		}
	}

	c := setEntry(target, repo, modules)
	if change != Created {
		change = c
	}
	if change == Unchanged {
		return Unchanged, undo, nil
	}
	if err := writeAll(file, docs); err != nil {
		return Unchanged, nil, err
	}
	if _, err := Load(root, ""); err != nil {
		undo()
		return Unchanged, nil, fmt.Errorf("change rejected, %s left unchanged: %w", FileName, err)
	}
	return change, undo, nil
}

// Deselect removes repo's entries from every document of addons.yaml. It
// reports whether there was one.
func Deselect(root, repo string) (bool, error) {
	file := filepath.Join(root, FileName)
	original, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	docs, err := decodeAll(original)
	if err != nil {
		return false, err
	}
	removed := false
	for _, d := range docs {
		m := d.Content[0]
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == repo {
				m.Content = slices.Delete(m.Content, i, i+2)
				removed = true
				break
			}
		}
	}
	if !removed {
		return false, nil
	}
	return true, writeAll(file, docs)
}

// setEntry adds modules to repo's entry in mapping document d.
func setEntry(d *yaml.Node, repo string, modules []string) Change {
	m := d.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != repo {
			continue
		}
		seq := m.Content[i+1]
		if seq.Kind != yaml.SequenceNode {
			*seq = yaml.Node{Kind: yaml.SequenceNode}
		}
		current := make([]string, 0, len(seq.Content))
		for _, n := range seq.Content {
			current = append(current, n.Value)
		}
		if slices.Contains(current, "*") {
			return Unchanged // already all of it
		}
		if slices.Contains(modules, "*") {
			seq.Content = []*yaml.Node{pattern("*")} // all replaces a list
			return Extended
		}
		change := Unchanged
		for _, mod := range modules {
			if !slices.Contains(current, mod) {
				seq.Content = append(seq.Content, pattern(mod))
				seq.Style = 0 // `repo: []` becomes a block list
				change = Extended
			}
		}
		return change
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	if len(modules) == 0 {
		seq.Style = yaml.FlowStyle // `repo: []`
	}
	for _, mod := range modules {
		seq.Content = append(seq.Content, pattern(mod))
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: repo}, seq)
	return Added
}

// pattern is a module name or wildcard; wildcards need quotes in YAML.
func pattern(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: s}
	if strings.ContainsAny(s, "*?[") {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// plainDocument returns the first document without ONLY or ENV, adding an
// empty one at the end when there is none.
func plainDocument(docs *[]*yaml.Node) *yaml.Node {
	for _, d := range *docs {
		m := d.Content[0]
		special := false
		for i := 0; i+1 < len(m.Content); i += 2 {
			if k := m.Content[i].Value; k == "ONLY" || k == "ENV" {
				special = true
			}
		}
		if !special {
			return d
		}
	}
	d := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	*docs = append(*docs, d)
	return d
}

// decodeAll reads every document of addons.yaml as nodes. Empty documents
// are dropped; every kept one is a mapping.
func decodeAll(content []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	var docs []*yaml.Node
	for {
		var d yaml.Node
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", FileName, err)
		}
		if len(d.Content) == 0 {
			continue
		}
		if d.Content[0].Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s: expected `repo: [modules]` entries", FileName)
		}
		docs = append(docs, &d)
	}
}

func writeAll(file string, docs []*yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return err
		}
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(file, buf.Bytes(), 0o644)
}
