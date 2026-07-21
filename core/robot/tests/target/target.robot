# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'target' CLI of Arm Total Performance.
Resource        ../../resources/keywords/target.resource
Suite Setup     Target Suite Setup
Suite Teardown  Target Suite Teardown
Test Tags       target


*** Test Cases ***
### Add ###

A Target Can Be Added
  [Documentation]  Tests that a new target can be added.
  [Tags]  add
  [Setup]  Run Keywords  Count Targets
  ...  AND  Generate SSH Key Pair  ${CURDIR}  id_supergator
  Given The Number Of Targets Is  ${G_NUM_TARGETS}
  When Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_cool_target
  Then The Last Command Succeeded
  And The Number Of Targets Has Increased By  1
  And The Target Is Displayed Correctly  host=1.2.3.4  user=supergator  key=${CURDIR}${/}id_supergator  port=${123}  name=my_cool_target
  [Teardown]  Run Keywords  Remove Target  my_cool_target
  ...  AND  Delete SSH Key Pair  ${CURDIR}  id_supergator

Duplicate Target Names Cannot Be Added
  [Documentation]  Tests that a target can't be added with a name that already exists.
  [Tags]  add
  [Setup]  Run Keywords  Generate SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  The Target Does Not Exist  my_cool_target
  ...  AND  Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_cool_target
  ...  AND  Count Targets
  Given The Number Of Targets Is  ${G_NUM_TARGETS}
  When Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_cool_target
  Then The Last Command Failed
  And The Number Of Targets Is  ${G_NUM_TARGETS}
  [Teardown]  Run Keywords  Remove Target  my_cool_target
  ...  AND  Delete SSH Key Pair  ${CURDIR}  id_supergator

A Target Named Localhost Cannot Be Added
  [Documentation]  Tests that a target can't be added with the reserved name localhost.
  [Tags]  add
  [Setup]  Run Keywords  Count Targets
  ...  AND  Generate SSH Key Pair  ${CURDIR}  id_supergator
  Given The Number Of Targets Is  ${G_NUM_TARGETS}
  When Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name localhost
  Then The Last Command Failed
  And The Number Of Targets Is  ${G_NUM_TARGETS}
  [Teardown]  Delete SSH Key Pair  ${CURDIR}  id_supergator

### Remove ###

A Target Can Be Removed
  [Documentation]  Tests that an individual target can be removed.
  [Tags]  remove
  [Setup]  Run Keywords  Generate SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  The Target Does Not Exist  my_cool_target
  ...  AND  Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_cool_target
  ...  AND  Count Targets
  Given The Number Of Targets Is  ${G_NUM_TARGETS}
  When Remove Target  my_cool_target
  Then The Last Command Succeeded
  And The Number Of Targets Has Decreased By  1
  [Teardown]  Delete SSH Key Pair  ${CURDIR}  id_supergator

A Target Can't Be Removed If Unknown
  [Documentation]  Tests that a target remove fails if the name isn't known.
  [Tags]  remove
  [Setup]  Run Keywords  Count Targets
  Given The Target Does Not Exist  my_unknown_target
  When Remove Target  my_unknown_target
  Then The Last Command Failed
  And The Number Of Targets Is  ${G_NUM_TARGETS}
  [Teardown]  Delete SSH Key Pair  ${CURDIR}  id_supergator

### Update ###

A Target Name Can Be Updated
  [Documentation]  Tests that target update can be used to change an existing target's name.
  [Tags]  update
  [Setup]  Run Keywords  Generate SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Ensure Target Does Not Exist  my_cool_target
  ...  AND  Ensure Target Does Not Exist  my_even_cooler_target
  ...  AND  Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_cool_target
  Given The Target Exists  my_cool_target
  And The Target Does Not Exist  my_even_cooler_target
  When Update Target  my_cool_target  name  my_even_cooler_target
  Then The Target Does Not Exist  my_cool_target
  And The Target Exists  my_even_cooler_target
  [Teardown]  Delete SSH Key Pair  ${CURDIR}  id_supergator

