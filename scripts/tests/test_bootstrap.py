# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Test the bootstrap entrypoints as a developer would use them.

`bootstrap` on macOS/Linux and `bootstrap.ps1` on Windows run against an
isolated HOME/USERPROFILE so profile updates, mise installs, and mise-managed
tools can be checked from shell state.
"""

import os
import shlex
import shutil
import subprocess
import sys
import time
from pathlib import Path
from typing import Optional

import pytest

if sys.platform != "win32":
    import pty
    import select


REPO_ROOT = Path(__file__).resolve().parents[2]
BOOTSTRAP = REPO_ROOT / "bootstrap"
BOOTSTRAP_PS1 = REPO_ROOT / "bootstrap.ps1"
BOOTSTRAP_TIMEOUT_SECONDS = 900
TOOLS_EXPECTED_AFTER_ACTIVATION = ("task", "go", "node", "npm", "protoc")


def assert_success(result: subprocess.CompletedProcess, action: str) -> None:
    assert result.returncode == 0, (
        f"{action} failed with {result.returncode}\n"
        f"stdout:\n{result.stdout}\n"
        f"stderr:\n{result.stderr}"
    )


class UnixBootstrapDriver:
    # Keep Unix shell setup in one place so the tests can describe bootstrap
    # behaviour in platform-neutral terms.
    name = "bootstrap"
    profile_name = ".zshrc"
    path_block_name = "mise path"
    activation_block_name = "mise activation"

    def __init__(self, home: Path):
        self.home = home
        self.script = BOOTSTRAP
        self.default_shell = "/bin/zsh"
        self.alternate_shell = "/bin/bash"
        for shell in (self.default_shell, self.alternate_shell):
            if not os.access(shell, os.X_OK):
                pytest.fail(
                    f"bootstrap tests require an executable shell at {shell}",
                    pytrace=False,
                )
        self.default_path = "/usr/bin:/bin:/usr/sbin:/sbin"
        self.mise_path = home / ".local/bin/mise"

    @classmethod
    def available(cls) -> bool:
        return sys.platform in ("darwin", "linux")

    def env(self, *, shell: Optional[str] = None, path: Optional[str] = None):
        env = os.environ.copy()
        env.update(
            {
                "HOME": str(self.home),
                "SHELL": shell or self.default_shell,
                "PATH": path or self.default_path,
                "NO_COLOR": "1",
                "TERM": "dumb",
            }
        )
        env.pop("MISE_INSTALL_PATH", None)
        env.pop("MISE_SHELL", None)
        return env

    def run(self, *args, env=None, check=True):
        result = subprocess.run(
            [str(self.script), *args],
            cwd=REPO_ROOT,
            env=env or self.env(),
            text=True,
            capture_output=True,
            timeout=BOOTSTRAP_TIMEOUT_SECONDS,
            check=False,
        )
        if check:
            assert_success(result, self.name)
        return result

    def run_interactive(self, input_text: str, *args, env=None):
        # Run prompt tests through a pty so default answers exercise the same
        # path a developer uses in a terminal.
        master_fd, slave_fd = pty.openpty()
        try:
            deadline = time.monotonic() + BOOTSTRAP_TIMEOUT_SECONDS
            process = subprocess.Popen(
                [str(self.script), *args],
                cwd=REPO_ROOT,
                env=env or self.env(),
                stdin=slave_fd,
                stdout=slave_fd,
                stderr=slave_fd,
                text=False,
                close_fds=True,
            )
            os.close(slave_fd)
            slave_fd = None

            if input_text:
                os.write(master_fd, input_text.encode())

            chunks = []
            while True:
                if time.monotonic() > deadline:
                    process.kill()
                    process.wait(timeout=5)
                    output = b"".join(chunks).decode(errors="replace")
                    pytest.fail(f"interactive {self.name} timed out\n{output}")

                ready, _, _ = select.select([master_fd], [], [], 0.1)
                if master_fd in ready:
                    try:
                        chunk = os.read(master_fd, 4096)
                    except OSError:
                        break
                    if not chunk:
                        break
                    chunks.append(chunk)

                if process.poll() is not None:
                    while True:
                        ready, _, _ = select.select([master_fd], [], [], 0)
                        if master_fd not in ready:
                            break
                        try:
                            chunk = os.read(master_fd, 4096)
                        except OSError:
                            break
                        if not chunk:
                            break
                        chunks.append(chunk)
                    break

            output = b"".join(chunks).decode(errors="replace").replace("\r\n", "\n")
            assert process.wait(timeout=5) == 0, output
            return output
        finally:
            if slave_fd is not None:
                os.close(slave_fd)
            os.close(master_fd)

    def bootstrap_fresh(self):
        self.run_interactive(self.interactive_accept_setup_input())

    def interactive_accept_setup_input(self) -> str:
        return "\n\n"

    def interactive_decline_activation_input(self) -> str:
        return "\nn\n"

    def assert_interactive_prompts(self, output: str) -> None:
        assert "Add mise to PATH? [Y/n]" in output
        assert "Enable automatic toolchain activation? [Y/n]" in output

    def run_shell_after_sourcing(self, profile: Path, command: str, *, shell: Optional[str] = None):
        env = os.environ.copy()
        env.update(
            {
                "HOME": str(self.home),
                "PATH": self.default_path,
                "NO_COLOR": "1",
            }
        )
        script = f"source {shlex.quote(str(profile))}; {command}"
        return subprocess.run(
            [shell or self.default_shell, "-c", script],
            env=env,
            text=True,
            capture_output=True,
            timeout=60,
            check=False,
        )

    def run_shell_without_sourcing(self, command: str, *, shell: Optional[str] = None):
        env = os.environ.copy()
        env.update(
            {
                "HOME": str(self.home),
                "PATH": self.default_path,
                "NO_COLOR": "1",
            }
        )
        return subprocess.run(
            [shell or self.default_shell, "-c", command],
            env=env,
            text=True,
            capture_output=True,
            timeout=60,
            check=False,
        )

    def command_path_after_sourcing(
        self,
        profile: Path,
        command: str,
        *,
        shell: Optional[str] = None,
    ) -> Path:
        result = self.run_shell_after_sourcing(
            profile,
            f"command -v {command}",
            shell=shell,
        )
        assert_success(result, f"resolve {command}")
        return Path(result.stdout.strip())

    def profile(self) -> Path:
        return self.home / self.profile_name

    def profile_if_exists(self) -> Optional[Path]:
        profile = self.profile()
        if profile.exists():
            return profile
        return None

    def alternate_profile(self) -> Path:
        return self.home / ".bashrc"

    def profile_for_shell(self, shell: str) -> Path:
        if shell.endswith("bash"):
            return self.home / ".bashrc"
        return self.home / ".zshrc"

    def block_count(self, profile: Path, block_name: str) -> int:
        return profile.read_text(encoding="utf-8").count(f"# >>> Performix {block_name} >>>")

    def assert_profile_configures_mise(self, profile: Path, *, shell: Optional[str] = None):
        content = profile.read_text(encoding="utf-8")
        assert 'export PATH="$HOME/.local/bin:$PATH"' in content
        assert self.activation_block_name in content
        assert self.mise_path.is_file()
        before = self.run_shell_without_sourcing("which mise", shell=shell)
        assert before.returncode != 0, before.stdout
        after = self.run_shell_after_sourcing(profile, "which mise", shell=shell)
        assert after.returncode == 0, after.stderr
        assert after.stdout.strip()

    def assert_profile_activates_toolchain(self, profile: Path, *, shell: Optional[str] = None):
        for tool in TOOLS_EXPECTED_AFTER_ACTIVATION:
            path = self.command_path_after_sourcing(profile, tool, shell=shell)
            assert str(path).startswith(str(self.home)), (
                f"{tool} resolved outside isolated HOME: {path}"
            )

    def assert_tools_not_activated(self, profile: Path):
        for tool in TOOLS_EXPECTED_AFTER_ACTIVATION:
            result = self.run_shell_after_sourcing(profile, f"command -v {tool}")
            if result.returncode != 0:
                continue
            path = Path(result.stdout.strip())
            assert not str(path).startswith(str(self.home)), (
                f"{tool} unexpectedly resolved from isolated HOME: {path}"
            )

    def assert_declined_activation_profile(self, profile: Optional[Path]) -> None:
        assert profile is not None
        content = profile.read_text(encoding="utf-8")
        assert self.path_block_name in content
        assert self.activation_block_name not in content
        before = self.run_shell_without_sourcing("which mise")
        assert before.returncode != 0, before.stdout
        after = self.run_shell_after_sourcing(profile, "which mise")
        assert after.returncode == 0, after.stderr
        assert after.stdout.strip()

    def copy_install_from(self, other: "UnixBootstrapDriver") -> None:
        # Prompt tests start from an existing local mise install. Copying a real
        # install preserves the executable and installed tool layout.
        (self.home / ".local/bin").mkdir(parents=True)
        shutil.copy2(other.mise_path, self.mise_path)
        source_data_dir = other.home / ".local/share/mise"
        target_data_dir = self.home / ".local/share/mise"
        if source_data_dir.exists():
            shutil.copytree(source_data_dir, target_data_dir, symlinks=True)

    def env_with_mise_on_path(self):
        env = self.env(path=f"{self.mise_path.parent}:{self.default_path}")
        env["MISE_SHELL"] = "zsh"
        return env

    def path_without_curl(self, toolbin: Path) -> str:
        for tool in ("dirname", "uname"):
            real_tool = shutil.which(tool)
            if real_tool is None:
                pytest.skip(f"{tool} is not available on this host")
            tool_path = toolbin / tool
            tool_path.parent.mkdir(parents=True, exist_ok=True)
            tool_path.symlink_to(real_tool)
        return str(toolbin)


class WindowsBootstrapDriver:
    # The Windows driver maps the shared bootstrap checks onto PowerShell
    # profiles and the Windows mise install location.
    name = "bootstrap.ps1"
    profile_name = "profile.ps1"
    path_block_name = "mise path"
    activation_block_name = "mise activation"

    def __init__(self, home: Path):
        self.home = home
        self.local_app_data = home / "LocalAppData"
        self.script = BOOTSTRAP_PS1
        self.pwsh = shutil.which("pwsh")
        self.mise_path = self.local_app_data / "mise/bin/mise.exe"

    @classmethod
    def available(cls) -> bool:
        return sys.platform == "win32"

    def env(self, *, shell: Optional[str] = None, path: Optional[str] = None):
        env = os.environ.copy()
        env.update(
            {
                "HOME": str(self.home),
                "USERPROFILE": str(self.home),
                "LOCALAPPDATA": str(self.local_app_data),
                "MISE_INSTALL_PATH": str(self.mise_path),
                "NO_COLOR": "1",
            }
        )
        if path is not None:
            env["PATH"] = path
        env.pop("MISE_SHELL", None)
        return env

    def run(self, *args, env=None, check=True):
        if self.pwsh is None:
            pytest.skip("pwsh is not installed")
        result = subprocess.run(
            [
                self.pwsh,
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                str(self.script),
                *args,
            ],
            cwd=REPO_ROOT,
            env=env or self.env(),
            text=True,
            capture_output=True,
            timeout=BOOTSTRAP_TIMEOUT_SECONDS,
            check=False,
        )
        if check:
            assert_success(result, self.name)
        return result

    def run_interactive(self, input_text: str, *args, env=None):
        if self.pwsh is None:
            pytest.skip("pwsh is not installed")
        result = subprocess.run(
            [
                self.pwsh,
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                str(self.script),
                *args,
            ],
            cwd=REPO_ROOT,
            env=env or self.env(),
            input=input_text,
            text=True,
            capture_output=True,
            timeout=BOOTSTRAP_TIMEOUT_SECONDS,
            check=False,
        )
        assert_success(result, self.name)
        return result.stdout + result.stderr

    def bootstrap_fresh(self):
        self.run_interactive(self.interactive_accept_setup_input())

    def interactive_accept_setup_input(self) -> str:
        # Accept activation through the test PowerShell profile. Leave the real
        # Windows user PATH unchanged.
        return "n\n\n"

    def interactive_decline_activation_input(self) -> str:
        return "n\nn\n"

    def assert_interactive_prompts(self, output: str) -> None:
        assert "Add mise to PATH? [Y/n]" in output
        assert "Enable automatic toolchain activation? [Y/n]" in output

    def profile(self) -> Path:
        profiles = list(self.home.rglob("*profile*.ps1"))
        assert profiles
        return profiles[0]

    def profile_if_exists(self) -> Optional[Path]:
        profiles = list(self.home.rglob("*profile*.ps1"))
        if profiles:
            return profiles[0]
        return None

    def alternate_profile(self) -> Path:
        pytest.skip("bootstrap.ps1 only configures PowerShell in this suite")

    def profile_for_shell(self, shell: str) -> Path:
        return self.profile()

    def block_count(self, profile: Path, block_name: str) -> int:
        return profile.read_text(encoding="utf-8").count(f"# >>> Performix {block_name} >>>")

    def run_shell_after_sourcing(self, profile: Path, command: str, *, shell: Optional[str] = None):
        if self.pwsh is None:
            pytest.skip("pwsh is not installed")
        profile_source = f". '{profile}'; " if profile.is_file() else ""
        ps_command = f"{profile_source}{command}"
        return subprocess.run(
            [
                self.pwsh,
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-Command",
                ps_command,
            ],
            env=self.env(),
            text=True,
            capture_output=True,
            timeout=60,
            check=False,
        )

    def command_path_after_sourcing(
        self,
        profile: Path,
        command: str,
        *,
        shell: Optional[str] = None,
    ) -> Path:
        result = self.run_shell_after_sourcing(
            profile,
            f"(Get-Command {command} -ErrorAction Stop).Source",
        )
        assert_success(result, f"resolve {command}")
        return Path(result.stdout.strip())

    def assert_profile_configures_mise(self, profile: Path, *, shell: Optional[str] = None):
        content = profile.read_text(encoding="utf-8")
        assert self.activation_block_name in content
        assert self.mise_path.is_file()

    def assert_profile_activates_toolchain(self, profile: Path, *, shell: Optional[str] = None):
        for tool in TOOLS_EXPECTED_AFTER_ACTIVATION:
            path = self.command_path_after_sourcing(profile, tool)
            assert str(path).startswith(str(self.home)) or str(path).startswith(
                str(self.local_app_data)
            ), f"{tool} resolved outside isolated HOME: {path}"

    def assert_tools_not_activated(self, profile: Optional[Path]):
        for tool in TOOLS_EXPECTED_AFTER_ACTIVATION:
            profile_source = ""
            if profile is not None and profile.exists():
                profile_source = f". '{profile}'; "
            result = self.run_shell_after_sourcing(
                profile or self.home,
                (
                    f"{profile_source}"
                    f"$command = Get-Command {tool} -ErrorAction SilentlyContinue; "
                    "$command.Source"
                ),
            )
            assert result.returncode == 0
            path = result.stdout.strip()
            assert not path.startswith(str(self.home))
            assert not path.startswith(str(self.local_app_data))

    def assert_declined_activation_profile(self, profile: Optional[Path]) -> None:
        if profile is None:
            return
        content = profile.read_text(encoding="utf-8")
        assert self.activation_block_name not in content

    def copy_install_from(self, other: "WindowsBootstrapDriver") -> None:
        source_mise_dir = other.local_app_data / "mise"
        target_mise_dir = self.local_app_data / "mise"
        if source_mise_dir.exists():
            shutil.copytree(source_mise_dir, target_mise_dir)
            return

        self.mise_path.parent.mkdir(parents=True)
        shutil.copy2(other.mise_path, self.mise_path)

    def env_with_mise_on_path(self):
        env = self.env()
        env["PATH"] = f"{self.mise_path.parent};{env.get('PATH', '')}"
        env["MISE_SHELL"] = "pwsh"
        return env

    def path_without_curl(self, toolbin: Path) -> str:
        pytest.skip("missing curl coverage applies to the POSIX shell bootstrap")


def make_driver(home: Path):
    # Run the entrypoint for the host platform. Shared assertions cover the
    # behaviour that should match across platforms.
    if UnixBootstrapDriver.available():
        return UnixBootstrapDriver(home)
    if WindowsBootstrapDriver.available():
        return WindowsBootstrapDriver(home)
    pytest.skip(f"unsupported bootstrap test platform: {sys.platform}")


@pytest.fixture(scope="session")
def driver(tmp_path_factory):
    # A fresh bootstrap covers the real mise download path and the first
    # `mise install`. Later `mise install` runs are normally cheap because mise
    # reuses its installed tool cache, so keep one bootstrapped HOME for tests
    # that inspect the resulting shell/profile state or rerun bootstrap.
    bootstrap_driver = make_driver(tmp_path_factory.mktemp("performix-bootstrap-home"))
    bootstrap_driver.bootstrap_fresh()
    return bootstrap_driver


def test_fresh_bootstrap_configures_profile_and_installs_tools(driver):
    """This tests that fresh bootstrap configures the pinned toolchain.

    A developer shell should be ready to use the Performix tools from the
    isolated test install after bootstrap completes.
    """
    profile = driver.profile()

    driver.assert_profile_configures_mise(profile)
    driver.assert_profile_activates_toolchain(profile)


def test_existing_local_mise_run_is_idempotent(driver):
    """This tests that bootstrap is idempotent for an existing local mise.

    Re-running bootstrap should keep one managed profile block per setting so
    developers can rerun setup after updates.
    """
    profile = driver.profile()

    driver.run_interactive(driver.interactive_accept_setup_input())

    assert driver.block_count(profile, driver.path_block_name) <= 1
    assert driver.block_count(profile, driver.activation_block_name) == 1
    driver.assert_profile_configures_mise(profile)
    driver.assert_profile_activates_toolchain(profile)


def test_existing_mise_on_path_does_not_rewrite_profile(driver):
    """This tests that bootstrap reuses an existing mise already on PATH.

    Existing shell setup should remain stable across repeated bootstrap runs.
    """
    profile = driver.profile()
    before = profile.read_text(encoding="utf-8")

    driver.run(env=driver.env_with_mise_on_path())

    assert profile.read_text(encoding="utf-8") == before
    driver.assert_profile_activates_toolchain(profile)


def test_bootstrap_can_configure_alternate_shell(driver):
    """This tests that bootstrap configures the active Unix shell profile.

    Unix developers may start from zsh or bash, so the script should update the
    profile belonging to the current shell.
    """
    if not isinstance(driver, UnixBootstrapDriver):
        pytest.skip("alternate shell profile coverage applies to bootstrap")

    driver.run_interactive(
        driver.interactive_accept_setup_input(),
        env=driver.env(shell=driver.alternate_shell),
    )

    profile = driver.alternate_profile()
    driver.assert_profile_configures_mise(profile, shell=driver.alternate_shell)
    driver.assert_profile_activates_toolchain(profile, shell=driver.alternate_shell)


def test_interactive_prompts_configure_existing_local_mise(tmp_path, driver):
    """This tests that prompt answers configure an existing local mise.

    The interactive flow should cover the same setup path a developer sees when
    starting from a local mise binary that is not yet configured for the shell.
    """
    interactive_driver = type(driver)(tmp_path / "home")
    interactive_driver.copy_install_from(driver)

    output = interactive_driver.run_interactive(interactive_driver.interactive_accept_setup_input())

    profile = interactive_driver.profile()
    interactive_driver.assert_interactive_prompts(output)
    interactive_driver.assert_profile_configures_mise(profile)
    interactive_driver.assert_profile_activates_toolchain(profile)


def test_interactive_declining_activation_leaves_tools_unactivated(tmp_path, driver):
    """This tests that declining activation leaves tools unactivated.

    A developer can make mise available and still choose explicit `mise exec`
    usage for the pinned toolchain.
    """
    interactive_driver = type(driver)(tmp_path / "home")
    interactive_driver.copy_install_from(driver)

    interactive_driver.run_interactive(interactive_driver.interactive_decline_activation_input())

    profile = interactive_driver.profile_if_exists()
    interactive_driver.assert_declined_activation_profile(profile)
    interactive_driver.assert_tools_not_activated(profile)


def test_bootstrap_download_requires_curl(tmp_path):
    """This tests that the POSIX bootstrap reports missing curl directly.

    The shell bootstrap avoids installing system packages, so a missing HTTPS
    client should produce a clear setup error.
    """
    if not UnixBootstrapDriver.available():
        pytest.skip("missing curl applies to bootstrap")

    driver = UnixBootstrapDriver(tmp_path / "home")
    env = driver.env()
    env["PATH"] = driver.path_without_curl(tmp_path / "toolbin")
    result = subprocess.run(
        ["/bin/bash", str(BOOTSTRAP)],
        cwd=REPO_ROOT,
        env=env,
        text=True,
        capture_output=True,
        timeout=60,
        check=False,
    )

    assert result.returncode == 1
    assert "curl is required to download mise" in result.stderr
