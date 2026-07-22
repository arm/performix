#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

'''
Python script to prepare the top-level license_terms/ directory in this repo
for release. The script does the following:

TPIP: Third Party Intellectual Property

- Move each tool's license_terms/ into their respective top-level license_terms/
- Prepend each Arm-provided tool's entry into third_party_licenses.json.
- Replace all symlinks under license_terms/ with their hard copies, to ensure
    GoReleaser can reliably archive them.

This script produces the final release-shaped license_terms tree directly.
'''

# IMPORTANT: We rely on ./scripts/tpip/tools-metadata.json to retrieve
# metadata about each Arm-provided tool; like name, homepage and license
# type. Because, currently, they don't report those informations and we have
# no way of reliably retrieving them. TODO: We need to come up with a better
# solution once we have the time / agreement between teams.
#
# The format of this file is a simple dict of tool_name -> {metadata}
# The tool_name must suffix match what's used under apap-cli/tools
# e.g. instruction_mix-0.4.4-py3-none-any.whl, the tool_name can simply be instruction_mix
TOOL_METADATA_FILE = "scripts/tpip/tools-metadata.json"
LICENSE_TEXTS_DIR = "scripts/tpip/license-texts"

# Top-level TPIP paths
LICENSE_TERMS_DIR = "license_terms"
TPIP_FILE = f"{LICENSE_TERMS_DIR}/third_party_licenses.json"

# Per-tool TPIP paths
TOOLS_DIR = "apap-cli/tools"
TPIP_PER_TOOL_DIR = f"{LICENSE_TERMS_DIR}/third_party_licenses"

# Blackduck reformat script
BLACKDUCK_REFORMAT_SCRIPT = "scripts/tpip/reformat-blackduck.py"

import os
import json
import shutil
import subprocess
import sys
from pathlib import Path
from terminology import terminology

def log(message: str) -> None:
    print(f"[prepare-license-terms] {message}")


def copy_dir_contents(src_dir: Path, dest_dir: Path) -> None:
    '''
    Copy the contents of src_dir into dest_dir, creating dest_dir if it doesn't exist.

    :param src_dir: Source directory
    :type src_dir: Path
    :param dest_dir: Destination directory
    :type dest_dir: Path
    '''
    for path in src_dir.rglob("*"):
        rel_path = path.relative_to(src_dir)
        target_path = dest_dir / rel_path
        if path.is_symlink():
            target = path.resolve(strict=False)
            if not target.exists():
                raise RuntimeError(f"Broken symlink: {path} -> {path.readlink()}")
            if target.is_dir():
                target_path.mkdir(parents=True, exist_ok=True)
                copy_dir_contents(target, target_path)
            else:
                target_path.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(target, target_path)
            continue
        if path.is_dir():
            target_path.mkdir(parents=True, exist_ok=True)
            continue
        target_path.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target_path)

def sync_tool_license_terms(root_dir: Path) -> None:
    '''
    Copy TPIP from each tool's license_terms directory into a single
    per-tool directory under license_terms/third_party_licenses/<tool_name>.

    :param root_dir: Root directory of the repository
    :type root_dir: Path
    '''
    tools_dir = root_dir / TOOLS_DIR
    top_level_terms_dir = root_dir / LICENSE_TERMS_DIR
    per_tool_terms_dir = root_dir / TPIP_PER_TOOL_DIR

    log(f"Tools directory: {tools_dir}")
    log(f"Top-level license terms directory: {top_level_terms_dir}")
    log(f"Per-tool license terms directory: {per_tool_terms_dir}")

    if not tools_dir.is_dir():
        log(f"Warning: tools directory not found: {tools_dir}. Skipping per-tool license aggregation.")
        return

    if per_tool_terms_dir.exists():
        log(f"Removing existing per-tool license terms: {per_tool_terms_dir}")
        shutil.rmtree(per_tool_terms_dir)
    per_tool_terms_dir.mkdir(parents=True, exist_ok=True)

    for tool_dir in sorted(p for p in tools_dir.iterdir() if p.is_dir()):
        version_dirs = [p for p in tool_dir.iterdir() if p.is_dir()]
        for version_dir in sorted(version_dirs):
            license_dir = version_dir / LICENSE_TERMS_DIR
            if not license_dir.is_dir():
                log(
                    "Warning: missing license_terms for tool "
                    f"'{tool_dir.name}' version '{version_dir.name}' at {license_dir}"
                )

    license_dirs = list(tools_dir.rglob(LICENSE_TERMS_DIR))
    if not license_dirs:
        log(f"Warning: no tool license_terms directories found under {tools_dir}")
        return

    for license_dir in license_dirs:
        rel_path = license_dir.relative_to(tools_dir)
        tool_name = rel_path.parts[0]
        dest_dir = per_tool_terms_dir / tool_name
        log(f"Collecting license_terms for tool '{tool_name}' from {license_dir}")
        copy_dir_contents(license_dir, dest_dir)

    log("Done copying per-tool license_terms")


