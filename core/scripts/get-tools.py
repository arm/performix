#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
get-tools.py - Retrieves the APX tool bundles and packages them.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import os
import platform
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import urllib.error
import urllib.request
import zipfile
from pathlib import Path


SCRIPT_DIR = Path(__file__).resolve().parent

STANDARD_TOOLS = [
    "neoprof",
    "target_agent",
    "instruction_mix",
    "cache_sharing",
    "asct",
    "jitdump_jvm",
    "dotnet_agent",
    "wperf",
    "sysutil-timeline",
    "syscall-trace",
]

# Pre-release tools are not pulled by default. They can be pulled using the
# --pre-release flag or by specifying them directly.
# Tools can be added here if they are not yet ready for release (e.g if the TPIP
# is not ready) as they are not pulled into CI builds.
PRE_RELEASE_TOOLS = [
    "topdown_tool",
    "wperf_cmn_visualizer",
    "cmn_tools",
]

RELEASE_TARGETS = [
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("windows", "amd64"),
    ("windows", "arm64"),
]
RELEASE_ARCH_NAMES = {"amd64": "x86_64", "arm64": "aarch64"}

ARTIFACTORY_BASE_URL = "https://artifactory.arm.com/artifactory"

_NEOPROF_VARIANTS = [
    ("Linux", "aarch64"),
    ("Linux", "x86_64"),
    ("Windows", "aarch64"),
    ("Windows", "x86_64"),
    ("Darwin", "aarch64"),
    ("Darwin", "x86_64"),
]

_NEOPROF_ANDROID_ARCH = "aarch64"
_NEOPROF_ANDROID_GATORD_ARTIFACT_ID = "gator-build-gatord-ndk-aarch64"

_SL_ANALYZE_TOOL = "sl-analyze"
_SL_RECORD_TOOL = "sl-record"

def _find_msys2_bash() -> Path:
    """
    Locate MSYS2 bash.exe on Windows. Checks PATH first, then common
    MSYS2 installation locations. MSYS2 is a prerequisite on Windows.
    """
    bash = shutil.which("bash")
    if bash:
        return Path(bash)
    for candidate in [
        Path(r"C:\msys64\usr\bin\bash.exe"),
        Path(r"C:\msys32\usr\bin\bash.exe"),
        Path(r"C:\tools\msys64\usr\bin\bash.exe"),
    ]:
        if candidate.is_file():
            return candidate
    raise RuntimeError(
        "bash.exe not found. MSYS2 is required on Windows. "
        r"Ensure MSYS2 is installed and C:\msys64\usr\bin is in your PATH."
    )


def _run_script(cmd: list) -> None:
    """
    Synchronously run a subprocess, inheriting stdout/stderr.
    """
    if not cmd:
        raise ValueError("No command provided to _run_script")

    # On Windows, use MSYS2 bash
    if _is_windows_host() and Path(str(cmd[0])).suffix == ".sh":
        bash = str(_find_msys2_bash())
        str_cmd = [str(c) for c in cmd]
        cmd = [bash] + [c.replace("\\", "/") for c in str_cmd]

    result = subprocess.run(cmd)
    if result.returncode != 0:
        raise RuntimeError(f"Command failed (exit {result.returncode})")


def _artifactory_api_token() -> str:
    token = os.environ.get("ARTIFACTORY_API_TOKEN", "")
    if not token:
        raise RuntimeError("ARTIFACTORY_API_TOKEN env var should be exposed!")
    return token


def _check_unzip() -> None:
    """
    Verify the system unzip command is available (required by shell sub-scripts).
    """
    if not shutil.which("unzip"):
        raise RuntimeError("Error: unzip is not installed.")


def _check_bash() -> None:
    """
    Verify bash is available on all platforms (required to run .sh sub-scripts).
    """
    if _is_windows_host():
        _find_msys2_bash()  # raises RuntimeError with a clear message if not found
    elif not shutil.which("bash"):
        raise RuntimeError("Error: bash is not installed.")


def _download(url: str, dest: Path, *, token: str | None = None) -> None:
    """
    Downloads artifactory assets from the given URL in to the destionation path.
    """
    headers: dict[str, str] = {}
    if token:
        headers["X-JFrog-Art-Api"] = token
    req = urllib.request.Request(url, headers=headers)
    dest.parent.mkdir(parents=True, exist_ok=True)
    try:
        with urllib.request.urlopen(req) as resp, dest.open("wb") as fh:
            shutil.copyfileobj(resp, fh)
    except urllib.error.URLError as exc:
        raise RuntimeError(f"Download failed from {url}: {exc}") from exc


