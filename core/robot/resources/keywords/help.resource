# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation  A resource file containing 'help' keywords for the Arm Total Performance CLI.
Resource       common.resource


*** Keywords ***
Display Help
  Run ATPerf CLI Command  --help

Help Is Displayed
  Should Be Equal As Integers  ${G_LAST_RESULT.rc}  0
  Should Not Be Empty  ${G_LAST_RESULT.stdout}
