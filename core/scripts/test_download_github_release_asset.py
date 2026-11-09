# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

import download_github_release_asset as downloader


class GitHubDownloadTests(unittest.TestCase):
    def test_fetch_release_uses_requests_default_verification(self):
        response = MagicMock()
        response.json.return_value = {"assets": []}
        with patch.object(downloader.requests, "get", return_value=response) as get:
            self.assertEqual(
                downloader.fetch_release("Arm-Debug/example", "v1.0.0", "token"),
                {"assets": []},
            )
        get.assert_called_once_with(
            "https://api.github.com/repos/Arm-Debug/example/releases/tags/v1.0.0",
            headers={
                "Accept": downloader.ACCEPT_JSON,
                "User-Agent": "gh-release-asset-downloader/0.1",
                "Authorization": "Bearer token",
            },
            timeout=120,
        )
        response.raise_for_status.assert_called_once()

    def test_download_asset_streams_response(self):
        response = MagicMock()
        response.__enter__.return_value = response
        response.iter_content.return_value = [b"first", b"second"]
        asset = {"url": "https://api.github.com/assets/1", "name": "asset.zip"}
        with tempfile.TemporaryDirectory() as temporary, patch.object(
            downloader.requests, "get", return_value=response
        ) as get:
            target = downloader.download_asset(asset, "token", temporary)
            self.assertEqual(Path(target).read_bytes(), b"firstsecond")
        get.assert_called_once_with(
            asset["url"],
            headers={
                "Accept": downloader.ACCEPT_BINARY,
                "User-Agent": "gh-release-asset-downloader/0.1",
                "Authorization": "Bearer token",
            },
            stream=True,
            timeout=120,
        )
        response.raise_for_status.assert_called_once()


if __name__ == "__main__":
    unittest.main()