def _make_executable(path: Path) -> None:
    if path.exists():
        path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


def _is_windows_host() -> bool:
    s = platform.system().lower()
    return s.startswith(("mingw", "msys", "cygwin", "windows"))


def _read_go_const(versions_file: Path, const_name: str) -> str:
    pattern = re.compile(rf'const\s+{re.escape(const_name)}\s*=\s*"([^"]+)"')
    for line in versions_file.read_text().splitlines():
        m = pattern.search(line)
        if m:
            return m.group(1)
    raise ValueError(f"Could not find constant '{const_name}' in {versions_file}")


def _is_snapshot_build() -> bool:
    """Return whether built-in tool bundles should use the snapshot engine version.

    GoReleaser snapshot binaries use <version>-dev. Workflows set
    PERFORMIX_SNAPSHOT_BUILD from the same condition that controls GoReleaser's
    --snapshot argument, so get-tools.py packages built-in tool bundles under
    the version the runtime exposes as performix.engineVersion.
    """
    value = os.environ.get("PERFORMIX_SNAPSHOT_BUILD")
    if value is not None:
        return value.strip().lower() in ("1", "true", "yes", "on")

    # Keep SNAPSHOT_ARG as a fallback for older callers.
    return bool(os.environ.get("SNAPSHOT_ARG", "").strip())


def _get_engine_version() -> str:
    version = os.environ.get("PERFORMIX_ENGINE_VERSION", "").strip()
    if not version:
        version = subprocess.check_output(
            [sys.executable, SCRIPT_DIR / "get_atperf_version.py"], text=True
        ).strip()
    if not version:
        raise RuntimeError("Could not determine Performix engine version")
    if _is_snapshot_build():
        version = f"{version}-dev"
    return version


def _get_builtin_tool_source(tool_name: str, required_file: str) -> Path:
    source_dir = SCRIPT_DIR.parent / "apap-cli" / "tools-builtin" / tool_name
    source_file = source_dir / required_file
    if not source_file.exists():
        raise FileNotFoundError(
            f"{required_file} not found for {tool_name} at {source_file}"
        )
    return source_dir


# ------------------------------------------------------------------------------
# neoprof
# ------------------------------------------------------------------------------


def _neoprof_arch_alt(arch: str) -> str:
    return {"aarch64": "arm64", "x86_64": "x64"}.get(arch, arch)


def _neoprof_artifact_info(os_name: str) -> tuple[str, str]:
    """
    Returns (artifact_os, artifact_toolchain) for the Artifactory path.
    """
    mapping = {
        "Linux": ("linux", "gcc-baseline"),
        "Windows": ("windows", "clang"),
        "Darwin": ("macos", "clang"),
    }
    if os_name not in mapping:
        raise ValueError(f"Unknown OS for neoprof: {os_name}")
    return mapping[os_name]


def _map_host_tools_dir(os_name: str, arch: str) -> tuple[str, str]:
    os_map = {"Linux": "linux", "Windows": "windows", "Darwin": "darwin"}
    arch_map = {"aarch64": "arm64", "x86_64": "x64"}
    if os_name not in os_map:
        raise ValueError(f"Unknown OS for host tools: {os_name}")
    return os_map[os_name], arch_map.get(arch, arch)


def _find_named_file(root: Path, file_name: str) -> Path | None:
    matches = [
        path
        for path in root.rglob(file_name)
        if path.is_file() and "__MACOSX" not in path.parts
    ]
    if not matches:
        return None
    if len(matches) == 1:
        return matches[0]

    match_list = ", ".join(str(path.relative_to(root)) for path in matches)
    raise FileNotFoundError(
        f"Expected exactly one {file_name} under {root}, found: {match_list}"
    )


def _neoprof_tool_dst_dir(
    tool_dst_base: Path, tool_name: str, version: str, rc: str
) -> Path:
    """
    Return the top-level destination directory for a packaged neoprof tool.
    """
    return tool_dst_base / tool_name / f"{version}-{rc}"


