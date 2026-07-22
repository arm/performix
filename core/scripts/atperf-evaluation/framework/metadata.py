# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import json
import re
import os
import subprocess
from dataclasses import asdict, dataclass, field
from typing import Any, Dict, List

from framework.quality_level import QualityLevel, indeterminable_distribution
from framework.runner import run_cmd

@dataclass
class BenchmarkMetadata:
    """Metadata tracker for benchmark runs"""
    benchmark_name: str = ""
    benchmark_args: Dict[str, Any] = field(default_factory=dict)
    system: Dict[str, Any] = field(default_factory=dict)
    workloads: List[Dict[str, Any]] = field(default_factory=list)
    n_runs: int = 0
    data_quality_distribution: Dict[str, int] = field(default_factory=dict)
    errors: List[str] = field(default_factory=list)
    warnings: List[str] = field(default_factory=list)
    data: Dict[str, Any] = field(default_factory=dict)
    atperf_version: str = ""
    atperf_git_sha: str = ""
    atperf_git_ref: str = ""

    def add_error(self, message: str):
        """Add an error message"""
        self.errors.append(message)
        self.set_quality_distribution(indeterminable_distribution())

    def add_warning(self, message: str):
        """Add a warning message"""
        self.warnings.append(message)

    def add_data(self, key: str, value: Any):
        """Add arbitrary data"""
        self.data[key] = value

    def set_quality_distribution(self, dist: Dict[QualityLevel, float]):
        """Record full quality distribution, rounding values to nearest integer."""
        normalized: Dict[str, int] = {}
        for k, v in dist.items():
            key = str(k.value)
            try:
                normalized[key] = int(round(float(v)))
            except (ValueError, TypeError):
                normalized[key] = 0
        self.data_quality_distribution = normalized

    def set_atperf_version(self, atperf_path: str = "atperf", atperf_git_sha: str = "", atperf_git_ref: str = ""):
        """Add atperf version using the provided binary path and capture Git provenance."""
        try:
            self.atperf_version = run_cmd(f"{atperf_path} version").stdout.strip()
        except (FileNotFoundError, subprocess.CalledProcessError, OSError):
            # Fall back to bare command (for PATH-based runs)
            try:
                self.atperf_version = run_cmd("atperf version").stdout.strip()
            except (FileNotFoundError, subprocess.CalledProcessError, OSError):
                self.atperf_version = ""

        repo_dir = None
        try:
            if atperf_path and (os.path.isabs(atperf_path) or os.path.sep in atperf_path):
                candidate = os.path.dirname(os.path.abspath(atperf_path))
                if os.path.isdir(os.path.join(candidate, ".git")):
                    repo_dir = candidate
        except Exception:
            repo_dir = None

        # Also capture git provenance, preferring explicitly provided CLI values if present
        self.set_atperf_git_provenance(repo_dir=repo_dir, atperf_git_sha=atperf_git_sha, atperf_git_ref=atperf_git_ref)

    def set_atperf_git_provenance(self, repo_dir: str | None = None, atperf_git_sha: str = "", atperf_git_ref: str = ""):
        """Record Git provenance for the atperf binary build source.
        Priority:
          1) Values provided via CLI args (if provided)
          2) Provided repo_dir (git -C <repo_dir> ...)
          3) Current working directory
        """
        # 1) Prefer explicitly provided values
        if atperf_git_sha:
            self.atperf_git_sha = atperf_git_sha
        if atperf_git_ref:
            self.atperf_git_ref = atperf_git_ref
        if self.atperf_git_sha and self.atperf_git_ref:
            return

        # 2) Try provided repo_dir
        if repo_dir:
            try:
                if not self.atperf_git_sha:
                    self.atperf_git_sha = run_cmd(f'git -C "{repo_dir}" rev-parse HEAD').stdout.strip()
            except (FileNotFoundError, subprocess.CalledProcessError, OSError):
                pass
            try:
                if not self.atperf_git_ref:
                    self.atperf_git_ref = run_cmd(f'git -C "{repo_dir}" rev-parse --abbrev-ref HEAD').stdout.strip()
            except (FileNotFoundError, subprocess.CalledProcessError, OSError):
                pass
            if self.atperf_git_sha and self.atperf_git_ref:
                return

        # 3) Fall back to current working directory
        try:
            if not self.atperf_git_sha:
                self.atperf_git_sha = run_cmd("git rev-parse HEAD").stdout.strip()
        except (FileNotFoundError, subprocess.CalledProcessError, OSError):
            self.atperf_git_sha = "Unable to determine git_sha"
        try:
            if not self.atperf_git_ref:
                self.atperf_git_ref = run_cmd("git rev-parse --abbrev-ref HEAD").stdout.strip()
        except (FileNotFoundError, subprocess.CalledProcessError, OSError):
            self.atperf_git_ref = "Unable to determine git_ref"

    def collect_system_metadata(self):
        """Collect system and environment metadata"""
        import platform
        import time

        try :
            perf_version = _extract_version_from_output(run_cmd("perf --version").stdout.strip())
        except Exception as e:
            perf_version = "unknown"

        self.system = {
            "run.timestamp_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "target.perf.version": perf_version,
            "target.python.version": platform.python_version(),
            "target.system.os": platform.system(),
            "target.system.release": platform.release(),
            "target.system.version": platform.version(),
            "target.system.arch": platform.machine()
        }

    def add_workloads_metadata(self, workloads):
        """Add workload metadata"""
        import os
        import shlex

        self.workloads = []
        for idx, w in enumerate(workloads or []):
            parts = shlex.split(w)
            exe = parts[0] if parts else w
            exe_name = os.path.basename(exe)
            self.workloads.append({
                "index": idx,
                "command": w,
                "executable": exe,
                "name": exe_name
            })

    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for JSON serialization"""
        result = asdict(self)
        result['data_quality_distribution'] = self.data_quality_distribution
        return result

    def to_json(self, indent: int = 2) -> str:
        """Convert to JSON string"""
        return json.dumps(self.to_dict(), indent=indent, default=str)

    def save_to_file(self, filepath: str):
        """Save metadata to JSON file"""
        with open(filepath, 'w') as f:
            f.write(self.to_json())

def _extract_version_from_output(out: str):
    first_line = out.splitlines()[0] if out else ""
    m = re.search(r'(\d+\.\d+(\.\d+)*)', first_line)
    return m.group(1) if m else first_line[:80]