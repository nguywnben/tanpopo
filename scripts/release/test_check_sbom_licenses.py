# Copyright 2026 The Tanpopo Authors.
# Licensed under the Apache License, Version 2.0.

"""Regression tests for fail-closed CycloneDX license scanning."""

import importlib.util
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("check-sbom-licenses.py")
SPEC = importlib.util.spec_from_file_location("check_sbom_licenses", MODULE_PATH)
CHECKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHECKER)


class ForbiddenLicenseTests(unittest.TestCase):
    def test_rejects_spdx_short_names(self):
        for value in (
            "GPL-3.0-only",
            "AGPL-3.0-or-later",
            "LGPL-2.1-only",
            "LGPL-3.0-only",
            "SSPL-1.0",
        ):
            with self.subTest(value=value):
                self.assertTrue(CHECKER.is_forbidden_license(value))

    def test_rejects_long_form_license_names(self):
        for value in (
            "GNU General Public License v3.0",
            "GNU Affero General Public License version 3",
            "GNU Lesser General Public License v2.1",
            "Server Side Public License v1",
        ):
            with self.subTest(value=value):
                self.assertTrue(CHECKER.is_forbidden_license(value))

    def test_allows_permissive_names(self):
        for value in ("Apache-2.0", "MIT", "BSD-3-Clause"):
            with self.subTest(value=value):
                self.assertFalse(CHECKER.is_forbidden_license(value))
                self.assertTrue(CHECKER.valid_spdx_expression(value))

    def test_rejects_unrecognized_identifiers_and_malformed_expressions(self):
        for value in (
            "Made-Up-License-9.9",
            "MIT OR",
            "MIT WITH NotARealException",
            "proprietary",
        ):
            with self.subTest(value=value):
                self.assertFalse(CHECKER.valid_spdx_expression(value))


if __name__ == "__main__":
    unittest.main()