def collect_tool_versions(tools_dir: Path) -> dict[str, str]:
    '''
    Build a dict mapping tool_name -> version from apap-cli/tools.
    All tools with at least one version directory are included.

    :param tools_dir: Directory containing tool bundles
    :type tools_dir: Path
    '''
    if not tools_dir.is_dir():
        log(f"Warning: tools directory not found: {tools_dir}. Skipping tool version collection.")
        return {}

    tool_versions: dict[str, str] = {}
    for tool_dir in sorted(p for p in tools_dir.iterdir() if p.is_dir()):
        version_dirs = sorted(p for p in tool_dir.iterdir() if p.is_dir())
        if not version_dirs:
            continue

        version_dir = version_dirs[-1]
        if len(version_dirs) > 1:
            log(
                f"Warning: multiple versions found for tool '{tool_dir.name}'. "
                f"Using latest sorted version '{version_dir.name}'."
            )

        if not (version_dir / LICENSE_TERMS_DIR).is_dir():
            log(
                f"Warning: selected version for tool '{tool_dir.name}' has no license_terms at "
                f"{version_dir / LICENSE_TERMS_DIR}"
            )

        tool_versions[tool_dir.name] = version_dir.name

    return tool_versions


def load_license_text(root_dir: Path, license_name: str) -> str:
    '''
    Load a license text from scripts/tpip/license-texts/<license>.txt,
    matching the filename case-insensitively.
    '''
    license_texts_dir = root_dir / LICENSE_TEXTS_DIR
    expected_filename = f"{license_name}.txt".lower()

    if not license_texts_dir.is_dir():
        raise RuntimeError(f"License texts directory not found: {license_texts_dir}")

    for path in license_texts_dir.iterdir():
        if path.is_file() and path.name.lower() == expected_filename:
            return path.read_text(encoding="utf-8")

    raise RuntimeError(f"License text not found for '{license_name}' in {license_texts_dir}")


