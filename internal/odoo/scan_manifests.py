# Copyright 2026 Mechas Heart
# SPDX-License-Identifier: Apache-2.0
"""Run by godoo: which python packages do these addons need that the nix
python does not have?

Reads `external_dependencies["python"]` from every module manifest in the
given directories (installable modules only).

Usage:  python scan_manifests.py DIR [DIR...]
Prints: {"package": ["module", ...], ...} as JSON, missing packages only.
"""
import ast
import importlib.util
import json
import os
import re
import sys
from importlib import metadata


def manifests(dirs):
    for d in dirs:
        if not os.path.isdir(d):
            continue
        for module in sorted(os.listdir(d)):
            for filename in ("__manifest__.py", "__openerp__.py"):
                path = os.path.join(d, module, filename)
                if os.path.isfile(path):
                    yield module, path
                    break


def installed(dep):
    """A dependency may be a distribution name or an import name."""
    try:
        metadata.distribution(dep)
        return True
    except metadata.PackageNotFoundError:
        pass
    try:
        return importlib.util.find_spec(dep) is not None
    except (ImportError, ValueError):
        return False


def main():
    missing = {}
    for module, path in manifests(sys.argv[1:]):
        try:
            with open(path) as f:
                manifest = ast.literal_eval(f.read())
        except (ValueError, SyntaxError):
            continue
        if not manifest.get("installable", True):
            continue
        for dep in manifest.get("external_dependencies", {}).get("python", []):
            name = re.split(r"[<>=!~;\[ ]", dep, maxsplit=1)[0]  # drop version specs
            if name and not installed(name):
                missing.setdefault(name, []).append(module)
    json.dump(missing, sys.stdout)


if __name__ == "__main__":
    main()