def _neoprof_package_tool_bundle(
    src_tmp_dir: Path,
    tool_dst_base: Path,
    tool_name: str,
    version: str,
    rc: str,
    os_name: str,
    arch: str,
    files_to_tar: list[str],
) -> None:
    """
    Create a packaged tool bundle tarball from files staged under src_tmp_dir.
    """
    tool_dst_dir = _neoprof_tool_dst_dir(tool_dst_base, tool_name, version, rc)
    output_file = tool_dst_dir / f"{tool_name}-{os_name}-{arch}.tar.gz"
    tool_dst_dir.mkdir(parents=True, exist_ok=True)

    is_win = _is_windows_host()

    def _filter(ti: tarfile.TarInfo) -> tarfile.TarInfo:
        if is_win:
            ti.uid = ti.gid = 0
            ti.uname = ti.gname = ""
            ti.mode = 0o755
        return ti

    with tarfile.open(output_file, "w:gz") as tf:
        for rel in files_to_tar:
            tf.add(src_tmp_dir / rel, arcname=rel, filter=_filter)
    print(f"[neoprof] Package created: {output_file}")


def _neoprof_prepare_binary(tmp_dir: Path, os_name: str, binary_name: str) -> Path:
    """
    Resolve a neoprof binary from the extracted archive, stage it at bin/<name>,
    and apply executable bits on non-Windows platforms.
    """
    binary: Path | None = None
    for candidate in [
        tmp_dir / "bin" / binary_name,
        tmp_dir / binary_name,
    ]:
        if candidate.is_file():
            binary = candidate
            break

    if binary is None:
        matches = [p for p in tmp_dir.rglob(binary_name) if p.is_file()]
        if len(matches) == 1:
            binary = matches[0]
        else:
            raise FileNotFoundError(f"{binary_name} not found in {tmp_dir}")

    staged_binary = tmp_dir / "bin" / binary_name
    if binary != staged_binary:
        staged_binary.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(binary, staged_binary)
    if os_name != "Windows":
        _make_executable(staged_binary)
    return staged_binary


# TODO: simplify this once Android sl-record is available from the neoprof
# artifact layout: https://github.com/Arm-Debug/performix/pull/2284
def _get_neoprof_android_variant(
    tool_dst_base: Path,
    staging_dir: Path,
    version: str,
    rc: str,
    android_sl_record_version: str,
    tpip_src: Path,
    token: str,
) -> None:
    """Download Android gatord and package it as an sl-record tool bundle."""
    tmp_dir = staging_dir / f"__tmp-Android-{_NEOPROF_ANDROID_ARCH}"
    extract_dir = tmp_dir / "gatord"

    artifact_id = _NEOPROF_ANDROID_GATORD_ARTIFACT_ID
    repo_path = (
        "mobile-studio.streamline-maven-releases/com/arm/streamline/"
        f"{artifact_id}/{android_sl_record_version}"
    )
    zip_name = f"{artifact_id}-{android_sl_record_version}-target-binary.zip"
    zip_path = tmp_dir / zip_name
    url = f"{ARTIFACTORY_BASE_URL}/{repo_path}/{zip_name}"

    tmp_dir.mkdir(parents=True, exist_ok=True)
    print(f"[neoprof] Downloading Android gatord from {repo_path} …")
    _download(url, zip_path, token=token)

    extract_dir.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(zip_path) as zf:
        zf.extractall(extract_dir)
    zip_path.unlink()

    gatord = _find_named_file(extract_dir, "gatord")
    if gatord is None:
        raise FileNotFoundError(f"gatord not found in {extract_dir}")

    staged_sl_record = tmp_dir / "bin" / _SL_RECORD_TOOL
    staged_sl_record.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(gatord, staged_sl_record)
    _make_executable(staged_sl_record)

    if not tpip_src.is_file():
        raise FileNotFoundError(f"third_party_licenses.txt not found in {tpip_src}")
    tpip_dst = tmp_dir / "license_terms" / "third_party_licenses.txt"
    tpip_dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(tpip_src, tpip_dst)

    _neoprof_package_tool_bundle(
        tmp_dir,
        tool_dst_base,
        _SL_RECORD_TOOL,
        version,
        rc,
        "Android",
        _NEOPROF_ANDROID_ARCH,
        ["bin/sl-record", "license_terms/third_party_licenses.txt"],
    )


