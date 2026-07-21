#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import uuid
import sys

def main():
    # Generate a random (version 4) UUID
    u = uuid.uuid4()
    print(str(u))
    return 0

if __name__ == "__main__":
    sys.exit(main())