def prepend_tools(root_dir: Path, tool_versions: dict[str, str]) -> None:
    '''
    Prepend Arm-provided tool entries into the top-level TPIP JSON file.

    For each tool the following sections of the BlackDuck JSON are updated:
      - componentLicenses: a new entry is prepended.
      - componentCopyrightTexts: a new entry is prepended.
      - licenseTexts: if the tool's license already has an entry, the tool is
        added to its component list.

    If the top-level TPIP JSON doesn't exist it highly likely means this is not
    a release build and we don't need to do aggregation.

    :param root_dir: Root directory of the repository
    :type root_dir: Path
    :param tool_versions: Mapping of tool_name -> version from sync_tool_license_terms
    :type tool_versions: dict[str, str]
    '''
    tpip_file = root_dir / TPIP_FILE
    metadata_path = root_dir / TOOL_METADATA_FILE

    if not tpip_file.is_file():
        log(f"Warning: TPIP file not found at {tpip_file}. Skipping tool injection.")
        return
    if not metadata_path.is_file():
        raise RuntimeError(f"Tool metadata file not found at {metadata_path}")

    with open(metadata_path, "r", encoding="utf-8") as f:
        tool_metadata = json.load(f)

    with open(tpip_file, "r", encoding="utf-8") as f:
        blackduck_json = json.load(f)


    component_licenses: list = []
    copyright_texts: list = []
    license_texts: list = blackduck_json.get("licenseTexts", [])

    injected = 0
    for tool_name, version in sorted(tool_versions.items()):
        # find the tool_name in the metadata by doing a 'startswith' match.
        # The reason is that the instruction_mix tool's directory name is
        # 'instruction_mix-0.4.4-py3-none-any.whl', but in the metadata we
        # store it as 'instruction_mix' to avoid maintaining it every version.
        metadata_entry = tool_metadata.get(tool_name) or next(
            (value for key, value in tool_metadata.items() if tool_name.startswith(key)),
            None,
        )
        if not metadata_entry:
            log(f"Warning: No metadata found for tool '{tool_name}'. Skipping injection.")
            continue

        licenses = metadata_entry.get("licenses", [])
        copyrights = metadata_entry.get("copyrights", "")
        homepage = metadata_entry.get("homepage", "")

        full_name = f"{tool_name} {version}".strip()

        # 1. Collect componentLicenses entry
        component_entry: dict = {
            "component": {
                "projectName": tool_name,
                "versionName": version,
            },
            "licenses": [{"name": license_name} for license_name in licenses],
        }
        if homepage:
            component_entry["component"]["homeUrl"] = homepage
        component_licenses.append(component_entry)

        # 2. Collect componentCopyrightTexts entry
        copyright_summary: dict = {
            "projectName": tool_name,
            "versionName": version,
        }
        if homepage:
            copyright_summary["homeUrl"] = homepage
        copyright_entry: dict = {
            "componentVersionSummary": copyright_summary,
            "copyrightTexts": [copyrights] if copyrights else [],
        }
        copyright_texts.append(copyright_entry)

        # 3. Add tool to the matching licenseTexts entry's component list
        for license_name in licenses:
            license_entry = next(
                (entry for entry in license_texts if entry.get("name") == license_name),
                None,
            )
            if license_entry is None:
                license_entry = {
                    "name": license_name,
                    "text": load_license_text(root_dir, license_name),
                    "components": [],
                    "modified": False,
                }
                license_texts.append(license_entry)
                log(f"Added licenseTexts entry for '{license_name}' from {root_dir / LICENSE_TEXTS_DIR}")
            license_entry.setdefault("components", []).append({
                "projectName": tool_name,
                "versionName": version,
            })
            log(f"Updated licenseTexts component list for '{license_name}' with: {full_name}")

        log(f"Prepared BD JSON injection for tool '{full_name}' ({', '.join(licenses) if licenses else 'no licenses'})")
        injected += 1

    if not injected:
        log("No tool entries to inject into the TPIP file.")
        return

    blackduck_json["componentLicenses"] = component_licenses + blackduck_json.get("componentLicenses", [])
    blackduck_json["componentCopyrightTexts"] = copyright_texts + blackduck_json.get("componentCopyrightTexts", [])
    blackduck_json["licenseTexts"] = license_texts

    with open(tpip_file, "w", encoding="utf-8") as f:
        json.dump(blackduck_json, f, indent=2, ensure_ascii=False)
        f.write("\n")

    log(f"Successfully injected {injected} tool entries into {tpip_file}")


def ensure_symlinks_dereferenced(source_dir: Path) -> None:
    '''
    Find all symlinks under source_dir and replace them with hard copies of their targets.
    Essentially eliminating all symlinks so that goreleaser can include them reliably across platforms.
    
    :param source_dir: Source directory to search for symlinks
    :type source_dir: Path
    '''
    symlinks = [path for path in source_dir.rglob("*") if path.is_symlink()]
    if not symlinks:
        log("No symlinks found under license_terms")
        return

    for symlink in symlinks:
        # Avoid dirty git state in CI after dereferencing symlinks
        if os.environ.get("GITHUB_ACTIONS", "").lower() == "true":
            subprocess.run(
                ["git", "update-index", "--assume-unchanged", str(symlink)],
                check=False,
            )
        
        target = symlink.resolve(strict=False)
        if not target.exists():
            raise RuntimeError(f"Broken symlink: {symlink} -> {symlink.readlink()}")

        if target.is_dir():
            log(f"Dereferencing directory symlink: {symlink} -> {target}")
            symlink.unlink()
            symlink.mkdir(parents=True, exist_ok=True)
            copy_dir_contents(target, symlink)
        else:
            log(f"Dereferencing file symlink: {symlink} -> {target}")
            symlink.unlink()
            symlink.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(target, symlink)