Target Host, Port and User Can Be Updated
  [Documentation]  Tests that target update can be used to change target fields other than name.
  [Tags]  update
  [Setup]  Run Keywords  Generate SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Ensure Target Does Not Exist  my_cool_target
  ...  AND  Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_cool_target
  Given The Target Exists  my_cool_target
  When Update Target  my_cool_target  host  alice@5.6.7.8:456:${CURDIR}${/}id_supergator
  Then The Target Is Single SSH With Details Containing  my_cool_target  host  5.6.7.8
  And The Target Is Single SSH With Details Containing  my_cool_target  port  456
  And The Target Is Single SSH With Details Containing  my_cool_target  username  alice
  [Teardown]  Delete SSH Key Pair  ${CURDIR}  id_supergator

Target SSH Key Can Be Updated
  [Documentation]  Tests that target update can be used to change target SSH key.
  [Tags]  update
  [Setup]  Run Keywords  Generate SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Generate SSH Key Pair  ${CURDIR}  id_megagator
  ...  AND  Ensure Target Does Not Exist  my_cool_target
  ...  AND  Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_cool_target
  Given The Target Exists  my_cool_target
  When Update Target  my_cool_target  host  supergator@1.2.3.4:${123}:${CURDIR}${/}id_megagator
  Then The Target Is Single SSH With Details Containing  my_cool_target  private_key_filename  ${CURDIR}${/}id_megagator
  [Teardown]  Run Keywords  Delete SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Delete SSH Key Pair  ${CURDIR}  id_megagator

Target SSH Key Cannot Be Set To An Invalid Key File
  [Documentation]  Tests that target update rejects invalid key files and the old target remains unchanged.
  [Tags]  update
  [Setup]  Run Keywords  Generate SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Ensure Target Does Not Exist  my_cool_target
  ...  AND  Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_cool_target
  ...  AND  Create File  ${CURDIR}${/}totally_a_valid_key_file  Definitely Valid Stuff Here!
  Given The Target Exists  my_cool_target
  When Update Target  my_cool_target  host  supergator@1.2.3.4:${123}:${CURDIR}${/}totally_a_valid_key_file
  Then The Last Command Failed
  And The Target Is Single SSH With Details Containing  my_cool_target  private_key_filename  ${CURDIR}${/}id_supergator
  [Teardown]  Run Keywords  Delete SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Remove File  ${CURDIR}${/}totally_a_valid_key_file

### Test ###

A Test Succeeds To A Valid Target
  [Documentation]  Tests that a valid target passes a connection test.
  [Tags]  test  disabled
  [Setup]  Run Keywords  Generate SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Generate SSH Key Pair  ${CURDIR}  id_megagator
  ...  AND  Ensure Target Does Not Exist  my_valid_target
  ...  AND  Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_valid_target
  Given The Target Exists  my_valid_target
  When Test Target  my_valid_target
  Then The Target Test Is Successful  my_valid_target
  [Teardown]  Run Keywords  Delete SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Remove File  ${CURDIR}${/}totally_a_valid_key_file
  ...  AND  Ensure Target Does Not Exist  my_valid_target

A Test Fails To An Invalid Target
  [Documentation]  Tests that an invalid target fails a connection test.
  [Tags]  test  disabled
  [Setup]  Run Keywords  Generate SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Generate SSH Key Pair  ${CURDIR}  id_megagator
  ...  AND  Ensure Target Does Not Exist  my_invalid_target
  ...  AND  Add Target  supergator@1.2.3.4:${123}:${CURDIR}${/}id_supergator --name my_invalid_target
  Given The Target Exists  my_invalid_target
  When Test Target  my_invalid_target
  Then The Target Test Is Not Successful  my_invalid_target
  [Teardown]  Run Keywords  Delete SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Remove File  ${CURDIR}${/}id_supergator
  ...  AND  Ensure Target Does Not Exist  my_invalid_target

