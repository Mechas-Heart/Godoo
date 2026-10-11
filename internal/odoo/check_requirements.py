# Copyright 2026 Mechas Heart
# SPDX-License-Identifier: Apache-2.0
"""Run by `godoo doctor`: is every package from odoo/requirements.txt present
in the nix python?

Only presence is checked. Versions come from nixpkgs (the set nixpkgs uses
for its own Odoo package), not from the exact pins in requirements.txt, so
"different version" is expected and only shown with --versions.

Usage: python check_requirements.py REQUIREMENTS.TXT [--versions]
Exit code 1 when something is missing.
"""
import re
import sys
from importlib import metadata

try:
    from packaging.requirements import Requirement
except ImportError:  # fall back to a simple parser
    Requirement = None


def requirements(path):
    """Yield (name, version spec) for lines that apply to this platform/python."""
    seen = set()
    with open(path) as f:
        for line in f:
            line = line.split("#", 1)[0].strip()
            if not line:
                continue
            if Requirement is not None:
                req = Requirement(line)
                if req.marker is not None and not req.marker.evaluate():
                    continue
                name, spec = req.name, str(req.specifier)
            else:
                text, _, marker = line.partition(";")
                if "win32" in marker:
                    continue
                name, spec = re.match(r"\s*([A-Za-z0-9_.\-]+)\S*\s*(.*)", text).groups()
            if name.lower() not in seen:
                seen.add(name.lower())
                yield name, spec.strip()


def main():
    path, show_versions = sys.argv[1], "--versions" in sys.argv
    missing = 0
    total = 0
    for name, spec in requirements(path):
        total += 1
        try:
            have = metadata.version(name)
        except metadata.PackageNotFoundError:
            missing += 1
            print("  MISSING  %-22s wanted %s" % (name, spec or "any"))
            continue
        if show_versions:
            print("  ok       %-22s %-12s (requirements.txt: %s)" % (name, have, spec or "any"))
    print("  %d requirements checked, %d missing" % (total, missing))
    return 1 if missing else 0


if __name__ == "__main__":
    sys.exit(main())
