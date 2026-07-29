# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation  A resource file containing SSH keywords.

Resource       common.resource
Library        Collections
Library        OperatingSystem
Library        Process
Library        String


*** Variables ***
${SSH_DIR}           ~${/}.ssh


*** Keywords ***
Generate SSH Key Pair
  [Arguments]  ${location}  ${key_name}
  Delete SSH Key Pair  ${location}  ${key_name}
  Run Host Command  ssh-keygen -t rsa -q -f ${location}${/}${key_name} -N ""
  The Last Command Succeeded

Delete SSH Key Pair
  [Arguments]  ${location}  ${key_name}
  Remove Files  ${location}${/}${key_name}  ${location}${/}${key_name}.pub
