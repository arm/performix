# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to verify that the CLI can be downloaded and installed correctly on MacOS arm64 hosts.
Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/download.resource
Resource        ../../resources/keywords/install.resource
Suite Setup     Install MacOs Arm64 Suite Setup
Suite Teardown  Install MacOs Arm64 Suite Teardown
Test Tags       install  macos  arm64  disabled


*** Test Cases ***
The Latest Tar Package Can Be Downloaded and Extracted
  [Documentation]  Test downloading and extracting the latest Tar package on MacOS arm64 hosts.
  Given The Latest Tar Package Is Downloaded Successfully  macOS  arm64
  When The Download Package Is Extracted Successfully
  Then The Installation Is Working As Expected
  And The License Terms Should Be Valid
  And The Changelog Should Be Present
  [Teardown]  Remove Extracted Tar Package


*** Keywords ***
Install MacOs Arm64 Suite Setup
  Common Setup
  Skip If Host Operating System Is Not  Darwin
  Skip If Host Architecture Is Not  ${ARCH_ARM64}
  GitHub Token Is Set
  GitHub Is Reachable

Install MacOs Arm64 Suite Teardown
  Remove Download Packages
  Common Teardown
