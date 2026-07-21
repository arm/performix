#!/usr/bin/env python3
#
# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Create a public source snapshot from the internal Performix repository."""

from __future__ import annotations

import json
import os
import re
import shlex
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


# We currently use ossmosis for preparing the internal repo snapshot to export to the public repo.
OSSMOSIS_REPO_URL = os.environ.get(
    "OSSMOSIS_REPO_URL",
    "https://github.com/Arm-Debug/ossmosis",
)
INTERNAL_REPO_URL = os.environ.get(
    "INTERNAL_REPO_URL",
    "https://github.com/Arm-Debug/performix",
)
PUBLIC_REPO_URL = os.environ.get(
    "PUBLIC_REPO_URL",
    "https://github.com/arm/performix",
)
MAX_CLASS = "public"
RELEASE_TAG_RE = re.compile(r"^v?[0-9]+(\.[0-9]+){1,2}([.-].*)?$")
RELEASE_STATE_FILENAME = "release-state.json"


class ReleaseExportError(Exception):
    """Raised when the open-source export cannot continue."""


@dataclass(frozen=True)
class PublishIdentity:
    """Identifies the public snapshot being prepared or pushed."""

    name: str
    local: bool = False

    @property
    def creates_tag(self) -> bool:
        return not self.local


def env_value(name: str, default: str = "") -> str:
    return os.environ.get(name) or default


def log(message: str = "") -> None:
    print(message)


def run(
    args: list[str | Path],
    *,
    cwd: Path | None = None,
    stdout=None,
    check: bool = True,
) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(
        [str(arg) for arg in args],
        cwd=cwd,
        text=True,
        stdout=stdout,
        check=False,
    )
    if check and result.returncode != 0:
        command = " ".join(str(arg) for arg in args)
        location = f" in {cwd}" if cwd else ""
        raise ReleaseExportError(f"command failed{location}: {command}")
    return result


def run_output(args: list[str | Path], *, cwd: Path | None = None, check: bool = True) -> str:
    result = run(args, cwd=cwd, stdout=subprocess.PIPE, check=check)
    return result.stdout.strip()


def require_command(command: str) -> None:
    if shutil.which(command) is None:
        raise ReleaseExportError(f"required command not found: {command}")


def require_no_legacy_local_internal() -> None:
    if "LOCAL_INTERNAL" in os.environ:
        raise ReleaseExportError("LOCAL_INTERNAL is no longer supported; use LOCAL=true instead")


def require_no_legacy_snapshot_name() -> None:
    if "SNAPSHOT_NAME" in os.environ:
        raise ReleaseExportError("SNAPSHOT_NAME is no longer supported; use COMMIT_MSG instead")


def env_bool(name: str, default: str = "false") -> bool:
    value = env_value(name, default)
    if value not in {"true", "false"}:
        raise ReleaseExportError(f"{name} must be either true or false")
    return value == "true"


def git_default_branch(checkout: Path) -> str:
    run(
        ["git", "-C", checkout, "remote", "set-head", "origin", "-a"],
        stdout=subprocess.DEVNULL,
        check=False,
    )
    branch = run_output(
        [
            "git",
            "-C",
            checkout,
            "symbolic-ref",
            "--quiet",
            "--short",
            "refs/remotes/origin/HEAD",
        ],
        check=False,
    )
    return branch.removeprefix("origin/")


def checkout_default_branch(checkout: Path) -> None:
    branch = git_default_branch(checkout)
    if not branch:
        raise ReleaseExportError(f"could not determine default branch for {checkout}")
    run(["git", "-C", checkout, "checkout", branch], stdout=subprocess.DEVNULL)
    run(["git", "-C", checkout, "pull", "--ff-only", "origin", branch], stdout=subprocess.DEVNULL)


def ensure_checkout(label: str, url: str, checkout: Path) -> None:
    if (checkout / ".git").is_dir():
        log(f"Updating {label} checkout at {checkout}")
        run(["git", "-C", checkout, "fetch", "--prune", "--tags", "origin"], stdout=subprocess.DEVNULL)
        return

    if checkout.exists():
        raise ReleaseExportError(f"{checkout} exists but is not a Git checkout")

    log(f"Cloning {label} from {url}")
    checkout.parent.mkdir(parents=True, exist_ok=True)
    run(["git", "clone", url, checkout], stdout=subprocess.DEVNULL)


