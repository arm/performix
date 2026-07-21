#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Collect system utilization stats and write to a timeline CSV.
"""

import sys
import traceback
import collector


if __name__ == "__main__":
    try:
        rc = collector.main(sys.argv[1:])
    except KeyboardInterrupt:
        # ctrl-c that turned into an exception -> treat as graceful stop
        rc = 0
    except SystemExit:
        # If other code explicitly calls sys.exit(), re-raise with its code
        # but ensure KeyboardInterrupt (if any) was handled above.
        raise
    except Exception:
        # Optional: show traceback then exit non-zero (helps debugging)
        traceback.print_exc()
        rc = 1
    # Exit with the chosen return code
    raise SystemExit(rc)
