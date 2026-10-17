// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package pypi finds pure-python wheels on PyPI for packages that nixpkgs
// does not have. godoo never runs pip: it records the wheel's URL and hash
// in godoo.yaml and nix builds it into the project's python, pinned and
// reproducible like everything else.
package pypi

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Release is one wheel, everything the flake needs to fetch and install it.
type Release struct {
	Name         string   // canonical PyPI name, e.g. "OdooRPC"
	Version      string   // e.g. "0.10.1"
	URL          string   // the .whl file
	Hash         string   // SRI hash, "sha256-..."
	RequiresDist []string // raw requirements, e.g. `importlib_metadata; python_version < "3.8"`
}

var client = &http.Client{Timeout: 30 * time.Second}

// Latest returns the newest release of name that ships a pure-python wheel
// (py3-none-any). Packages with only sdists or compiled wheels are refused:
// they need a hand-written nix derivation.
func Latest(ctx context.Context, name string) (Release, error) { return Get(ctx, name, "") }

// Get returns release version of name ("" = the newest), with the same
// pure-wheel rule as Latest.
func Get(ctx context.Context, name, version string) (Release, error) {
	var data struct {
		Info struct {
			Name         string   `json:"name"`
			Version      string   `json:"version"`
			RequiresDist []string `json:"requires_dist"`
		} `json:"info"`
		URLs []struct {
			Filename    string            `json:"filename"`
			PackageType string            `json:"packagetype"`
			URL         string            `json:"url"`
			Digests     map[string]string `json:"digests"`
			Yanked      bool              `json:"yanked"`
		} `json:"urls"`
	}
	endpoint := "https://pypi.org/pypi/" + url.PathEscape(name) + "/json"
	if version != "" {
		endpoint = "https://pypi.org/pypi/" + url.PathEscape(name) + "/" + url.PathEscape(version) + "/json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("pypi: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		if version != "" {
			return Release{}, fmt.Errorf("version %s of %s is not on PyPI", version, name)
		}
		return Release{}, fmt.Errorf("not on PyPI either")
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("pypi: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Release{}, fmt.Errorf("pypi: %w", err)
	}

	for _, f := range data.URLs {
		if f.PackageType != "bdist_wheel" || f.Yanked || !isPureWheel(f.Filename) {
			continue
		}
		hash, err := sri(f.Digests["sha256"])
		if err != nil {
			return Release{}, fmt.Errorf("pypi %s: %w", f.Filename, err)
		}
		return Release{
			Name:         data.Info.Name,
			Version:      data.Info.Version,
			URL:          f.URL,
			Hash:         hash,
			RequiresDist: data.Info.RequiresDist,
		}, nil
	}
	return Release{}, fmt.Errorf("%s %s has no pure-python wheel on PyPI (only sdist or compiled wheels); it needs a nix derivation", name, data.Info.Version)
}

// isPureWheel matches "*-py3-none-any.whl" and "*-py2.py3-none-any.whl".
func isPureWheel(filename string) bool {
	return strings.HasSuffix(filename, "-py3-none-any.whl") || strings.HasSuffix(filename, ".py3-none-any.whl")
}

// sri converts PyPI's hex sha256 into the "sha256-<base64>" form nix uses.
func sri(hexDigest string) (string, error) {
	raw, err := hex.DecodeString(hexDigest)
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("bad sha256 digest %q", hexDigest)
	}
	return "sha256-" + base64.StdEncoding.EncodeToString(raw), nil
}

var (
	reqNameRe      = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._-]*)`)
	pythonMarkerRe = regexp.MustCompile(`^python_(?:full_)?version\s*(<=|>=|==|!=|<|>)\s*["']([0-9.]+)["']$`)
)

// Dependencies returns the package names from requires_dist that apply to a
// Linux/macOS install on the given python version (e.g. "3.12"). Optional
// extras are skipped. Markers it cannot evaluate are kept, to be safe.
func Dependencies(requiresDist []string, pythonVersion string) []string {
	var out []string
	for _, req := range requiresDist {
		spec, marker, _ := strings.Cut(req, ";")
		m := reqNameRe.FindStringSubmatch(spec)
		if m == nil || !markerApplies(strings.TrimSpace(marker), pythonVersion) {
			continue
		}
		out = append(out, m[1])
	}
	return out
}

func markerApplies(marker, pythonVersion string) bool {
	if marker == "" {
		return true
	}
	lower := strings.ToLower(marker)
	switch {
	case strings.Contains(lower, "extra"):
		return false // optional feature
	case strings.Contains(lower, "win32"), strings.Contains(lower, "windows"), strings.Contains(lower, `"nt"`):
		return false
	}
	if m := pythonMarkerRe.FindStringSubmatch(strings.TrimSpace(marker)); m != nil {
		cmp := compareVersions(pythonVersion, m[2])
		switch m[1] {
		case "<":
			return cmp < 0
		case "<=":
			return cmp <= 0
		case ">":
			return cmp > 0
		case ">=":
			return cmp >= 0
		case "==":
			return cmp == 0
		case "!=":
			return cmp != 0
		}
	}
	return true
}

// compareVersions compares dotted versions on the components both have:
// "3.12.4" vs "3.8" compares 3.12 with 3.8.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, _ := strconv.Atoi(as[i])
		y, _ := strconv.Atoi(bs[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
