# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to verify that the CLI can be downloaded and installed correctly on Linux x86 hosts.
Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/download.resource
Resource        ../../resources/keywords/install.resource
Suite Setup     Install Linux X86 Suite Setup
Suite Teardown  Install Linux X86 Suite Teardown
Test Tags       install  linux  x86  disabled


*** Test Cases ***
The Latest Debian Package Can Be Downloaded and Installed
  [Documentation]  Test downloading and installing the latest Debian package on Linux x86 hosts.
  Given The Latest Debian Package Is Downloaded Successfully  linux  x86
  When The Download Package Is Extracted Successfully
  Then The Installation Is Working As Expected
  And The License Terms Should Be Valid
  And The Changelog Should Be Present
  [Teardown]  Remove Extracted Deb Package

The Latest Tar Package Can Be Downloaded and Extracted
  [Documentation]  Test downloading and extracting the latest Tar package on Linux x86 hosts.
  Given The Latest Tar Package Is Downloaded Successfully  Linux  x86
  When The Download Package Is Extracted Successfully
  Then The Installation Is Working As Expected
  And The License Terms Should Be Valid
  And The Changelog Should Be Present
  [Teardown]  Remove Extracted Tar Package


*** Keywords ***
Install Linux X86 Suite Setup
  Common Setup
  Skip If Host Operating System Is Not  Linux
  Skip If Host Architecture Is Not  ${ARCH_X86_64}
  GitHub Token Is Set
  GitHub Is Reachable

Install Linux X86 Suite Teardown
  Remove Download Packages
  Common Teardown