def reformat_blackduck_tpip(
    root_dir: Path,
    input_path: Path,
    output_path: Path,
    remove_after: bool = False,
) -> None:
    '''
    Run the Black Duck reformatter used by release packaging.

    :param root_dir: Root directory of the repository
    :type root_dir: Path
    :param input_path: Black Duck notices JSON input
    :type input_path: Path
    :param output_path: Reformatted TPIP text output
    :type output_path: Path
    :param remove_after: Delete the input JSON after successful reformatting
    :type remove_after: bool
    '''
    reformat_script = root_dir / BLACKDUCK_REFORMAT_SCRIPT
    if not reformat_script.is_file():
        raise RuntimeError(f"Blackduck reformat script not found at {reformat_script}")

    cmd = [
        sys.executable,
        str(reformat_script),
        "--input",
        str(input_path),
        "--output",
        str(output_path),
    ]
    if remove_after:
        cmd.append("--remove-after")

    result = subprocess.run(cmd, check=False)

    if result.returncode != 0:
        raise RuntimeError(f"Blackduck reformat script failed: {result.returncode}")


def save_cli_tpip(root_dir: Path) -> None:
    '''
    Saves the CLI's TPIP file (without aggregated tool entries) into
    license_terms/third_party_licenses/ for release.
    Must be called before prepend_tools() so that we can have CLI-only TPIP.

    :param root_dir: Root directory of the repository
    :type root_dir: Path
    '''
    tpip_json = root_dir / TPIP_FILE
    if not tpip_json.is_file():
        log(f"Warning: TPIP JSON not found at {tpip_json}. Skipping CLI TPIP save.")
        return

    full_product_name_cli = terminology.get_product_full_name() + " CLI"
    folder_name = full_product_name_cli.lower().replace(" ", "-")

    dest_dir = root_dir / TPIP_PER_TOOL_DIR / folder_name
    dest_dir.mkdir(parents=True, exist_ok=True)
    dest_file = dest_dir / "third_party_licenses.txt"

    reformat_blackduck_tpip(root_dir, tpip_json, dest_file)
    log(f"Saved CLI TPIP to {dest_file}")


def save_aggregate_tpip(root_dir: Path) -> None:
    '''
    Saves the final aggregate TPIP file and removes the intermediate Black Duck
    JSON so the resulting license_terms tree matches release artifacts.

    :param root_dir: Root directory of the repository
    :type root_dir: Path
    '''
    tpip_json = root_dir / TPIP_FILE
    if not tpip_json.is_file():
        log(f"Warning: TPIP JSON not found at {tpip_json}. Skipping aggregate TPIP save.")
        return

    dest_file = root_dir / LICENSE_TERMS_DIR / "third_party_licenses.txt"
    reformat_blackduck_tpip(root_dir, tpip_json, dest_file, remove_after=True)
    log(f"Saved aggregate TPIP to {dest_file}")

def main() -> None:
    script_dir = Path(__file__).resolve().parent
    root_dir = script_dir.parent
    source_dir = root_dir / "license_terms"
    log(f"Preparing release license terms from {source_dir}")

    if not source_dir.is_dir():
        raise RuntimeError(f"License terms directory not found: {source_dir}")

    sync_tool_license_terms(root_dir)
    tool_versions = collect_tool_versions(root_dir / TOOLS_DIR)
    ensure_symlinks_dereferenced(source_dir)
    save_cli_tpip(root_dir)
    prepend_tools(root_dir, tool_versions)
    save_aggregate_tpip(root_dir)

    log(f"Prepared license terms at {source_dir}")


if __name__ == "__main__":
    try:
        main()
        log("Success")
    except Exception as exc:  # pragma: no cover - defensive top-level guard
        print(f"Error: {exc}", file=sys.stderr)
        sys.exit(1)
