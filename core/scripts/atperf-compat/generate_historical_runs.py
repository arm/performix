#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

"""
Script: generate_historical_runs.py

Description:
  Reads a JSON array of {uid, cmd_args}, runs each recipe
  against a target via `recipe run`, exports the resulting run
  to <run_id>.zip, and stores it in:

    <output>/<engine_version>/<target>/<uid>/<run_id>.zip

  It then writes a single manifest at:

    <output>/<engine_version>/manifest.json

  listing each run’s recipe, run_id, and relative zip path.  This
  manifest drives the downstream compatibility validator.

Usage (from repo root):
  python3 scripts/atperf-compat/generate_historical_runs.py \
    --config scripts/atperf-compat/historical_runs_config.json \
    [--cli-bin /path/to/apx] \
    [--target <machine>] \
    [--workload <override>] \
    [--output <outdir>] \
    [--dry-run]
"""
import argparse
import json
import logging
import re
import sys
from pathlib import Path
from subprocess import CompletedProcess
import os

sys.path.append(os.path.join(os.path.dirname(__file__), ".."))
from terminology import terminology
import run_export_helper

product_binary_name = terminology.get_product_binary_name()

# Logging setup
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)-8s %(name)s: %(message)s"
)
logger = logging.getLogger("historical_runs")

# Helpers
def load_config(path: Path) -> list[dict]:
    if not path.is_file():
        logger.error("Config not found: %s", path)
        sys.exit(1)
    data = json.loads(path.read_text())
    if not isinstance(data, list):
        logger.error("Config must be a JSON array")
        sys.exit(1)
    return data

def run_cmd(cmd: list[str], cwd: Path) -> CompletedProcess:
    try:
        return run_export_helper.run_cli(cmd, cwd)
    except run_export_helper.CommandFailure as error:
        p = error.process
        logger.error("Failed [%s]: %s", " ".join(cmd), p.stderr.strip())
        logger.error("  STDOUT: %s", p.stdout.strip() or "<no stdout>")
        logger.error("  STDERR: %s", p.stderr.strip() or "<no stderr>")
        sys.exit(p.returncode)


def find_cli_bin(repo: Path, custom: str | None) -> Path:
    exe = Path(custom).expanduser() if custom else repo / "apap-cli" / product_binary_name
    if not exe.exists():
        logger.error("%s binary not found: %s", product_binary_name, exe)
        sys.exit(1)
    return exe.resolve()

def parse_run_id(output: str) -> str:
    try:
        return run_export_helper.parse_recipe_run_id(output)
    except ValueError:
        logger.error("Could not parse Run ID")
        logger.debug("Output was:\n%s", output)
        sys.exit(1)

def get_engine_version(cli_bin: Path) -> str:
    p = run_cmd([str(cli_bin), "version"], cli_bin.parent)
    m = re.search(r"CLI version:\s*([0-9]+\.[0-9]+\.[0-9]+)", p.stdout)
    if not m:
        logger.error("Version parse failed:\n%s", p.stdout)
        sys.exit(1)
    return m.group(1)

# Core
def process_entry(
    entry: dict,
    cli_bin: Path,
    version: str,
    target: str | None,
    output: Path,
    workload_override: str | None,
    dry: bool,
    manifest: list
):
    uid    = entry.get("uid")
    args   = entry.get("cmd_args")
    if not args or not isinstance(args, list):
        logger.warning("Skipping invalid entry: %s", entry)
        return

    # derive recipe name from the first cmd arg
    recipe = args[0]

    # override workload if requested
    if workload_override:
        orig = next((a.split("=",1)[1] for a in args if a.startswith("--workload=")), "<none>")

        # rebuild args so that "--workload" and its value are separate tokens
        new_args: list[str] = []
        for token in args:
            if token.startswith("--workload="):
                new_args.append("--workload")
                new_args.append(workload_override)
            else:
                new_args.append(token)
                args = new_args

        logger.info("Overriding workload for uid='%s' from %s to %s", uid, orig, workload_override)

    # build command
    cmd = [str(cli_bin), "recipe", "run"]
    cmd += args

    # always inject --deploy-tools after the recipe name (4th token)
    # cmd = [apx, "recipe", "run", "<recipe>", ...]
    cmd.insert(4, "--deploy-tools")

    if target:
        cmd += ["--target", target]

    # log & dry-run return
    logger.info("EXEC (dry=%s): %s", dry, " ".join(cmd))
    if dry:
        return

    # 1) run and get run_id
    p = run_cmd(cmd, cli_bin.parent)
    run_id = parse_run_id(p.stdout + p.stderr)
    logger.info("%s produced Run ID %s", uid, run_id)

    # 2) export zip
    zip_name = f"{run_id}.zip"
    run_cmd([
        str(cli_bin), "run", "export", run_id,
        str(cli_bin.parent.parent)
    ], cli_bin.parent.parent)
    zip_p = cli_bin.parent.parent / zip_name
    if not zip_p.exists():
        logger.error("Missing %s", zip_name)
        sys.exit(1)

    # 3) move and write config
    tgt = target or "default"
    dest = output / version / tgt / uid
    dest.mkdir(parents=True, exist_ok=True)
    zip_p.replace(dest / zip_name)
    cfg = {"uid":uid, "recipe":recipe, "run_id":run_id}
    (dest / "config.json").write_text(json.dumps(cfg, indent=2))
    logger.info("Stored %s at %s", uid, dest)

    # add to manifest
    manifest.append({
        "recipe": recipe,
        "run_id": run_id,
        "zip_path": f"{tgt}/{uid}/{run_id}.zip"
    })

# Entrypoint
def main():
    p = argparse.ArgumentParser()
    p.add_argument("--config",     required=True, type=Path)
    p.add_argument("--cli-bin",    help=f"custom {product_binary_name} path")
    p.add_argument("--target",     help=f"optional {product_binary_name} target")
    p.add_argument("--workload",   help="override workload")
    p.add_argument("--output",     type=Path, default=Path("historical_runs"))
    p.add_argument("--dry-run",    action="store_true")
    args = p.parse_args()
    if args.dry_run:
        logger.setLevel(logging.DEBUG)

    repo    = Path(__file__).resolve().parent.parent.parent
    cli_bin  = find_cli_bin(repo, args.cli_bin)
    version = get_engine_version(cli_bin)
    entries = load_config(args.config)

    manifest: list = []
    for e in entries:
        process_entry(e, cli_bin, version,
                      args.target, args.output,
                      args.workload, args.dry_run,
                      manifest)

    # write per-version manifest.json
    if not args.dry_run:
        mf = args.output / version / "manifest.json"
        mf.parent.mkdir(parents=True, exist_ok=True)
        mf.write_text(json.dumps(manifest, indent=2))
        logger.info("Wrote manifest: %s", mf)

if __name__ == "__main__":
    main()