def _get_neoprof_variant(
    os_name: str,
    arch: str,
    tool_dst_base: Path,
    staging_dir: Path,
    sl_analyze_host_base: Path,
    version: str,
    rc: str,
    token: str,
) -> Path:
    """
    Downloads and repackages a single neoprof variant (e.g. Linux, AArch64).
    Returns the path to the temp directory created; caller needs to clean it up.
    """
    arch_alt = _neoprof_arch_alt(arch)
    artifact_os, artifact_toolchain = _neoprof_artifact_info(os_name)

    archive_name = (
        f"neoverse-profiler-{arch_alt}-{artifact_os}-{artifact_toolchain}.zip"
    )

    # Use a per-variant tmp dir so concurrent calls don't collide
    tmp_dir = staging_dir / f"__tmp-{os_name}-{arch}"

    url = (
        f"{ARTIFACTORY_BASE_URL}/mobile-studio.builds/streamline-cxx/releases/neoprof"
        f"/{version}/post-commit/{rc}/{archive_name}"
    )

    zip_path = staging_dir / archive_name
    staging_dir.mkdir(parents=True, exist_ok=True)

    print(f"[neoprof] Downloading {os_name}/{arch} …")
    _download(url, zip_path, token=token)

    tmp_dir.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(zip_path) as zf:
        zf.extractall(tmp_dir)
    zip_path.unlink()

    # Package sl-analyze for all platforms
    sl_analyze_name = "sl-analyze.exe" if os_name == "Windows" else "sl-analyze"
    sl_analyze = _neoprof_prepare_binary(tmp_dir, os_name, sl_analyze_name)
    _neoprof_package_tool_bundle(
        tmp_dir,
        tool_dst_base,
        _SL_ANALYZE_TOOL,
        version,
        rc,
        os_name,
        arch,
        [f"bin/{sl_analyze_name}", "license_terms/third_party_licenses.txt"],
    )

    # Package sl-record for Linux only
    if os_name == "Linux":
        _neoprof_prepare_binary(tmp_dir, os_name, "sl-record")
        _neoprof_package_tool_bundle(
            tmp_dir,
            tool_dst_base,
            _SL_RECORD_TOOL,
            version,
            rc,
            os_name,
            arch,
            ["bin/sl-record", "license_terms/third_party_licenses.txt"],
        )

    # Copy sl-analyze into the host-tools directory.
    host_os_dir, host_arch_dir = _map_host_tools_dir(os_name, arch)
    host_dir = sl_analyze_host_base / f"{host_os_dir}-{host_arch_dir}"
    shutil.rmtree(host_dir, ignore_errors=True)
    host_dir.mkdir(parents=True, exist_ok=True)

    shutil.copy2(sl_analyze, host_dir / sl_analyze_name)

    print(f"[neoprof] Done {os_name}/{arch}")
    return tmp_dir


def _copy_neoprof_tpip(
    variant_tmp_dirs: dict[tuple[str, str], Path], tool_dst_dir: Path
) -> None:
    """
    Retrieves the TPIP from the first variant's temp directory.
    Only the first variant is choosen since all of them have the same TPIP.
    """
    tmp_dir = next(iter(variant_tmp_dirs.values()))
    tpip_src = tmp_dir / "license_terms" / "third_party_licenses.txt"

    if not tpip_src.is_file():
        raise FileNotFoundError(f"third_party_licenses.txt not found in {tpip_src}")

    tpip_out = tool_dst_dir / "license_terms" / "third_party_licenses.txt"
    tpip_out.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(tpip_src, tpip_out)