def ensure_clean_checkout(label: str, checkout: Path) -> None:
    status = run_output(["git", "-C", checkout, "status", "--porcelain"])
    if status:
        raise ReleaseExportError(f"{label} checkout has uncommitted changes: {checkout}")


def reset_disposable_checkout(label: str, checkout: Path) -> None:
    log(f"Resetting disposable {label} checkout at {checkout}")
    run(["git", "-C", checkout, "reset", "--hard", "HEAD"], stdout=subprocess.DEVNULL)
    run(["git", "-C", checkout, "clean", "-fdx"], stdout=subprocess.DEVNULL)


def tracked_paths(checkout: Path) -> list[Path]:
    paths = run_output(["git", "-C", checkout, "ls-files", "-z"])
    return [Path(path) for path in paths.split("\0") if path]


def copy_tracked_worktree(source: Path, destination: Path) -> None:
    source_paths = set(tracked_paths(source))
    destination_paths = set(tracked_paths(destination))

    for relative_path in destination_paths - source_paths:
        destination_path = destination / relative_path
        if destination_path.is_dir():
            shutil.rmtree(destination_path)
        elif destination_path.exists():
            destination_path.unlink()

    for relative_path in source_paths:
        source_path = source / relative_path
        destination_path = destination / relative_path

        if source_path.exists():
            destination_path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source_path, destination_path)
        elif destination_path.exists():
            destination_path.unlink()


def prepare_local_internal_checkout(repo_root: Path, internal_checkout: Path) -> None:
    log(f"Preparing internal Performix checkout from local repository at {repo_root}")
    if internal_checkout.exists():
        shutil.rmtree(internal_checkout)

    internal_checkout.parent.mkdir(parents=True, exist_ok=True)
    run(["git", "clone", repo_root, internal_checkout], stdout=subprocess.DEVNULL)
    copy_tracked_worktree(repo_root, internal_checkout)


def resolve_manifest(repo_root: Path, internal_checkout: Path) -> Path:
    manifest_path = env_value("MANIFEST_PATH")
    candidates = []

    if manifest_path:
        manifest = Path(manifest_path)
        candidates.append(manifest if manifest.is_absolute() else repo_root / manifest)
    else:
        candidates.extend(
            [
                repo_root / ".ossmosis.json",
                internal_checkout / ".ossmosis.json",
            ]
        )

    for candidate in candidates:
        if candidate.is_file():
            return candidate

    tried = ", ".join(str(candidate) for candidate in candidates)
    raise ReleaseExportError(f"manifest not found; checked {tried}")


def resolve_latest_tag(internal_checkout: Path) -> str:
    tags = run_output(["git", "-C", internal_checkout, "tag", "--list", "--sort=v:refname"])
    release_tags = [tag for tag in tags.splitlines() if RELEASE_TAG_RE.match(tag)]
    return release_tags[-1] if release_tags else ""


def venv_path(ossmosis_checkout: Path) -> Path:
    return ossmosis_checkout / ".venv"


def venv_python(venv: Path) -> Path:
    return venv / ("Scripts/python.exe" if os.name == "nt" else "bin/python")


def venv_ossmosis(venv: Path) -> Path:
    return venv / ("Scripts/ossmosis.exe" if os.name == "nt" else "bin/ossmosis")


def ensure_ossmosis(ossmosis_checkout: Path) -> Path:
    override = env_value("OSSMOSIS_BINARY")
    if override:
        binary = Path(override)
        if not binary.is_file() or (os.name != "nt" and not os.access(binary, os.X_OK)):
            raise ReleaseExportError(f"OSSMOSIS_BINARY is not executable: {binary}")
        return binary

    venv = venv_path(ossmosis_checkout)
    binary = venv_ossmosis(venv)
    if not binary.is_file():
        log(f"Installing ossmosis in {venv}")
        run([sys.executable, "-m", "venv", venv])
        run(
            [
                venv_python(venv),
                "-m",
                "pip",
                "install",
                "--disable-pip-version-check",
                "-e",
                ossmosis_checkout,
            ]
        )

    return binary


def public_branch_slug(value: str) -> str:
    slug = re.sub(r"[^A-Za-z0-9._-]+", "-", value.lower())
    slug = re.sub(r"-+", "-", slug).strip(".-")
    return slug or "snapshot"


def default_public_branch(snapshot_identifier: str) -> str:
    return f"performix/{public_branch_slug(snapshot_identifier)}"


