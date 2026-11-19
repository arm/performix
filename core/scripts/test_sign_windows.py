# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import concurrent.futures
import json
import os
import shutil
import subprocess
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from lib.msys2 import find_msys2_bash, is_windows_host, ucrt64_environment


# Exercise the real Bash scripts without contacting the signing service.
FAKE_CURL = r'''
curl() {
    local method=GET output= data= upload= url=
    while (( $# )); do
        case "$1" in
            -X) method="$2"; shift ;;
            -o) output="$2"; shift ;;
            -d) data="$2"; shift ;;
            -T) upload="$2"; shift ;;
            -H) shift ;;
            http*) url="$1" ;;
        esac
        shift
    done
    printf '%s|%s|%s\n' "$method" "$url" "$data" >> "$SIGN_TEST_LOG"
    if [[ "$method" == PUT ]]; then
        [[ -f "$upload" ]] || return 1
    elif [[ "$method" == GET ]]; then
        printf 'signed binary' > "$output"
    fi
    [[ "$method" != "$SIGN_TEST_FAIL" ]]
}
export -f curl
bash "$SIGN_TEST_SCRIPT" windows "$SIGN_TEST_BINARY" 1.2.3 arm64
'''


class WindowsSigningScriptTests(unittest.TestCase):
    def sign(self, root, failure=""):
        root.mkdir()
        binary = root / "tool.exe"
        binary.write_bytes(b"original binary")
        binary.chmod(0o755)
        environment = ucrt64_environment() if is_windows_host() else os.environ.copy()
        environment.update({
            "SIGN_REQUIRED": "true",
            "ARTIFACTORY_API_TOKEN": "test-token",
            "SIGN_TEST_SCRIPT": Path(__file__).with_name("sign.sh").resolve().as_posix(),
            "SIGN_TEST_BINARY": binary.as_posix(),
            "SIGN_TEST_LOG": (root / "requests.log").as_posix(),
            "SIGN_TEST_FAIL": failure,
            "TMPDIR": root.as_posix(),
        })
        bash = str(find_msys2_bash()) if is_windows_host() else shutil.which("bash")
        if bash is None:
            self.skipTest("Bash is required to test Windows signing scripts")
        result = subprocess.run(
            [bash, "-c", FAKE_CURL], env=environment, capture_output=True, text=True,
        )
        log = root / "requests.log"
        self.assertTrue(log.exists(), result.stderr)
        calls = [line.split("|", 2) for line in log.read_text().splitlines()]
        self.assertEqual(
            sorted(file.name for file in root.iterdir()), ["requests.log", "tool.exe"]
        )
        return result, binary, calls

    def test_concurrent_requests_use_separate_upload_and_cleanup_paths(self):
        with tempfile.TemporaryDirectory() as temporary:
            roots = [Path(temporary) / f"build {index}" for index in range(2)]
            with concurrent.futures.ThreadPoolExecutor() as executor:
                results = list(executor.map(self.sign, roots))
            request_roots = []
            for result, binary, calls in results:
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(binary.read_bytes(), b"signed binary")
                if not is_windows_host():
                    self.assertEqual(binary.stat().st_mode & 0o777, 0o755)
                self.assertEqual(
                    [call[0] for call in calls], ["PUT", "POST", "GET", "DELETE"]
                )
                request_root = calls[0][1].rsplit("/", 1)[0]
                request_roots.append(request_root)
                payload = json.loads(calls[1][2])
                self.assertEqual(payload["source"], calls[0][1])
                self.assertEqual(payload["dest"], calls[2][1])
                self.assertEqual(calls[2][1], f"{request_root}/signed/tool.exe")
                self.assertEqual(calls[3][1], request_root)
            self.assertNotEqual(*request_roots)

    def test_failed_upload_sign_or_download_preserves_original(self):
        for failure in ("PUT", "POST", "GET"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                result, binary, calls = self.sign(Path(temporary) / "build", failure)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(binary.read_bytes(), b"original binary")
                self.assertEqual(calls[-1][0], failure)


SCRIPT = Path(__file__).with_name("sign-windows.sh").resolve()
UNSIGNED = b"unsigned executable"
SIGNED = b"signed executable"

# Redirect every request to the test server and shorten only the retry delay.
RUN_SCRIPT = r'''
curl() {
    local args=("$@")
    local index url
    for ((index = 0; index < ${#args[@]}; index++)); do
        if [[ "${args[index]}" == --retry-delay ]]; then
            args[index + 1]=1
        fi
    done
    index=$((${#args[@]} - 1))
    url="${args[index]#https://}"
    args[index]="${SIGNING_TEST_URL}/${url#*/}"
    command "$SIGNING_TEST_CURL" --disable --noproxy '*' "${args[@]}"
}
source "$@"
'''


class SigningHandler(BaseHTTPRequestHandler):
    def respond(self, status=200, body=b""):
        length = int(self.headers.get("Content-Length", 0))
        self.server.requests.append(
            (self.command, self.path, self.rfile.read(length))
        )
        self.send_response(status)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_PUT(self):
        self.respond()

    def do_POST(self):
        self.respond(self.server.signing_statuses.pop(0))

    def do_GET(self):
        self.respond(body=SIGNED)

    def do_DELETE(self):
        self.respond()

    def log_message(self, *_args):
        pass


class WindowsSigningTests(unittest.TestCase):
    def setUp(self):
        self.environment = ucrt64_environment() if is_windows_host() else os.environ.copy()
        self.bash = str(find_msys2_bash()) if is_windows_host() else shutil.which("bash")
        curl = shutil.which("curl", path=self.environment.get("PATH"))
        if self.bash is None or curl is None:
            self.skipTest("Bash and curl are required to test Windows signing scripts")
        self.curl = Path(curl).as_posix()
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.binary = Path(temporary.name) / "apx.exe"
        self.binary.write_bytes(UNSIGNED)
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), SigningHandler)
        self.addCleanup(self.server.server_close)
        self.server.requests = []
        self.server.signing_statuses = []
        thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(thread.join)
        self.addCleanup(self.server.shutdown)

    def run_signing(self, statuses):
        self.server.signing_statuses = list(statuses)
        return subprocess.run(
            [self.bash, "-c", RUN_SCRIPT, "test-signing", SCRIPT.as_posix(),
             self.binary.as_posix(), "1.2.3", "arm64"],
            env={
                **self.environment,
                "ARTIFACTORY_API_TOKEN": "test-token",
                "SIGNING_TEST_CURL": self.curl,
                "SIGNING_TEST_URL": f"http://127.0.0.1:{self.server.server_port}",
            },
            capture_output=True,
            text=True,
            timeout=30,
        )

    def test_success_downloads_signed_binary_and_cleans_up(self):
        result = self.run_signing([200])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.binary.read_bytes(), SIGNED)
        self.assertEqual(
            [method for method, _, _ in self.server.requests],
            ["PUT", "POST", "GET", "DELETE"],
        )

    def test_http_500_retries_the_same_post_then_downloads(self):
        result = self.run_signing([500, 200])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.binary.read_bytes(), SIGNED)
        self.assertEqual(
            [method for method, _, _ in self.server.requests],
            ["PUT", "POST", "POST", "GET", "DELETE"],
        )
        self.assertEqual(self.server.requests[1], self.server.requests[2])
        upload_path = self.server.requests[0][1]
        request_root = upload_path.rsplit("/", 1)[0]
        self.assertRegex(upload_path, r"^/artifactory/ste-apap\.cli/1\.2\.3/arm64/[^/]+/apx\.exe$")
        self.assertEqual(self.server.requests[3][1], f"{request_root}/signed/apx.exe")
        self.assertEqual(self.server.requests[4][1], request_root)
        self.assertEqual(
            json.loads(self.server.requests[1][2]),
            {
                "signer": "authenticode",
                "source": f"https://eu-west-1.artifactory.aws.arm.com{upload_path}",
                "dest": f"https://eu-west-1.artifactory.aws.arm.com{request_root}/signed/apx.exe",
            },
        )

    def test_repeated_http_500_fails_after_four_attempts(self):
        result = self.run_signing([500] * 4)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.binary.read_bytes(), UNSIGNED)
        self.assertEqual(
            [method for method, _, _ in self.server.requests],
            ["PUT", "POST", "POST", "POST", "POST"],
        )

    def test_authentication_errors_fail_without_retrying(self):
        for status in (401, 403):
            with self.subTest(status=status):
                self.server.requests.clear()
                result = self.run_signing([status])
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.binary.read_bytes(), UNSIGNED)
                self.assertEqual(
                    [method for method, _, _ in self.server.requests],
                    ["PUT", "POST"],
                )


if __name__ == "__main__":
    unittest.main()