def get_neoprof(tool_dst_base: Path, sl_analyze_host_base: Path) -> None:
    """
    Downloads all neoprof variants in parallel and packages them.
    """
    versions_file = (
        SCRIPT_DIR.parent / "atperf-version" / "versions" / "tool_versions.go"
    )
    token = _artifactory_api_token()
    version = _read_go_const(versions_file, "NeoprofVersion")
    rc = _read_go_const(versions_file, "NeoprofReleaseCandidate")
    android_sl_record_version = _read_go_const(
        versions_file, "AndroidSlRecordVersion"
    )
    staging_dir = tool_dst_base / "__neoprof-staging" / f"{version}-{rc}"

    # One temp dir per variant (e.g., Linux-AArch64)
    variant_tmp_dirs = {
        (os_name, arch): staging_dir / f"__tmp-{os_name}-{arch}"
        for os_name, arch in _NEOPROF_VARIANTS
    }

    errors: list[str] = []

    # List of completed get_neoprof threads per variant
    # Maps (os-name, arch) -> temp directory path for cleanup and TPIP
    # (e.g., ("Linux", "aarch64") -> Path("tools/__neoprof-staging/1.0/__tmp-Linux-aarch64"))
    completed: dict[tuple[str, str], Path] = {}

    try:
        with concurrent.futures.ThreadPoolExecutor() as pool:
            futures = {
                pool.submit(
                    _get_neoprof_variant,
                    os_name,
                    arch,
                    tool_dst_base,
                    staging_dir,
                    sl_analyze_host_base,
                    version,
                    rc,
                    token,
                ): (os_name, arch)
                for os_name, arch in _NEOPROF_VARIANTS
            }

            for fut in concurrent.futures.as_completed(futures):
                os_name, arch = futures[fut]
                try:
                    completed[(os_name, arch)] = fut.result()
                except Exception as exc:
                    errors.append(f"neoprof {os_name}/{arch}: {exc}")

        if errors:
            raise RuntimeError("\n".join(errors))

        # Add TPIP to tool directories
        _copy_neoprof_tpip(
            completed,
            _neoprof_tool_dst_dir(tool_dst_base, _SL_ANALYZE_TOOL, version, rc),
        )
        _copy_neoprof_tpip(
            completed,
            _neoprof_tool_dst_dir(tool_dst_base, _SL_RECORD_TOOL, version, rc),
        )

        sl_record_tpip = (
            _neoprof_tool_dst_dir(tool_dst_base, _SL_RECORD_TOOL, version, rc)
            / "license_terms"
            / "third_party_licenses.txt"
        )
        _get_neoprof_android_variant(
            tool_dst_base,
            staging_dir,
            version,
            rc,
            android_sl_record_version,
            sl_record_tpip,
            token,
        )
    finally:
        for tmp_dir in variant_tmp_dirs.values():
            shutil.rmtree(tmp_dir, ignore_errors=True)
        shutil.rmtree(staging_dir.parent, ignore_errors=True)


# ------------------------------------------------------------------------------
# target_agent
# ------------------------------------------------------------------------------


def get_target_agent(tool_dst_base: Path) -> None:
    """
    Builds all target agent variants locally using gorelease and packages them.
    """

    if os.getenv("SKIP_APX_AGENT_BUILD"):
        print("Skipping APX agent build")
        return

    agent_bin = subprocess.check_output(
        [
            sys.executable,
            SCRIPT_DIR / "terminology" / "terminology.py",
            "get_agent_binary_name",
        ],
        text=True,
    ).strip()

    artifacts_dir = SCRIPT_DIR.parent / f"{agent_bin}-artifacts"
    if not artifacts_dir.is_dir():
        _run_script(
            [
                sys.executable,
                SCRIPT_DIR / "build_target_agent.py",
                "--config",
                SCRIPT_DIR.parent / ".goreleaser-agent.yml",
                "--no-inject",
                "--no-sign",
                "--snapshot",
            ]
        )
        dist_dir = SCRIPT_DIR.parent.parent / "dist"
        dist_dir.rename(artifacts_dir)

    target_agent_version = os.environ.get("TARGET_AGENT_VERSION", "")
    snapshot_build = _is_snapshot_build()
    if not target_agent_version:
        target_agent_version = subprocess.check_output(
            [sys.executable, SCRIPT_DIR / "get_atperf_version.py"], text=True
        ).strip()
    elif snapshot_build:
        target_agent_version = f"{target_agent_version}-dev"

    agent_variants = [
        ("Android", "aarch64", f"{agent_bin}-android-arm64.tar.gz"),
        ("Linux", "aarch64", f"{agent_bin}-linux-arm64.tar.gz"),
        ("Linux", "x86_64", f"{agent_bin}-linux-amd64.tar.gz"),
        ("Windows", "aarch64", f"{agent_bin}-windows-arm64.tar.gz"),
        ("Windows", "x86_64", f"{agent_bin}-windows-amd64.tar.gz"),
        ("Darwin", "aarch64", f"{agent_bin}-darwin-arm64.tar.gz"),
        ("Darwin", "x86_64", f"{agent_bin}-darwin-amd64.tar.gz")
    ]
    for os_name, arch, src in agent_variants:
        _run_script(
            [
                sys.executable,
                SCRIPT_DIR / "bundle_tool.py",
                "--tool-name",
                agent_bin,
                "--version",
                target_agent_version,
                "--os",
                os_name,
                "--arch",
                arch,
                "--source",
                str(artifacts_dir / src),
                "--tools-dir",
                str(tool_dst_base),
            ]
        )

    shutil.rmtree(artifacts_dir)


