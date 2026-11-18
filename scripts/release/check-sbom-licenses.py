#!/usr/bin/env python3
# Copyright 2026 The Tanpopo Authors.
# Licensed under the Apache License, Version 2.0.

"""Fail closed on missing or forbidden dependency licenses in CycloneDX SBOMs."""

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
SPDX = json.loads((ROOT / "spdx-identifiers.json").read_text(encoding="utf-8"))
SPDX_LICENSES = set(SPDX["licenses"])
SPDX_EXCEPTIONS = set(SPDX["exceptions"])
FORBIDDEN = re.compile(
    r"(?i)\b(?:"
    r"(?:gnu\s+)?(?:affero\s+)?(?:lesser\s+)?general\s+public\s+license"
    r"|server[- ]side\s+public\s+license"
    r"|(?:a?gpl|lgpl|sspl)(?:[- ]?[0-9.]+)?"
    r")\b"
)
TOKEN = re.compile(r"\s*(\(|\)|AND\b|OR\b|WITH\b|[A-Za-z0-9.+-]+)", re.IGNORECASE)


def components(items):
    for component in items:
        yield component
        yield from components(component.get("components", []))


def license_values(component):
    values = []
    for entry in component.get("licenses", []):
        license_info = entry.get("license", {})
        expression = entry.get("expression")
        identifier = license_info.get("id")
        name = license_info.get("name")
        value = expression or identifier or name
        if isinstance(value, str) and value.strip():
            values.append(value.strip())
    return values


def is_forbidden_license(value):
    return FORBIDDEN.search(value) is not None


def valid_spdx_expression(value):
    tokens = []
    position = 0
    while position < len(value):
        match = TOKEN.match(value, position)
        if not match:
            return False
        token = match.group(1)
        tokens.append(token.upper() if token.upper() in {"AND", "OR", "WITH"} else token)
        position = match.end()
    if not tokens:
        return False

    def parse_primary(index):
        if index >= len(tokens):
            raise ValueError
        token = tokens[index]
        if token == "(":
            index = parse_or(index + 1)
            if index >= len(tokens) or tokens[index] != ")":
                raise ValueError
            return index + 1, False
        if token in {"AND", "OR", "WITH", ")"} or token not in SPDX_LICENSES:
            raise ValueError
        return index + 1, True

    def parse_with(index):
        index, is_license = parse_primary(index)
        if index < len(tokens) and tokens[index] == "WITH":
            if not is_license or index + 1 >= len(tokens) or tokens[index + 1] not in SPDX_EXCEPTIONS:
                raise ValueError
            index += 2
        return index

    def parse_and(index):
        index = parse_with(index)
        while index < len(tokens) and tokens[index] == "AND":
            index = parse_with(index + 1)
        return index

    def parse_or(index):
        index = parse_and(index)
        while index < len(tokens) and tokens[index] == "OR":
            index = parse_and(index + 1)
        return index

    try:
        return parse_or(0) == len(tokens)
    except ValueError:
        return False


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
            values = license_values(component)
            if not values:
                print(f"{path}: missing or unknown license for {name}", file=sys.stderr)
                failed = True
                continue
            for value in values:
                if is_forbidden_license(value):
                    print(f"{path}: forbidden copyleft license {value!r} on {name}", file=sys.stderr)
                    failed = True
                elif not valid_spdx_expression(value):
                    print(f"{path}: unrecognized SPDX license expression {value!r} on {name}", file=sys.stderr)
                    failed = True
        print(f"{path}: checked {len(entries)} components")
    return 1 if failed else 0


if __name__ == "__main__":
    if len(sys.argv) < 2:
        raise SystemExit("usage: check-sbom-licenses.py <cyclonedx-sbom.json>...")
    raise SystemExit(main(sys.argv[1:]))
