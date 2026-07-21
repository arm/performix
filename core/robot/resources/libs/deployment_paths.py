# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import shlex
from typing import Callable, Tuple


def resolve_deployment_dir_for_user_posix(run_fn: Callable[[str], Tuple[int, str]], username: str, product_bin_name: str) -> str:
    """
    Resolve the <product_bin_name> deployment directory for a specific user on POSIX targets.
    Mirrors ResolveToolsBaseDir in apap-engine/conductor/target_path_resolver.go.
    """
    rc, output = run_fn(f"getent passwd {shlex.quote(username)}")
    if rc == 0 and output:
        fields = output.strip().split(":")
        if len(fields) >= 6:
            home = fields[5].strip()
            if home:
                test_rc, _ = run_fn(f"sudo -u {shlex.quote(username)} test -w {shlex.quote(home)}")
                if test_rc == 0:
                    return f"{home}/.local/share/{product_bin_name}"
    return f"/tmp/{product_bin_name}/{username}"


def resolve_deployment_dir_for_user_windows(
    get_profile_path_fn: Callable[[str], str],
    is_writable_fn: Callable[[str], bool],
    temp_dir: str,
    username: str,
    product_bin_name: str,
) -> str:
    """
    Resolve the <product_bin_name> deployment directory for a specific user on Windows.
    Mirrors ResolveToolsBaseDir in apap-engine/conductor/target_path_resolver.go.
    """
    profile_path = get_profile_path_fn(username)
    if profile_path and is_writable_fn(profile_path):
        return f"{profile_path}/AppData/Local/{product_bin_name}"

    return f"{temp_dir}/{product_bin_name}/{username}"