def expected_public_commit_message(identity: PublishIdentity) -> str:
    if identity.local:
        return identity.name
    return f"Update open-source repository for Arm Performix version {identity.name}"


def update_public_commit_metadata(
    public_checkout: Path,
    identity: PublishIdentity,
    previous_head: str,
) -> str:
    current_head = run_output(["git", "-C", public_checkout, "rev-parse", "HEAD"])
    if current_head == previous_head:
        raise ReleaseExportError("ossmosis did not create a new public source commit")

    run(
        [
            "git",
            "-C",
            public_checkout,
            "commit",
            "--amend",
            "-m",
            expected_public_commit_message(identity),
        ],
        stdout=subprocess.DEVNULL,
    )
    updated_head = run_output(["git", "-C", public_checkout, "rev-parse", "HEAD"])
    if identity.creates_tag:
        run(["git", "-C", public_checkout, "tag", "-f", identity.name, updated_head], stdout=subprocess.DEVNULL)
    return run_output(["git", "-C", public_checkout, "rev-parse", "--short", "HEAD"])


def validate_prepared_public_snapshot(public_checkout: Path, identity: PublishIdentity) -> str:
    if not (public_checkout / ".git").is_dir():
        prepare_command = (
            f"task release:prepare LOCAL=true COMMIT_MSG={shlex.quote(identity.name)}"
            if identity.local
            else f"task release:prepare TAG={shlex.quote(identity.name)}"
        )
        raise ReleaseExportError(
            f"public checkout not found at {public_checkout}; "
            f"run {prepare_command} first"
        )

    head = run_output(["git", "-C", public_checkout, "rev-parse", "HEAD"], check=False)
    if not head:
        raise ReleaseExportError(f"public checkout has no HEAD commit: {public_checkout}")
    if identity.creates_tag:
        tag_target = run_output(
            ["git", "-C", public_checkout, "rev-parse", "--verify", f"refs/tags/{identity.name}^{{}}"],
            check=False,
        )
        if not tag_target:
            raise ReleaseExportError(f"prepared public snapshot tag not found: {identity.name}")
        if head != tag_target:
            raise ReleaseExportError(f"prepared public snapshot tag {identity.name} does not point at HEAD")

    message = run_output(["git", "-C", public_checkout, "log", "-1", "--format=%B"], check=False).rstrip("\n")
    expected_message = expected_public_commit_message(identity)
    if message != expected_message:
        raise ReleaseExportError(
            f"prepared public snapshot commit has unexpected message: {message!r}; "
            f"expected {expected_message!r}"
        )

    ensure_clean_checkout("public Performix", public_checkout)
    return run_output(["git", "-C", public_checkout, "rev-parse", "--short", "HEAD"])


def push_public_snapshot(public_checkout: Path, identity: PublishIdentity, public_branch: str) -> None:
    log(f"Pushing public snapshot commit to {public_branch}")
    run(["git", "-C", public_checkout, "push", "origin", f"HEAD:refs/heads/{public_branch}"])
    if identity.creates_tag:
        log(f"Pushing public snapshot tag {identity.name}")
        run(["git", "-C", public_checkout, "push", "origin", f"refs/tags/{identity.name}"])


def get_release_workdir(repo_root: Path) -> Path:
    return Path(env_value("RELEASE_WORKDIR", str(repo_root / ".release-worktrees")))


def release_state_path(release_workdir: Path) -> Path:
    return release_workdir / RELEASE_STATE_FILENAME


def write_release_state(release_workdir: Path, identity: PublishIdentity, public_branch: str) -> None:
    state = {
        "schema_version": 1,
        "local": identity.local,
        "name": identity.name,
        "public_branch": public_branch,
    }
    release_workdir.mkdir(parents=True, exist_ok=True)
    release_state_path(release_workdir).write_text(json.dumps(state, indent=2) + "\n", encoding="utf-8")


