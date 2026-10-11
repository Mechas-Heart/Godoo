// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Edit changes godoo.yaml in place through yaml.v3's node tree, so comments
// and key order survive. The result is validated with Load; if it is
// invalid, the original file is put back and the error returned.
func Edit(root string, change func(doc *yaml.Node) error) error {
	file := filepath.Join(root, FileName)
	original, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(original, &doc); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: expected a mapping at the top level", FileName)
	}
	if err := change(doc.Content[0]); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if _, err := Load(root); err != nil {
		_ = os.WriteFile(file, original, 0o644)
		return fmt.Errorf("change rejected, %s left unchanged: %w", FileName, err)
	}
	return nil
}

// MapChild returns the value node for key in a mapping, creating an empty
// node of the given kind when the key is missing (or null).
func MapChild(m *yaml.Node, key string, kind yaml.Kind) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v := m.Content[i+1]
			if v.Tag == "!!null" || (v.Kind == yaml.ScalarNode && v.Value == "") {
				*v = yaml.Node{Kind: kind}
			}
			return v
		}
	}
	v := &yaml.Node{Kind: kind}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, v)
	return v
}

// Scalar makes a plain string node; quoted forces quotes (for "17.0").
func Scalar(value string, quoted bool) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	if quoted {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// SeqStrings returns the string values of a sequence node.
func SeqStrings(seq *yaml.Node) []string {
	out := make([]string, 0, len(seq.Content))
	for _, n := range seq.Content {
		out = append(out, n.Value)
	}
	return out
}

// SeqAdd appends values not yet in the sequence; it returns the added ones.
func SeqAdd(seq *yaml.Node, values ...string) (added []string) {
	have := map[string]bool{}
	for _, v := range SeqStrings(seq) {
		have[v] = true
	}
	for _, v := range values {
		if !have[v] {
			seq.Content = append(seq.Content, Scalar(v, false))
			have[v] = true
			added = append(added, v)
		}
	}
	return added
}

// SeqRemove deletes values from the sequence; it returns the removed ones.
func SeqRemove(seq *yaml.Node, values ...string) (removed []string) {
	drop := map[string]bool{}
	for _, v := range values {
		drop[v] = true
	}
	kept := seq.Content[:0]
	for _, n := range seq.Content {
		if drop[n.Value] {
			removed = append(removed, n.Value)
			continue
		}
		kept = append(kept, n)
	}
	seq.Content = kept
	return removed
}
