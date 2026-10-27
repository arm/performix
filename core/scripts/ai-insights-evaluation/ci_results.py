# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Stage AI Insights evaluation evidence for CI uploads.

Select evidence for review, excluding temporary agent homes, source workspaces
and connection state, including leftovers from interrupted attempts. The workflow
uploads the staged directory to GitHub and builds the Artifactory archive from
it; the original results directory is left unchanged.

This is an evidence-retention policy, not content sanitization. Retained
transcripts and agent responses can contain anything the agent observed. Credential
isolation and sandboxing govern access during execution; CI separately requires
confirmed Pathways target termination before publishing evidence.
"""

import argparse
import json
import shutil
from pathlib import Path

from create_ai_insights_results_archive import prerecorded_metadata_entries
from pathways_artifacts import (
    is_pathways_artifact,
    pathways_attempt_directory_index,
    pathways_test_ids,
    result_path_contains_symlink,
)


EXCLUDED_RESULT_DIRECTORIES = frozenset(
    {"imported_sources", "codex_home", "codex_workspace", "performix_state", "agent_home"}
)


def should_stage_result(source, results, pathways_ids):
    """Return whether ``source`` is retained as result evidence for upload.

    Non-Pathways result files are retained, except for known directories that
    hold local source copies or agent state. For a Pathways test, only the
    evidence accepted by ``is_pathways_artifact`` directly inside its attempt directory
    are retained, excluding temporary Pathways state even if an execution
    stopped before its normal artifact-capture step.
    """
    relative = source.relative_to(results)
    if not source.is_file() or result_path_contains_symlink(results, relative):
        return False
    if any(part in EXCLUDED_RESULT_DIRECTORIES for part in relative.parts):
        return False

    attempt_index = pathways_attempt_directory_index(relative, pathways_ids)
    if attempt_index is None:
        return True

    is_direct_attempt_file = len(relative.parts) == attempt_index + 3
    return is_direct_attempt_file and is_pathways_artifact(source.name)


def stage_results(results, destination, manifest):
    """Copy retained evidence to ``destination/results``, preserving its layout."""
    # The manifest identifies Pathways tests and selects their evidence allowlist;
    # both workflows use this helper without a separate workflow-specific mode.
    pathways_ids = pathways_test_ids(manifest)
    for source in results.rglob("*"):
        relative = source.relative_to(results)
        if not should_stage_result(source, results, pathways_ids):
            continue
        target = destination / "results" / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)


def stage_prerecorded_metadata(run_artifact_base, destination):
    """Stage input-run metadata alongside results, without copying input runs."""
    for source, relative in prerecorded_metadata_entries(run_artifact_base):
        if result_path_contains_symlink(run_artifact_base, source.relative_to(run_artifact_base)):
            continue
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)


def main():
    """Parse CI paths and create a new directory of retained artifacts."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--results-dir", required=True, type=Path)
    parser.add_argument("--destination", required=True, type=Path)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--run-artifact-base", required=True, type=Path)
    args = parser.parse_args()
    args.destination.mkdir(parents=True, exist_ok=False)
    stage_results(args.results_dir, args.destination, json.loads(args.manifest.read_text()))
    stage_prerecorded_metadata(args.run_artifact_base, args.destination)


if __name__ == "__main__":
    main()
