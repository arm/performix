# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Cross-platform foundation helpers for the Go build/test/lint orchestration scripts.

# Contents of Package
This package replaces the shared Bash plumbing under ``core/scripts`` so the
``task``/``make`` entrypoints work cross-platform (Linux, macOS, Windows) without requiring Bash:
- :mod:`lib.constants` - shared Artifactory URLs (replaces ``constants.sh``) and the ``core/``
  root locator ``get_core_root`` (replaces ``get-project-root.sh``).
- :mod:`lib.goflags` - the mandatory ``duckdb_arrow`` build-tag handling.

# Entrypoints & Imports
## Ensure scripts are on ``sys.path``
Scripts should ensure ``core/scripts`` is on ``sys.path``.

The reliable method is to run entrypoints with ``python -m`` from ``core/scripts``.
For example, to run ``core/scripts/protobuf/generate.py``:
```
cd core/scripts
python -m protobuf.generate
```

Alternatively, set ``PYTHONPATH=core/scripts``.

## Imports
Entrypoints then import these helpers absolutely, for example:
```
from lib.goflags import go_env
```

Modules inside this package import siblings relatively, for example:
```
from .constants import get_core_root
```
"""
