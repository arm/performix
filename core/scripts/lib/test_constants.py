# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Unit tests for :mod:`lib.constants`.

Run from ``core/scripts`` so the ``lib`` package is importable, e.g.::

    cd core/scripts
    python -m unittest lib.test_constants -v
"""

import unittest

from lib.constants import get_core_root


class GetCoreRootTests(unittest.TestCase):
    def test_resolves_to_the_directory_containing_scripts_lib(self):
        root = get_core_root()

        # ``get_core_root`` is ``core/scripts/lib/constants.py`` -> ``parents[2]``.
        # Asserting this file exists relative to the computed root validates the
        # ``parents[2]`` arithmetic without hard-coding the directory name.
        self.assertTrue(root.is_dir())
        self.assertTrue((root / "scripts" / "lib" / "constants.py").is_file())

    def test_returns_an_absolute_path(self):
        self.assertTrue(get_core_root().is_absolute())


if __name__ == "__main__":
    unittest.main()
