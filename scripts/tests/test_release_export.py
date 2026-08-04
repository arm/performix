# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import importlib.util
import subprocess
import sys
from pathlib import Path

import pytest


SCRIPT_PATH = Path(__file__).resolve().parents[1] / "release" / "export-open-source.py"


def load_release_export_module():
    spec = importlib.util.spec_from_file_location("release_export", SCRIPT_PATH)
    assert spec is not None
    assert spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


release_export = load_release_export_module()


@pytest.fixture(autouse=True)
def clear_release_env(monkeypatch):
    for name in [
        "LOCAL",
        "LOCAL_INTERNAL",
        "MANIFEST_PATH",
        "PUBLIC_BRANCH",
        "RELEASE_WORKDIR",
        "COMMIT_MSG",
        "SNAPSHOT_NAME",
        "TAG",
    ]:
        monkeypatch.delenv(name, raising=False)


def run_git(repo: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", "-C", repo, *args],
        check=check,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def init_repo(repo: Path) -> None:
    repo.mkdir(parents=True)
    run_git(repo, "init")
    run_git(repo, "config", "user.name", "Test User")
    run_git(repo, "config", "user.email", "test@example.com")
    (repo / "file.txt").write_text("initial\n", encoding="utf-8")
    run_git(repo, "add", "file.txt")
    run_git(repo, "commit", "-m", "Initial commit")


def commit_change(repo: Path, text: str, message: str) -> None:
    (repo / "file.txt").write_text(text, encoding="utf-8")
    run_git(repo, "add", "file.txt")
    run_git(repo, "commit", "-m", message)


def head(repo: Path) -> str:
    return run_git(repo, "rev-parse", "HEAD").stdout.strip()


def subject(repo: Path) -> str:
    return run_git(repo, "log", "-1", "--format=%s").stdout.strip()


def message(repo: Path) -> str:
    return run_git(repo, "log", "-1", "--format=%B").stdout.rstrip("\n")


def author(repo: Path) -> str:
    return run_git(repo, "log", "-1", "--format=%an <%ae>").stdout.strip()


def assert_release_error(expected: str, action) -> None:
    with pytest.raises(release_export.ReleaseExportError, match=expected):
        action()


def test_prepare_rejects_local_with_tag(monkeypatch, tmp_path):
    monkeypatch.setenv("LOCAL", "true")
    monkeypatch.setenv("TAG", "v2026.3.0")
    assert_release_error(
        "TAG cannot be used with LOCAL=true; use COMMIT_MSG instead",
        lambda: release_export.prepare_release_snapshot(tmp_path),
    )


def test_prepare_rejects_local_without_commit_msg(monkeypatch, tmp_path):
    monkeypatch.setenv("LOCAL", "true")
    assert_release_error(
        "COMMIT_MSG is required when LOCAL=true",
        lambda: release_export.prepare_release_snapshot(tmp_path),
    )


def test_prepare_rejects_commit_msg_without_local(monkeypatch, tmp_path):
    monkeypatch.setenv("COMMIT_MSG", "Publish public fix")
    assert_release_error(
        "COMMIT_MSG is only valid with LOCAL=true",
        lambda: release_export.prepare_release_snapshot(tmp_path),
    )


def test_prepare_rejects_legacy_snapshot_name(monkeypatch, tmp_path):
    monkeypatch.setenv("SNAPSHOT_NAME", "public-fix")
    assert_release_error(
        "SNAPSHOT_NAME is no longer supported; use COMMIT_MSG instead",
        lambda: release_export.prepare_release_snapshot(tmp_path),
    )


def test_prepare_rejects_legacy_local_internal(monkeypatch, tmp_path):
    monkeypatch.setenv("LOCAL_INTERNAL", "true")
    assert_release_error(
        "LOCAL_INTERNAL is no longer supported; use LOCAL=true",
        lambda: release_export.prepare_release_snapshot(tmp_path),
    )


def test_push_rejects_missing_tag(monkeypatch, tmp_path):
    monkeypatch.setenv("RELEASE_WORKDIR", str(tmp_path))
    assert_release_error(
        "prepared release state not found",
        lambda: release_export.push_prepared_release_snapshot(tmp_path),
    )


def test_push_rejects_local_without_commit_msg(monkeypatch, tmp_path):
    monkeypatch.setenv("LOCAL", "true")
    monkeypatch.setenv("RELEASE_WORKDIR", str(tmp_path))
    assert_release_error(
        "COMMIT_MSG is required when LOCAL=true",
        lambda: release_export.push_prepared_release_snapshot(tmp_path),
    )


def test_push_rejects_legacy_snapshot_name(monkeypatch, tmp_path):
    monkeypatch.setenv("SNAPSHOT_NAME", "public-fix")
    assert_release_error(
        "SNAPSHOT_NAME is no longer supported; use COMMIT_MSG instead",
        lambda: release_export.push_prepared_release_snapshot(tmp_path),
    )


def test_push_rejects_legacy_local_internal(monkeypatch, tmp_path):
    monkeypatch.setenv("LOCAL_INTERNAL", "true")
    assert_release_error(
        "LOCAL_INTERNAL is no longer supported; use LOCAL=true",
        lambda: release_export.push_prepared_release_snapshot(tmp_path),
    )


def test_suggested_push_command_uses_local_commit_msg():
    identity = release_export.PublishIdentity("Publish public fix", local=True)
    assert release_export.suggested_push_command(identity, "performix/publish-public-fix") == (
        "task release:push LOCAL=true COMMIT_MSG='Publish public fix'"
    )


def test_default_public_branch_slugifies_local_commit_msg():
    assert release_export.default_public_branch("Fix images in README file") == "performix/fix-images-in-readme-file"
    assert release_export.default_public_branch("Publish: README/images!") == "performix/publish-readme-images"


def test_suggested_push_command_uses_tag_for_release():
    identity = release_export.PublishIdentity("v2026.3.0")
    assert release_export.suggested_push_command(identity, "performix/v2026.3.0") == (
        "task release:push TAG=v2026.3.0"
    )


def test_suggested_stateful_push_command_uses_only_release_workdir(monkeypatch):
    monkeypatch.setenv("RELEASE_WORKDIR", "/tmp/release")
    assert release_export.suggested_stateful_push_command() == "task release:push RELEASE_WORKDIR=/tmp/release"


def test_release_state_round_trips_identity_and_public_branch(tmp_path):
    identity = release_export.PublishIdentity("Publish public fix", local=True)

    release_export.write_release_state(tmp_path, identity, "public/fix")

    loaded_identity, public_branch = release_export.read_release_state(tmp_path)
    assert loaded_identity == identity
    assert public_branch == "public/fix"


def test_tagged_metadata_creates_tag_and_validates(tmp_path):
    repo = tmp_path / "public"
    init_repo(repo)
    previous_head = head(repo)
    commit_change(repo, "release\n", "Exported release")

    identity = release_export.PublishIdentity("v2026.3.0")
    short_head = release_export.update_public_commit_metadata(repo, identity, previous_head)

    assert short_head == head(repo)[:7]
    assert subject(repo) == "Update open-source repository for Arm Performix version v2026.3.0"
    assert author(repo) == "Test User <test@example.com>"
    assert run_git(repo, "rev-parse", "--verify", "refs/tags/v2026.3.0^{}").stdout.strip() == head(repo)
    assert release_export.validate_prepared_public_snapshot(repo, identity) == short_head


def test_local_metadata_does_not_create_tag_and_validates(tmp_path):
    repo = tmp_path / "public"
    init_repo(repo)
    previous_head = head(repo)
    commit_change(repo, "snapshot\n", "Exported snapshot")

    identity = release_export.PublishIdentity("Publish public fix", local=True)
    short_head = release_export.update_public_commit_metadata(repo, identity, previous_head)

    assert short_head == head(repo)[:7]
    assert subject(repo) == "Publish public fix"
    assert run_git(repo, "rev-parse", "--verify", "refs/tags/Publish public fix^{}", check=False).returncode != 0
    assert release_export.validate_prepared_public_snapshot(repo, identity) == short_head


def test_local_metadata_uses_full_commit_message(tmp_path):
    repo = tmp_path / "public"
    init_repo(repo)
    previous_head = head(repo)
    commit_change(repo, "snapshot\n", "Exported snapshot")

    commit_msg = "Publish public fix\n\nExpose the local snapshot without a release tag."
    identity = release_export.PublishIdentity(commit_msg, local=True)
    release_export.update_public_commit_metadata(repo, identity, previous_head)

    assert message(repo) == commit_msg
    assert release_export.validate_prepared_public_snapshot(repo, identity)


def test_tagged_validation_requires_tag(tmp_path):
    repo = tmp_path / "public"
    init_repo(repo)
    commit_change(repo, "release\n", "Update open-source repository for Arm Performix version v2026.3.0")

    identity = release_export.PublishIdentity("v2026.3.0")
    assert_release_error(
        "prepared public snapshot tag not found",
        lambda: release_export.validate_prepared_public_snapshot(repo, identity),
    )


def test_local_validation_rejects_wrong_subject(tmp_path):
    repo = tmp_path / "public"
    init_repo(repo)
    commit_change(repo, "snapshot\n", "Wrong subject")

    identity = release_export.PublishIdentity("Publish public fix", local=True)
    assert_release_error(
        "prepared public snapshot commit has unexpected message",
        lambda: release_export.validate_prepared_public_snapshot(repo, identity),
    )


def test_tagged_push_pushes_branch_and_tag(monkeypatch, tmp_path):
    calls = []
    monkeypatch.setattr(release_export, "run", lambda args, **kwargs: calls.append([str(arg) for arg in args]))

    release_export.push_public_snapshot(tmp_path, release_export.PublishIdentity("v2026.3.0"), "performix/v2026.3.0")

    assert calls == [
        ["git", "-C", str(tmp_path), "push", "origin", "HEAD:refs/heads/performix/v2026.3.0"],
        ["git", "-C", str(tmp_path), "push", "origin", "refs/tags/v2026.3.0"],
    ]


def test_local_push_pushes_branch_only(monkeypatch, tmp_path):
    calls = []
    monkeypatch.setattr(release_export, "run", lambda args, **kwargs: calls.append([str(arg) for arg in args]))

    release_export.push_public_snapshot(
        tmp_path,
        release_export.PublishIdentity("Publish public fix", local=True),
        "performix/public-fix",
    )

    assert calls == [
        ["git", "-C", str(tmp_path), "push", "origin", "HEAD:refs/heads/performix/public-fix"],
    ]


def test_push_uses_prepared_release_state_without_repeating_args(monkeypatch, tmp_path):
    release_workdir = tmp_path / "workdir"
    public_checkout = release_workdir / "public-performix"
    init_repo(public_checkout)
    commit_change(public_checkout, "snapshot\n", "Publish public fix")
    identity = release_export.PublishIdentity("Publish public fix", local=True)
    release_export.write_release_state(release_workdir, identity, "performix/public-fix")
    monkeypatch.setenv("RELEASE_WORKDIR", str(release_workdir))

    calls = []
    monkeypatch.setattr(
        release_export,
        "push_public_snapshot",
        lambda checkout, pushed_identity, branch: calls.append((checkout, pushed_identity, branch)),
    )

    release_export.push_prepared_release_snapshot(tmp_path)

    assert calls == [(public_checkout, identity, "performix/public-fix")]


def test_push_public_branch_overrides_prepared_release_state(monkeypatch, tmp_path):
    release_workdir = tmp_path / "workdir"
    public_checkout = release_workdir / "public-performix"
    init_repo(public_checkout)
    commit_change(public_checkout, "snapshot\n", "Publish public fix")
    identity = release_export.PublishIdentity("Publish public fix", local=True)
    release_export.write_release_state(release_workdir, identity, "performix/public-fix")
    monkeypatch.setenv("RELEASE_WORKDIR", str(release_workdir))
    monkeypatch.setenv("PUBLIC_BRANCH", "performix/override")

    calls = []
    monkeypatch.setattr(
        release_export,
        "push_public_snapshot",
        lambda checkout, pushed_identity, branch: calls.append((checkout, pushed_identity, branch)),
    )

    release_export.push_prepared_release_snapshot(tmp_path)

    assert calls == [(public_checkout, identity, "performix/override")]