Correct Target Is Tested When Multiple Targets Exist
  [Documentation]  Tests that the correct target is tested when multiple targets exist.
  [Tags]  test
  [Setup]  Run Keywords  Remove All Targets
  ...  AND  Create Default Dummy Target  dummy
  ...  AND  The Test Target Is Added Successfully
  Given The Target Is The Default  dummy
  And The Target Is Not The Default  ${G_TARGET_NAME}
  When Test Target  ${G_TARGET_NAME}
  Then The Last Command Succeeded
  [Teardown]  Remove All Targets

The Localhost Target Test Succeeds
  [Documentation]  Tests that the reserved localhost target passes target test.
  [Tags]  test  localhost
  [Setup]  Set Localhost As Test Target
  Given The Target Exists  ${G_TARGET_NAME}
  When Test Target  ${G_TARGET_NAME}
  Then The Last Command Succeeded
  [Teardown]  Restore Remote Test Target

### Default ###

The First SSH Target Added Is Automatically Set As Default
  [Documentation]  Tests that the first target added automatically becomes the default target.
  [Tags]  default
  [Setup]  Run Keywords  Remove All Targets
  ...  AND  Generate SSH Key Pair  ${CURDIR}  id_supergator
  Given There Are No SSH Targets
  When Add Target  root@1.2.3.4:443:${CURDIR}${/}id_supergator --name my_default_target
  And The Number Of Targets Has Increased By  1
  Then The Target Is The Default  my_default_target
  [Teardown]  Run Keywords  Delete SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Remove File  ${CURDIR}${/}id_supergator
  ...  AND  Ensure Target Does Not Exist  my_default_target

The Default Target Can Be Set Manually
  [Documentation]  Tests that the default target can be manually assigned to another target.
  [Tags]  default
  [Setup]  Run Keywords  Remove All Targets
  ...  AND  Generate SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Add Target  root@1.2.3.4:443:${CURDIR}${/}id_supergator --name my_first_target
  ...  AND  Generate SSH Key Pair  ${CURDIR}  id_megagator
  ...  AND  Add Target  root@5.6.7.8:443:${CURDIR}${/}id_megagator --name my_second_target
  Given The Target Is The Default  my_first_target
  And The Target Is Not The Default  my_second_target
  When Set Default Target  my_second_target
  Then The Target Is The Default  my_second_target
  And The Target Is Not The Default  my_first_target
  [Teardown]  Run Keywords  Remove All Targets
  ...  AND  Delete SSH Key Pair  ${CURDIR}  id_supergator
  ...  AND  Remove File  ${CURDIR}${/}id_supergator
  ...  AND  Delete SSH Key Pair  ${CURDIR}  id_megagator
  ...  AND  Remove File  ${CURDIR}${/}id_megagator

### Prepare ###

The Target Can Be Prepared
  [Documentation]  Tests that the target can be prepared for collection.
  [Tags]  prepare
  [Setup]  Run Keywords  Remove All Targets
  ...  AND  The Test Target Is Added Successfully
  ...  AND  Unprepare The Target
  Given The Target Is Unprepared
  When Prepare The Test Target
  Then The Last Command Succeeded
  And The Target Is Prepared
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  The Test Target Is Removed Successfully

Localhost Can Be Prepared
  [Documentation]  Tests that a localhost target can be prepared for collection.
  [Tags]  prepare  localhost
  [Setup]  Run Keywords  Set Localhost As Test Target
  ...  AND  Remove All Targets
  ...  AND  Unprepare The Target
  Given The Target Is Unprepared
  When Prepare The Test Target
  Then The Last Command Succeeded
  And The Target Is Prepared
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  Restore Remote Test Target

Correct Target Is Prepared When Multiple Targets Exist
  [Documentation]  Tests that the correct target is prepared when multiple targets exist.
  [Tags]  prepare
  [Setup]  Run Keywords  Remove All Targets
  ...  AND  Create Default Dummy Target  dummy
  ...  AND  The Test Target Is Added Successfully
  ...  AND  Unprepare The Target
  Given The Target Is The Default  dummy
  And The Target Is Not The Default  ${G_TARGET_NAME}
  When Prepare The Test Target
  Then The Last Command Succeeded
  And The Target Is Prepared
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  Remove All Targets