# ------------------------------------------------------------------------------
# sysutil-timeline
# ------------------------------------------------------------------------------


def get_sysutil_timeline(tool_dst_base: Path) -> None:
    """
    Builds all sysutil-timeline variants locally and packages them.
    """
    source_dir = _get_builtin_tool_source("sysutil-timeline", "sysutil-timeline.py")

    version = _get_engine_version()
    tool_dst_dir = tool_dst_base / "sysutil-timeline" / version
    tool_dst_dir.mkdir(parents=True, exist_ok=True)

    _EXCLUDE_NAMES = {"__pycache__", "env", "tests"}

    def _filter(ti: tarfile.TarInfo) -> tarfile.TarInfo | None:
        for part in Path(ti.name).parts:
            if part in _EXCLUDE_NAMES or part.endswith(".pyc"):
                return None
        return ti

    for os_name, arch in [("Linux", "aarch64"), ("Linux", "x86_64")]:
        archive_name = f"sysutil-timeline-{os_name}-{arch}.tar.gz"
        output_file = tool_dst_dir / archive_name

        print(f"[sysutil-timeline] Creating {archive_name} …")

        with tarfile.open(output_file, "w:gz") as tf:
            for entry in sorted(source_dir.iterdir()):
                tf.add(entry, arcname=entry.name, filter=_filter)

        print(f"[sysutil-timeline] Package created: {output_file}")


# ------------------------------------------------------------------------------
# syscall-trace
# ------------------------------------------------------------------------------


def get_syscall_trace(tool_dst_base: Path) -> None:
    """
    Builds all syscall-trace variants locally and packages them.
    """
    source_dir = (
        SCRIPT_DIR.parent / "apap-cli" / "tools-builtin" / "syscall-trace" / "source"
    )
    source_file = source_dir / "main.go"
    if not source_file.exists():
        raise FileNotFoundError(f"main.go not found for syscall-trace at {source_file}")

    if not shutil.which("go"):
        raise RuntimeError("go is required to build the syscall-trace tool")

    version = _get_engine_version()
    tool_dst_dir = tool_dst_base / "syscall-trace" / version
    tool_dst_dir.mkdir(parents=True, exist_ok=True)

    variants = [
        ("Linux", "aarch64", "arm64"),
        ("Linux", "x86_64", "amd64"),
    ]

    for os_name, arch, goarch in variants:
        archive_name = f"syscall-trace-{os_name}-{arch}.tar.gz"
        output_file = tool_dst_dir / archive_name

        print(f"[syscall-trace] Creating {archive_name} …")

        with tempfile.TemporaryDirectory() as tmp_dir:
            binary = Path(tmp_dir) / "syscall-trace"
            env = os.environ.copy()
            env.update({"CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": goarch})
            subprocess.run(
                ["go", "build", "-o", str(binary), "."],
                cwd=source_dir,
                env=env,
                check=True,
            )

            binary.chmod(
                binary.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH
            )
            with tarfile.open(output_file, "w:gz") as tf:
                info = tf.gettarinfo(str(binary), arcname="syscall-trace")
                info.mode = 0o755
                with binary.open("rb") as fileobj:
                    tf.addfile(info, fileobj)

        print(f"[syscall-trace] Package created: {output_file}")


# ------------------------------------------------------------------------------
# instruction_mix
# ------------------------------------------------------------------------------


def get_instruction_mix(tool_dst_base: Path) -> None:
    _run_script([SCRIPT_DIR / "get-instruction-mix.sh", str(tool_dst_base)])


# ------------------------------------------------------------------------------
# cache_sharing
# ------------------------------------------------------------------------------


def get_cache_sharing(tool_dst_base: Path) -> None:
    """
    Returns the path to the script that retrieves the cache sharing Python
    tool bundle.
    """
    _run_script([SCRIPT_DIR / "get-cache-sharing.sh", str(tool_dst_base)])


# ------------------------------------------------------------------------------
# asct
# ------------------------------------------------------------------------------


def get_asct(tool_dst_base: Path) -> None:
    """
    Returns the path to the script that retrieves the ASCT Python tool bundle.
    """
    _run_script([SCRIPT_DIR / "get-asct.sh", str(tool_dst_base)])


# ------------------------------------------------------------------------------
# jitdump_jvm
# ------------------------------------------------------------------------------


