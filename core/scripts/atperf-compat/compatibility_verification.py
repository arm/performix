#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Script: compatibility_verification.py

Validate ATP’s backward‐compatibility by:
1. Importing historical runs.
2. Rendering each run with the engine version currently being released 
3. Comparing actual result (SUCCESS vs. error) against
    the expected outcome derived from the compatibility matrix.
"""
import argparse
import json
import os
import subprocess
import sys
import logging
from pathlib import Path
sys.path.append(os.path.join(os.path.dirname(__file__), ".."))
from terminology import terminology


# Logging setup
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)-8s %(message)s"
)
logger = logging.getLogger("verify_migration")

def to_semver(s: str) -> tuple:
    try:
        sv = tuple(map(int, s.split(".")))
        if len(sv) == 3:
            return sv
        logger.error("Expected %s to have 3 numeric segments; got %s", s, len(sv))
        sys.exit(1)
    except ValueError as e:
        logger.error("Cannot parse %s as a semver: %s", s, e)
        sys.exit(1)

def get_current_version(atperf: Path) -> str:
    try:
        p = subprocess.run([str(atperf), "version"],
                           capture_output=True, text=True, check=True)
    except subprocess.CalledProcessError as e:
        logger.error("Failed to run '%s version' (exit %s)", atperf, e.returncode)
        if e.stdout:
            logger.error("atperf version stdout:\n%s", e.stdout.rstrip())
        if e.stderr:
            logger.error("atperf version stderr:\n%s", e.stderr.rstrip())
        raise
    for line in p.stdout.splitlines():
        if "version:" in line.lower():
            ver = line.strip().split()[-1]
            logger.info("Current engine version: %s", ver)
            return ver
    raise RuntimeError("Failed to parse engine version")

def load_matrix(path: Path) -> dict:
    try:
        text = path.read_text()
    except Exception as e:
        logger.error("Could not read compatibility matrix %s: %s", path, e)
        sys.exit(1)
    try:
        data = json.loads(text)
        return data["compatibility"]
    except (json.JSONDecodeError, KeyError) as e:
        logger.error("Invalid matrix format in %s: %s", path, e)
        sys.exit(1)

def load_manifest(path: Path) -> list:
    try:
        text = path.read_text()
    except Exception as e:
        logger.error("Could not read manifest %s: %s", path, e)
        sys.exit(1)
    try:
        return json.loads(text)
    except json.JSONDecodeError as e:
        logger.error("Invalid JSON in manifest %s: %s", path, e)
        sys.exit(1)

def load_migrations(path: Path) -> list:
    try:
        text = path.read_text()
    except Exception as e:
        logger.error("Could not read migrations %s: %s", path, e)
        sys.exit(1)
    try:
        return json.loads(text)
    except json.JSONDecodeError as e:
        logger.error("Invalid JSON in migrations %s: %s", path, e)
        sys.exit(1)

def extract_render_session_id(output: str) -> str:
    try:
        return json.loads(output)["data"]["invocation"]["session_id"]
    except (json.JSONDecodeError, KeyError, TypeError):
        return ""


def close_render_session(atperf: Path, session_id: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(atperf), "render", "close", session_id],
        capture_output=True,
        text=True,
    )


def migrate_recipe(migrations: list, recipe: str, version: str) -> str:
    """
    Function to migrate an old recipe name to its current name
    """
    version_sem_ver = to_semver(version)
    migrated_name = recipe

    # loop through migrations in chronological order, applying one at a time
    for m in migrations:
        if version_sem_ver >= to_semver(m["version"]):
            continue
        if migrated_name == m["from"]:
            migrated_name = m["to"]

    if migrated_name != recipe:
        msg = f"Migrated recipe name {recipe} to {migrated_name}"
        logger.info(msg)

    return migrated_name

def main():
    p = argparse.ArgumentParser()
    p.add_argument("--historical", required=True, type=Path,
                   help="Root of historical_runs/")
    p.add_argument("--matrix",     required=True, type=Path,
                   help="Path to the compatibility matrix (e.g. atperf-compatibility/compatibility/matrix.json)")
    p.add_argument("--migrations", required=True, type=Path,
                   help="Path to the recipe name migrations list (e.g. apap-engine/run/recipemigration/migrations.json)")
    p.add_argument("--atperf-bin", type=Path, default=Path(f"apap-cli/{terminology.get_product_binary_name()}"),
                   help="Path to atperf executable")
    p.add_argument("--dry-run",    action="store_true",
                   help="Log commands without actually running them")
    args = p.parse_args()

    # Resolve paths relative to repo root if needed
    repo_root = Path(__file__).resolve().parent.parent.parent
    hist_dir = args.historical if args.historical.is_absolute() else (repo_root / args.historical)
    matrix_fp = args.matrix if args.matrix.is_absolute() else (repo_root / args.matrix)
    migrations_fp = args.migrations if args.migrations.is_absolute() else (repo_root / args.migrations)
    atperf_fp = args.atperf_bin if args.atperf_bin.is_absolute() else (repo_root / args.atperf_bin)

    # Resolve to absolute paths for consistent logging/usage
    hist_dir = hist_dir.resolve(strict=False)
    matrix_fp = matrix_fp.resolve(strict=False)
    migrations_fp = migrations_fp.resolve(strict=False)
    atperf_fp = atperf_fp.resolve(strict=False)

    # Validate paths
    if not hist_dir.exists():
        logger.error("Historical runs directory not found: %s", hist_dir)
        sys.exit(1)
    if not matrix_fp.exists():
        logger.error("Compatibility matrix not found: %s", matrix_fp)
        sys.exit(1)
    if not migrations_fp.exists():
        logger.error("Recipe name migrations list not found: %s", migrations_fp)
        sys.exit(1)
    if not atperf_fp.exists():
        logger.error("atperf binary not found: %s", atperf_fp)
        sys.exit(1)

    current_ver = get_current_version(atperf_fp)
    matrix = load_matrix(matrix_fp)
    migrations = load_migrations(migrations_fp)
    migrations.sort(key=lambda x: to_semver(x["version"]))
    failures = []

    # Walk each generated-version directory
    for version_dir in sorted(hist_dir.iterdir()):
        if not version_dir.is_dir():
            continue
        gen_ver     = version_dir.name
        manifest_fp = version_dir / "manifest.json"
        if not manifest_fp.exists():
            logger.warning("Skipping %s: no manifest.json", gen_ver)
            continue

        manifest = load_manifest(manifest_fp)
        logger.info("Validating %d runs generated by %s", len(manifest), gen_ver)

        for rec in manifest:
            recipe = rec["recipe"]
            run_id = rec["run_id"]
            zip_fp = version_dir / rec["zip_path"]

            # migrate recipe name if necessary
            migrated_recipe = migrate_recipe(migrations, recipe, gen_ver)

            # lookup matrix entry
            recipe_map = matrix.get(migrated_recipe, {})
            entry = recipe_map.get(current_ver) or recipe_map.get("NEXT_VERSION")
            if not entry:
                msg = f"No matrix entry for {migrated_recipe}@{current_ver}"
                logger.error(msg)
                failures.append({
                    "version": gen_ver,
                    "recipe":  recipe,
                    "run_id":  run_id,
                    "error":   msg
                })
                continue

            min_supported = entry["minimum_version"]
            expect_ok     = to_semver(gen_ver) >= to_semver(min_supported)
            logger.debug(
                "Run %s: gen_ver=%s min_supported=%s expect_success=%s",
                run_id, gen_ver, min_supported, expect_ok
            )

            cmd_import = [str(atperf_fp), "run", "import", str(zip_fp)]
            cmd_render = [str(atperf_fp), "run", "render", run_id, "--json"]

            if args.dry_run:
                logger.info("[DRY] %s", " ".join(cmd_import))
                logger.info("[DRY] %s", " ".join(cmd_render))
                continue

            # import
            logger.debug("Importing run %s", run_id)
            try:
                subprocess.run(cmd_import, check=True, capture_output=True, text=True)
            except subprocess.CalledProcessError as e:
                stderr = (e.stderr or "").strip()
                stdout = (e.stdout or "").strip()
                logger.error(
                    "Import failed for %s: stderr=%s stdout=%s",
                    run_id,
                    stderr or "<no stderr>",
                    stdout or "<no stdout>",
                )
                failures.append({
                    "version": gen_ver,
                    "recipe":  recipe,
                    "run_id":  run_id,
                    "step":    "import",
                    "stderr":  stderr or "<no stderr>",
                    "stdout":  stdout or "<no stdout>",
                })
                continue

            # render
            logger.info("Rendering recipe=%s run_id=%s (gen_ver=%s)", recipe, run_id, gen_ver)
            logger.debug("Command: %s", " ".join(cmd_render))
            res = subprocess.run(cmd_render, capture_output=True, text=True)
            actual_ok = (res.returncode == 0)
            stderr    = res.stderr.strip()
            stdout    = res.stdout.strip()
            session_id = extract_render_session_id(stdout)

            if actual_ok:
                logger.info("Run %s render SUCCESS", run_id)
            else:
                logger.info(
                    "Run %s render FAILURE: stderr=%s stdout=%s",
                    run_id,
                    stderr or "<no stderr>",
                    stdout or "<no stdout>",
                )

            if session_id:
                close_res = close_render_session(atperf_fp, session_id)
                if close_res.returncode != 0:
                    close_stderr = (close_res.stderr or "").strip()
                    close_stdout = (close_res.stdout or "").strip()
                    logger.warning(
                        "Failed to close render session %s for run %s: stderr=%s stdout=%s",
                        session_id,
                        run_id,
                        close_stderr or "<no stderr>",
                        close_stdout or "<no stdout>",
                    )
            else:
                logger.warning(
                    "No render session id found for run %s; skipping render session cleanup",
                    run_id,
                )

            # compare
            if actual_ok != expect_ok:
                logger.error(
                    "Mismatch: recipe=%s run_id=%s gen_ver=%s expected=%s actual=%s min_supported=%s",
                    recipe, run_id, gen_ver,
                    "SUCCESS" if expect_ok else "INCOMPATIBLE",
                    "SUCCESS" if actual_ok else "FAILURE",
                    min_supported
                )
                failures.append({
                    "version":         gen_ver,
                    "recipe":          recipe,
                    "run_id":          run_id,
                    "minimum_version": min_supported,
                    "expected":        "SUCCESS" if expect_ok else "INCOMPATIBLE",
                    "actual":          "SUCCESS" if actual_ok else "FAILURE",
                    "stderr":          stderr or "<no stderr>",
                    "stdout":          stdout or "<no stdout>",
                })

    # summary & exit
    if failures:
        logger.error("Validation completed with %d failures", len(failures))
        print(json.dumps({"failures": failures}, indent=2))
        sys.exit(1)

    logger.info("All historical runs validated successfully.")
    sys.exit(0)

if __name__ == "__main__":
    main()