The Target Prepare Closes The Target Agent Connection
  [Documentation]  Tests that preparing the target closes the connection to the
  ...  agent on target. This behaviour ensures stale agents are cleaned up.
  [Tags]  prepare
  [Setup]  Run Keywords  Remove All Targets
  ...  AND  The Test Target Is Added Successfully
  Given Prepare The Test Target
  AND Run Target Info  ${G_TARGET_NAME}
  AND Target Process Is Running  ${AGENT_BINARY_FILE_NAME}
  When Remove The Target Deployment Directory
  AND Prepare The Test Target
  Then Target Process Is Not Running  ${AGENT_BINARY_FILE_NAME}
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  The Test Target Is Removed Successfully

### Info ###

The Target Info CLI Contains Target Information
  [Documentation]  Tests that the target info CLI outputs some information about the specified target
  ...  Note the test doesn't confirm the info is correct
  ...  We currently only support Linux targets so confirm the OS family is Linux
  [Tags]  info
  [Setup]  Run Keywords  The Test Target Is Added Successfully
  ...  AND  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  ...  AND  Prepare The Test Target
  Given The Target Is Prepared
  When Run Target Info  ${G_TARGET_NAME}
  Then The Last Command Succeeded
  And The Target Info Contains  family  ${G_TARGET_OS}  cpu_arch  ${G_TARGET_ARCH}
  [Teardown]  Remove Target  ${G_TARGET_NAME}

The Target Info CLI Contains Target Information For Localhost
  [Documentation]  Tests that the target info CLI outputs some information about the localhost target
  ...  Note the test doesn't confirm the info is correct
  ...  We currently only support Linux targets so confirm the OS family is Linux
  [Tags]  info  localhost
  [Setup]  Run Keywords  Set Localhost As Test Target
  ...  AND  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  ...  AND  Prepare The Test Target
  Given The Target Is Prepared
  When Run Target Info  ${G_TARGET_NAME}
  Then The Last Command Succeeded
  And The Target Info Contains  family  ${G_TARGET_OS}  cpu_arch  ${G_TARGET_ARCH}
  [Teardown]  Restore Remote Test Target

The Target Info CLI Contains Process Information
  [Documentation]  Tests that the target info CLI outputs some running processes from the specified target
  ...  Note the test doesn't confirm the info is correct
  [Tags]  info
  [Setup]  Run Keywords  The Test Target Is Added Successfully
  ...  AND  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  ...  AND  Prepare The Test Target
  Given The Target Is Prepared
  When Run Target Info  ${G_TARGET_NAME} --pids
  Then The Last Command Succeeded
  And The Target PID Info Contains  pid  ${EXPECTED_PID}
  And The Target PID Info Contains  name  ${EXPECTED_PROCESS_NAME}
  [Teardown]  Remove Target  ${G_TARGET_NAME}

The Target Info CLI Contains Process Information For Localhost
  [Documentation]  Tests that the target info CLI outputs some running processes from the localhost target
  ...  Note the test doesn't confirm the info is correct
  [Tags]  info  localhost
  [Setup]  Run Keywords  Set Localhost As Test Target
  ...  AND  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  ...  AND  Prepare The Test Target
  Given The Target Is Prepared
  When Run Target Info  ${G_TARGET_NAME} --pids
  Then The Last Command Succeeded
  And The Target PID Info Contains  pid  ${EXPECTED_PID}
  And The Target PID Info Contains  name  ${EXPECTED_PROCESS_NAME}
  [Teardown]  Restore Remote Test Target


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.

Target Suite Setup
  Common Setup
  Remove All Targets

Target Suite Teardown
  Remove All Targets
  Common Teardown