def get_jitdump_jvm(tool_dst_base: Path) -> None:
    base = [
        sys.executable,
        SCRIPT_DIR / "get-github-tool.py",
        "jitdump-jvm",
        "--tools-dir",
        str(tool_dst_base),
        "--os",
        "linux",
    ]
    _run_script(
        base
        + [
            "--arch",
            "aarch64",
            "--third-party-licenses",
            "license_terms/third_party_licenses.txt",
        ]
    )
    _run_script(base + ["--arch", "x86_64"])


# ------------------------------------------------------------------------------
# dotnet_agent
# ------------------------------------------------------------------------------


def get_dotnet_agent(tool_dst_base: Path) -> None:
    base = [
        sys.executable,
        SCRIPT_DIR / "get-github-tool.py",
        "dotnet-agent",
        "--tools-dir",
        str(tool_dst_base),
        "--os",
        "linux",
    ]
    _run_script(
        base
        + [
            "--arch",
            "aarch64",
            "--third-party-licenses",
            "license_terms/third_party_licenses.txt",
        ]
    )
    _run_script(base + ["--arch", "x86_64"])


# ------------------------------------------------------------------------------
# wperf
# ------------------------------------------------------------------------------


def get_wperf(tool_dst_base: Path) -> None:
    _run_script([sys.executable, SCRIPT_DIR / "get-wperf.py", str(tool_dst_base)])


# ------------------------------------------------------------------------------
# topdown_tool
# ------------------------------------------------------------------------------


def get_topdown_tool(tool_dst_base: Path) -> None:
    _run_script(
        [
            sys.executable,
            SCRIPT_DIR / "get-wheel-tool-from-source.py",
            "topdown_tool",
            "--tools-dir",
            str(tool_dst_base),
        ]
    )


# ------------------------------------------------------------------------------
# wperf_cmn_visualizer
# ------------------------------------------------------------------------------


def get_wperf_cmn_visualizer(tool_dst_base: Path) -> None:
    _run_script(
        [
            sys.executable,
            SCRIPT_DIR / "get-wheel-tool-from-source.py",
            "wperf_cmn_visualizer",
            "--tools-dir",
            str(tool_dst_base),
        ]
    )


# ------------------------------------------------------------------------------
# cmn_tools
# ------------------------------------------------------------------------------


def get_cmn_tools(tool_dst_base: Path) -> None:
    _run_script(
        [
            sys.executable,
            SCRIPT_DIR / "get-wheel-tool-from-source.py",
            "cmn_tools",
            "--tools-dir",
            str(tool_dst_base),
        ]
    )


# ------------------------------------------------------------------------------
# CLI
# ------------------------------------------------------------------------------


def _dispatch(tool: str, tool_dst_base: Path, sl_analyze_host_base: Path) -> None:
    """
    Invokes the appropriate Python function to retrieve the specified tool.
    """
    dispatch: dict[str, object] = {
        "neoprof": lambda: get_neoprof(tool_dst_base, sl_analyze_host_base),
        "target_agent": lambda: get_target_agent(tool_dst_base),
        "instruction_mix": lambda: get_instruction_mix(tool_dst_base),
        "cache_sharing": lambda: get_cache_sharing(tool_dst_base),
        "asct": lambda: get_asct(tool_dst_base),
        "jitdump_jvm": lambda: get_jitdump_jvm(tool_dst_base),
        "dotnet_agent": lambda: get_dotnet_agent(tool_dst_base),
        "wperf": lambda: get_wperf(tool_dst_base),
        "sysutil-timeline": lambda: get_sysutil_timeline(tool_dst_base),
        "syscall-trace": lambda: get_syscall_trace(tool_dst_base),
        "topdown_tool": lambda: get_topdown_tool(tool_dst_base),
        "wperf_cmn_visualizer": lambda: get_wperf_cmn_visualizer(tool_dst_base),
        "cmn_tools": lambda: get_cmn_tools(tool_dst_base),
    }

    fn = dispatch.get(tool)
    if fn is None:
        raise ValueError(f"Unknown tool: {tool}")
    fn()  # type: ignore[operator]