def read_release_state(release_workdir: Path) -> tuple[PublishIdentity, str]:
    state_path = release_state_path(release_workdir)
    if not state_path.is_file():
        raise ReleaseExportError(
            f"prepared release state not found at {state_path}; "
            "pass TAG=<tag> or LOCAL=true COMMIT_MSG=<message>"
        )

    try:
        state = json.loads(state_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as error:
        raise ReleaseExportError(f"prepared release state is not valid JSON: {state_path}") from error

    if not isinstance(state, dict) or state.get("schema_version") != 1:
        raise ReleaseExportError(f"prepared release state has an unsupported format: {state_path}")

    local = state.get("local")
    name = state.get("name")
    public_branch = state.get("public_branch")
    if not isinstance(local, bool) or not isinstance(name, str) or not name:
        raise ReleaseExportError(f"prepared release state is missing snapshot identity: {state_path}")
    if not isinstance(public_branch, str) or not public_branch:
        raise ReleaseExportError(f"prepared release state is missing public branch: {state_path}")

    return PublishIdentity(name, local=local), public_branch


def clean_release_workdir(repo_root: Path) -> None:
    workdir = get_release_workdir(repo_root)
    shutil.rmtree(workdir, ignore_errors=True)
    log(f"Removed release workdir: {workdir}")


def format_task_assignment(name: str, value: str) -> str:
    return f"{name}={shlex.quote(value)}"


def suggested_push_command(identity: PublishIdentity, public_branch: str) -> str:
    parts = [
        "task",
        "release:push",
    ]
    if identity.local:
        parts.append(format_task_assignment("LOCAL", "true"))
        parts.append(format_task_assignment("COMMIT_MSG", identity.name))
    else:
        parts.append(format_task_assignment("TAG", identity.name))

    release_workdir = env_value("RELEASE_WORKDIR")
    if release_workdir:
        parts.append(format_task_assignment("RELEASE_WORKDIR", release_workdir))

    expected_public_branch = default_public_branch(identity.name)
    if public_branch != expected_public_branch:
        parts.append(format_task_assignment("PUBLIC_BRANCH", public_branch))

    return " ".join(parts)


def suggested_stateful_push_command() -> str:
    parts = [
        "task",
        "release:push",
    ]

    release_workdir = env_value("RELEASE_WORKDIR")
    if release_workdir:
        parts.append(format_task_assignment("RELEASE_WORKDIR", release_workdir))

    return " ".join(parts)


def prepare_release_snapshot(repo_root: Path) -> None:
    require_no_legacy_local_internal()
    require_no_legacy_snapshot_name()
    use_local = env_bool("LOCAL")

    require_command("git")

    release_workdir = get_release_workdir(repo_root)
    ossmosis_checkout = release_workdir / "ossmosis"
    internal_checkout = release_workdir / "internal-performix"
    public_checkout = release_workdir / "public-performix"
    selected_tag = env_value("TAG")
    commit_msg = env_value("COMMIT_MSG")
    public_branch = env_value("PUBLIC_BRANCH")

    if use_local and selected_tag:
        raise ReleaseExportError("TAG cannot be used with LOCAL=true; use COMMIT_MSG instead")
    if use_local and not commit_msg:
        raise ReleaseExportError("COMMIT_MSG is required when LOCAL=true")
    if not use_local and commit_msg:
        raise ReleaseExportError("COMMIT_MSG is only valid with LOCAL=true")

    log("Preparing release snapshot")
    log(f"Release workdir: {release_workdir}")
    log(f"Public repo target: {PUBLIC_REPO_URL}")

    ensure_checkout("ossmosis", OSSMOSIS_REPO_URL, ossmosis_checkout)
    checkout_default_branch(ossmosis_checkout)

    if use_local:
        prepare_local_internal_checkout(repo_root, internal_checkout)
    else:
        ensure_checkout("internal Performix", INTERNAL_REPO_URL, internal_checkout)
        reset_disposable_checkout("internal Performix", internal_checkout)

    ensure_checkout("public Performix", PUBLIC_REPO_URL, public_checkout)
    ensure_clean_checkout("public Performix", public_checkout)
    checkout_default_branch(public_checkout)
    ensure_clean_checkout("public Performix", public_checkout)

    if not use_local and not selected_tag:
        selected_tag = resolve_latest_tag(internal_checkout)
        if not selected_tag:
            raise ReleaseExportError(f"could not resolve latest release tag from {INTERNAL_REPO_URL}")
        log(f"Resolved latest internal tag: {selected_tag}")

    identity = PublishIdentity(commit_msg, local=True) if use_local else PublishIdentity(selected_tag)

    if use_local:
        log(f"Using local Performix snapshot as {identity.name}")
    else:
        log(f"Checking out internal Performix tag {identity.name}")
        run(["git", "-C", internal_checkout, "checkout", "--detach", identity.name], stdout=subprocess.DEVNULL)

    manifest = resolve_manifest(repo_root, internal_checkout)
    log(f"Using manifest: {manifest}")

    ossmosis_binary = ensure_ossmosis(ossmosis_checkout)
    public_branch = public_branch or default_public_branch(identity.name)
    public_head_before_export = run_output(["git", "-C", public_checkout, "rev-parse", "HEAD"])

    log(f"Exporting {identity.name} with max classification {MAX_CLASS}")
    run(
        [
            ossmosis_binary,
            "export",
            "--source",
            internal_checkout,
            "--output",
            public_checkout,
            "--manifest",
            manifest,
            "--max-class",
            MAX_CLASS,
        ]
    )

    export_commit = update_public_commit_metadata(
        public_checkout,
        identity,
        public_head_before_export,
    )

    log("Scanning exported public checkout")
    run(
        [
            ossmosis_binary,
            "scan",
            "--root",
            public_checkout,
            "--manifest",
            manifest,
            "--max-class",
            MAX_CLASS,
        ]
    )
    write_release_state(release_workdir, identity, public_branch)

    log()
    log(f"Created public snapshot commit {export_commit}")
    if identity.creates_tag:
        log(f"Created public snapshot tag {identity.name}")
        log(f"Selected internal tag: {identity.name}")
    else:
        log(f"Selected local snapshot: {identity.name}")
    log(f"Public checkout: {public_checkout}")
    log("Inspect the local commit before pushing:")
    log(f"  git -C {public_checkout} show --stat HEAD")
    log(f"  git -C {public_checkout} diff HEAD^..HEAD")
    if identity.creates_tag:
        log(f"  git -C {public_checkout} tag --points-at HEAD")
    log("Push after review with:")
    log(f"  {suggested_stateful_push_command()}")
    log("Override the public review branch with PUBLIC_BRANCH=<branch> if needed.")


def push_prepared_release_snapshot(repo_root: Path) -> None:
    require_no_legacy_local_internal()
    require_no_legacy_snapshot_name()
    require_command("git")

    selected_tag = env_value("TAG")
    commit_msg = env_value("COMMIT_MSG")
    use_local = env_bool("LOCAL")
    release_workdir = get_release_workdir(repo_root)

    if use_local and selected_tag:
        raise ReleaseExportError("TAG cannot be used with LOCAL=true; use COMMIT_MSG instead")
    if use_local and not commit_msg:
        raise ReleaseExportError("COMMIT_MSG is required when LOCAL=true")
    if not use_local and commit_msg:
        raise ReleaseExportError("COMMIT_MSG is only valid with LOCAL=true")

    if env_value("MANIFEST_PATH"):
        raise ReleaseExportError("MANIFEST_PATH is only valid with task release:prepare")

    if selected_tag or commit_msg or use_local:
        identity = PublishIdentity(commit_msg, local=True) if use_local else PublishIdentity(selected_tag)
        public_branch = env_value("PUBLIC_BRANCH")
        if use_local and not public_branch:
            _, public_branch = read_release_state(release_workdir)
        public_branch = public_branch or default_public_branch(identity.name)
    else:
        identity, prepared_public_branch = read_release_state(release_workdir)
        public_branch = env_value("PUBLIC_BRANCH", prepared_public_branch)

    public_checkout = release_workdir / "public-performix"
    export_commit = validate_prepared_public_snapshot(public_checkout, identity)

    log("Pushing existing reviewed public snapshot")
    log(f"Public checkout: {public_checkout}")
    log(f"Public snapshot commit: {export_commit}")
    if identity.creates_tag:
        log(f"Public snapshot tag: {identity.name}")
    else:
        log(f"Public local snapshot: {identity.name}")
    push_public_snapshot(public_checkout, identity, public_branch)


def main() -> int:
    try:
        script_dir = Path(__file__).resolve().parent
        repo_root = script_dir.parent.parent

        args = sys.argv[1:] or ["--prepare"]
        if env_value("PUSH"):
            raise ReleaseExportError("PUSH is no longer supported; use task release:push after local review")

        if args == ["--clean"]:
            clean_release_workdir(repo_root)
            return 0

        if args == ["--prepare"]:
            prepare_release_snapshot(repo_root)
            return 0

        if args == ["--push"]:
            push_prepared_release_snapshot(repo_root)
            return 0

        raise ReleaseExportError("expected one of: --prepare, --push, --clean")
    except ReleaseExportError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
