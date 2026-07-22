# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Unit tests for :mod:`lib.goflags`.

Run from ``core/scripts`` so the ``lib`` package is importable, e.g.::

    cd core/scripts
    python -m unittest lib.test_goflags -v
"""

import unittest

from lib.goflags import (
    DUCKDB_ARROW_BUILD_TAG,
    GOFLAGS_TAGS,
    go_build_tag_flag,
    go_env,
    golangci_build_tags_flag,
)


class GoEnvTests(unittest.TestCase):
    def test_sets_goflags_and_preserves_base_keys(self):
        base = {"PATH": "/usr/bin", "FOO": "bar"}
        env = go_env(base)

        self.assertEqual(env["GOFLAGS"], "-tags=duckdb_arrow")
        self.assertEqual(env["PATH"], "/usr/bin")
        self.assertEqual(env["FOO"], "bar")

    def test_does_not_mutate_the_supplied_base(self):
        base = {"PATH": "/usr/bin"}
        go_env(base)

        self.assertNotIn("GOFLAGS", base)

    def test_overrides_a_preexisting_goflags_in_base(self):
        env = go_env({"GOFLAGS": "-tags=something_else"})

        self.assertEqual(env["GOFLAGS"], "-tags=duckdb_arrow")

    def test_defaults_to_process_environment(self):
        env = go_env()

        self.assertEqual(env["GOFLAGS"], "-tags=duckdb_arrow")


if __name__ == "__main__":
    unittest.main()