def _prepare_release_tool_dirs(tool_dst_base: Path) -> None:
    for goos, goarch in RELEASE_TARGETS:
        release_dir = tool_dst_base.with_name(f"{tool_dst_base.name}-{goos}-{goarch}")
        shutil.rmtree(release_dir, ignore_errors=True)

        # Tools that match the suffixes are included for all platforms
        suffixes = {
            "-Android-aarch64.tar.gz",
            "-Linux-aarch64.tar.gz",
            "-Linux-x86_64.tar.gz",
            "-Windows-aarch64.tar.gz",
            f"-{goos.title()}-{RELEASE_ARCH_NAMES[goarch]}.tar.gz",
        }

        def ignore_incompatible_bundles(directory: str, names: list[str]) -> set[str]:
            if "license_terms" in Path(directory).parts:
                return set()
            return {
                name
                for name in names
                if name.endswith(".tar.gz")
                and not any(name.endswith(suffix) for suffix in suffixes)
            }

        shutil.copytree(
            tool_dst_base,
            release_dir,
            ignore=ignore_incompatible_bundles,
        )

        print(f"[get-tools] Prepared release tools: {release_dir}")


def _build_argument_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Retrieve the necessary tools and package them.",
        epilog=(
            "Examples:\n"
            "  %(prog)s\n"
            "  %(prog)s --pre-release\n"
            "  %(prog)s neoprof target_agent\n"
            "  %(prog)s --dest /custom/path neoprof wperf\n"
            "  %(prog)s --dest /custom/path\n\n"
            f"Standard tools:    {' '.join(STANDARD_TOOLS)}\n"
            f"Pre-release tools: {' '.join(PRE_RELEASE_TOOLS)}\n\n"
            "Requires: ARTIFACTORY_API_TOKEN env var"
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--dest",
        type=Path,
        default=SCRIPT_DIR.parent / "apap-cli" / "tools",
        help="destination path for tools (default: %(default)s)",
    )
    parser.add_argument(
        "--pre-release",
        action="store_true",
        help="include pre-release tools when no tool names are given",
    )
    parser.add_argument(
        "tools",
        nargs="*",
        metavar="TOOL",
        help="specific tool names to fetch",
    )
    return parser


def _parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    return _build_argument_parser().parse_args(argv)


def _resolve_tools(args: argparse.Namespace) -> list[str]:
    if args.tools:
        return list(args.tools)

    tools = list(STANDARD_TOOLS)
    if args.pre_release:
        tools += list(PRE_RELEASE_TOOLS)
    return tools


def main(argv: list[str] | None = None) -> None:
    args = _parse_args(argv)

    try:
        _artifactory_api_token()
        _check_unzip()
        _check_bash()
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)

    tool_dst_base = args.dest
    tools_to_get = _resolve_tools(args)
    sl_analyze_host_base = SCRIPT_DIR.parent / "apap-cli" / "sl-analyze-host-tools"

    # Validate all requested tools before starting any downloads
    known = set(STANDARD_TOOLS) | set(PRE_RELEASE_TOOLS)
    unknown = [t for t in tools_to_get if t not in known]

    if unknown:
        print(f"Unknown tool(s): {' '.join(unknown)}", file=sys.stderr)
        print(f"Standard tools:    {' '.join(STANDARD_TOOLS)}", file=sys.stderr)
        print(f"Pre-release tools: {' '.join(PRE_RELEASE_TOOLS)}", file=sys.stderr)
        sys.exit(1)

    print(
        f"[get-tools] Fetching {len(tools_to_get)} tool(s) in parallel: "
        f"{' '.join(tools_to_get)}"
    )
    print(f"[get-tools] Destination: {tool_dst_base}")

    # A thread pool is used to fetch the tools in parallel; one thread per tool
    # By default the thread (or worker) count is the number of cores available
    errors: list[str] = []
    pool = concurrent.futures.ThreadPoolExecutor()

    # Dispatch
    futures: dict[concurrent.futures.Future, str] = {}
    for tool in tools_to_get:
        future = pool.submit(_dispatch, tool, tool_dst_base, sl_analyze_host_base)
        futures[future] = tool

    # Wait
    for fut in concurrent.futures.as_completed(futures):
        try:
            fut.result()
        except Exception as exc:
            tool = futures[fut]
            errors.append(f"{tool}: {exc}")

    if errors:
        print("\n[get-tools] The following tools failed:", file=sys.stderr)
        for e in errors:
            print(e, file=sys.stderr)
        sys.exit(1)

    _prepare_release_tool_dirs(tool_dst_base)
    print("[get-tools] All done.")


if __name__ == "__main__":
    main()
