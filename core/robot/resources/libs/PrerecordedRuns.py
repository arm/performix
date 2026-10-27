# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Robot keywords for finding pre-recorded runs and checking their metadata."""

import sys
from pathlib import Path

from robot.api.deco import keyword, library

# Find the shared scripts even when Robot is started directly from another directory.
sys.path.append(str(Path(__file__).resolve().parents[3] / "scripts"))
# pylint: disable-next=wrong-import-position
from lib import robot_prerecorded_runs as prerecorded_runs  # noqa: E402


@library(auto_keywords=False)
class PrerecordedRuns:
    """Expose the pre-recorded run helpers used by Robot tests."""

    @keyword("Resolve Robot Prerecorded Run")
    def resolve_run(self, logical_id, target_os, target_arch):
        """Find a pre-recorded run for the test's target operating system and architecture."""
        return prerecorded_runs.resolve_robot_prerecorded_run(logical_id, target_os, target_arch)

    @keyword("Validate Robot Prerecorded Metadata")
    def validate_metadata(self, metadata_path, run_id, workload_command):
        """Check that the pre-recorded run's metadata matches the test settings."""
        prerecorded_runs.validate_robot_prerecorded_metadata(metadata_path, run_id, workload_command)
