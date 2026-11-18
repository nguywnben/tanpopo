#!/usr/bin/env python3
# Copyright 2026 The Tanpopo Authors.
# Licensed under the Apache License, Version 2.0.

"""Fail closed on missing or forbidden dependency licenses in CycloneDX SBOMs."""

import json
import re
import sys
from pathlib import Path

FORBIDDEN = re.compile(r"(?i)\b(?:A?GPL|SSPL)(?:[- ]?[0-9.]+)?\b")
UNKNOWN = {"unknown", "noassertion", "none", "unlicensed"}


def components(items):
    for component in items:
        yield component
        yield from components(component.get("components", []))


def license_text(component):
    values = []
    for entry in component.get("licenses", []):
        license_info = entry.get("license", {})
        values.extend((license_info.get("id", ""), license_info.get("name", ""), entry.get("expression", "")))
    return " ".join(value.strip() for value in values if isinstance(value, str) and value.strip())


def main(paths):
    failed = False
    for path in paths:
        bom = json.loads(Path(path).read_text(encoding="utf-8"))
        entries = list(components(bom.get("components", [])))
        if not entries:
            print(f"{path}: SBOM has no dependency components", file=sys.stderr)
            failed = True
            continue
        for component in entries:
            name = component.get("name", "<unnamed>")
            value = license_text(component)
            if not value or any(token in UNKNOWN for token in re.split(r"[^A-Za-z0-9.-]+", value.lower())):
                print(f"{path}: missing or unknown license for {name}", file=sys.stderr)
                failed = True
            elif FORBIDDEN.search(value):
                print(f"{path}: forbidden copyleft license {value!r} on {name}", file=sys.stderr)
                failed = True
        print(f"{path}: checked {len(entries)} components")
    return 1 if failed else 0


if __name__ == "__main__":
    if len(sys.argv) < 2:
        raise SystemExit("usage: check-sbom-licenses.py <cyclonedx-sbom.json>...")
    raise SystemExit(main(sys.argv[1:]))
