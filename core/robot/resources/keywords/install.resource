# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation  A resource file containing keywords for extracting and installing Arm Performix packages.

Resource       common.resource


*** Variables ***
${EXTRACTED_PKG_DIR}             ${TEMPDIR}/apx
${EXTRACTED_PKG_PATH}            ${EMPTY}
${EXTRACTED_DEB_PATH}            ${EXTRACTED_PKG_DIR}/extracted_deb
${EXTRACTED_TAR_PATH}            ${EXTRACTED_PKG_DIR}/extracted_tar
${EXTRACTED_ZIP_PATH}            ${EXTRACTED_PKG_DIR}/extracted_zip
${EXPECTED_ATPERF_LOCATION_DEB}  opt/apx/apx
${EXPECTED_ATPERF_LOCATION_TAR}  apx
${EXPECTED_ATPERF_LOCATION_ZIP}  apx.exe
${EXPECTED_TPIP_LOCATION}  license_terms/
${EXPECTED_TPIP_MAIN}  Arm Performix
${EXPECTED_TPIP_TOOLS}  asct  jitdump-jvm  dotnet-agent  sl-analyze  sl-record  instruction_mix  wperf  cache_sharing
${EXPECTED_CHANGELOG_FILE}  CHANGELOG.md


*** Keywords ***
The Download Package Is Extracted Successfully
  [Documentation]  Extracts the download package and confirms it was successful.
  Extract Package  ${DOWNLOADED_PKG_PATH}
  The Last Command Succeeded

Get Package Extension
  [Documentation]  Returns the file extension of the specified package. The Split Extension keyword only splits on the
  ...  last dot, so needs to be called repeatedly to handle extensions like .tar.gz.
  [Arguments]  ${pkg}
  ${base}  ${ext} =  Split Extension  ${pkg}
  IF  '${ext}' == 'gz'
    # Split again to get "tar" and join with "gz" to get "tar.gz"
    ${base}  ${ext2} =  Split Extension  ${base}
    ${joined} =  Catenate  SEPARATOR=.  ${ext2}  ${ext}
    Should Not Be Empty  ${joined}  msg=Could not join package extensions
    RETURN  ${joined}
  END
  Should Not Be Empty  ${ext}  msg=Package extension could not be determined
  RETURN  ${ext}

Extract Package
  [Documentation]  Extracts the specified package based on its file extension.
  [Arguments]  ${pkg}
  ${ext} =  Get Package Extension  ${pkg}
  Log  Extracting package ${pkg} with extension ${ext}
  IF  '${ext}' == 'deb'
    Extract Deb Package  ${pkg}
  ELSE IF  '${ext}' == 'tar.gz'
    Extract Tar Package  ${pkg}
  ELSE IF  '${ext}' == 'zip'
    Extract Zip Package  ${pkg}
  ELSE
    Fail  Unsupported package extension: ${ext}
  END
  Directory Should Not Be Empty  ${EXTRACTED_PKG_PATH}

Extract Deb Package
  [Documentation]  Extracts the specified .deb package and sets the extracted path variable.
  [Arguments]  ${pkg}
  VAR  ${EXTRACTED_PKG_PATH} =  ${EXTRACTED_DEB_PATH}  scope=SUITE
  Create Directory  ${EXTRACTED_PKG_PATH}
  Run Host Command  dpkg-deb -R ${pkg} ${EXTRACTED_PKG_PATH}

Extract Tar Package
  [Documentation]  Extracts the specified .tar.gz package and sets the extracted path variable.
  [Arguments]  ${pkg}
  VAR  ${EXTRACTED_PKG_PATH} =  ${EXTRACTED_TAR_PATH}  scope=SUITE
  Create Directory  ${EXTRACTED_PKG_PATH}
  Run Host Command  tar -xzf ${pkg} -C ${EXTRACTED_PKG_PATH}

Extract Zip Package
  [Documentation]  Extracts the specified .zip package and sets the extracted path variable.
  [Arguments]  ${pkg}
  VAR  ${EXTRACTED_PKG_PATH} =  ${EXTRACTED_ZIP_PATH}  scope=SUITE
  Create Directory  ${EXTRACTED_PKG_PATH}
  Run Host Command  unzip ${pkg} -d ${EXTRACTED_PKG_PATH}

Remove Extracted Deb Package
  [Documentation]  Removes the extracted .deb package directory.
  Remove Directory  ${EXTRACTED_DEB_PATH}  recursive=True

Remove Extracted Tar Package
  [Documentation]  Removes the extracted .tar.gz package directory.
  Remove Directory  ${EXTRACTED_TAR_PATH}  recursive=True

Remove Extracted Zip Package
  [Documentation]  Removes the extracted .zip package directory.
  Remove Directory  ${EXTRACTED_ZIP_PATH}  recursive=True

Remove Extracted Packages
  [Documentation]  Removes the extracted packages directory.
  Remove Extracted Deb Package
  Remove Extracted Tar Package
  Remove Extracted Zip Package
  Remove Directory  ${EXTRACTED_PKG_DIR}  recursive=True

Determine APX CLI Bin Path In Extracted Package
  [Documentation]  Determines the path to the apx executable based on the package type.
  Should Not Be Empty  ${DOWNLOADED_PKG_PATH}  msg=Could not determine package path
  ${ext} =  Get Package Extension  ${DOWNLOADED_PKG_PATH}
  IF  '${ext}' == 'deb'
    RETURN  ${EXTRACTED_PKG_PATH}/${EXPECTED_ATPERF_LOCATION_DEB}
  ELSE IF  '${ext}' == 'tar.gz'
    RETURN  ${EXTRACTED_PKG_PATH}/${EXPECTED_ATPERF_LOCATION_TAR}
  ELSE IF  '${ext}' == 'zip'
    RETURN  ${EXTRACTED_PKG_PATH}/${EXPECTED_ATPERF_LOCATION_ZIP}
  ELSE
    Fail  Unsupported package extension: ${ext}
  END

Determine TPIP Root Path In Extracted Package
  [Documentation]  Determines the license terms root path based on the package type.
  ${root} =  Determine Root Path In Extracted Package
  RETURN  ${root}/${EXPECTED_TPIP_LOCATION}

Determine Root Path In Extracted Package
  [Documentation]  Determines the package root path based on the package type.
  Should Not Be Empty  ${DOWNLOADED_PKG_PATH}  msg=Could not determine package path
  ${ext} =  Get Package Extension  ${DOWNLOADED_PKG_PATH}
  IF  '${ext}' == 'deb'
    RETURN  ${EXTRACTED_PKG_PATH}/opt/apx
  ELSE IF  '${ext}' == 'tar.gz'
    RETURN  ${EXTRACTED_PKG_PATH}
  ELSE IF  '${ext}' == 'zip'
    RETURN  ${EXTRACTED_PKG_PATH}
  ELSE
    Fail  Unsupported package extension: ${ext}
  END

Run Extracted APX CLI Command
  [Documentation]  Invoke the apx CLI. Always issue the --json flag. This is a special version of the Run APX CLI
  ...  Command keyword that uses the apx binary in the extracted package instead of the locally built one.
  [Arguments]  ${cmd_string}
  ${bin} =  Determine APX CLI Bin Path In Extracted Package
  ${cmd} =  Catenate  ${bin} ${cmd_string} ${JSON_FLAG}
  Run Host Command  ${cmd}

The Extracted APX CLI Command Is Successful
  [Documentation]  Verifies that the last executed APX CLI command was successful.
  [Arguments]  ${cmd_string}
  Run Extracted APX CLI Command  ${cmd_string}
  The Last Command Succeeded

The Installation Is Working As Expected
  [Documentation]  Verifies that the Arm Performix installation is working as expected, by running a sample of commands.
  The Extracted APX CLI Command Is Successful  help
  The Extracted APX CLI Command Is Successful  recipe info cpu_microarchitecture
  The Extracted APX CLI Command Is Successful  render list
  The Extracted APX CLI Command Is Successful  run list
  The Extracted APX CLI Command Is Successful  ssh list-keys
  The Extracted APX CLI Command Is Successful  target add user@1.2.3.4:22 --name fake_target
  The Extracted APX CLI Command Is Successful  target list
  The Extracted APX CLI Command Is Successful  target remove fake_target
  The Extracted APX CLI Command Is Successful  version

The Extracted Package Contains Valid Main TPIP
  [Documentation]  Verifies that the extracted package contains valid license terms in the main third_party_licenses.txt file.
  ${tpip_root} =  Determine TPIP Root Path In Extracted Package
  ${root_path} =  Join Path  ${tpip_root}  third_party_licenses.txt
  File Should Exist  ${root_path}  msg=Main TPIP license terms file is missing
  ${root_text} =  Get File  ${root_path}
  Should Contain  ${root_text}  ${EXPECTED_TPIP_MAIN}  msg=Main TPIP license terms is invalid

The Extracted Package Contains Valid Tool TPIP
  [Documentation]  Verifies that each packaged per-tool license terms file is present and non-empty.
  ${tpip_root} =  Determine TPIP Root Path In Extracted Package
  ${per_tool_root} =  Join Path  ${tpip_root}  third_party_licenses
  Directory Should Not Be Empty  ${per_tool_root}  msg=Per-tool TPIP license terms directory is empty
  ${tool_dirs} =  List Directories In Directory  ${per_tool_root}
  FOR  ${tool_dir}  IN  @{tool_dirs}
    ${tool_path} =  Join Path  ${per_tool_root}  ${tool_dir}  third_party_licenses.txt
    File Should Exist  ${tool_path}  msg=License terms file for ${tool_dir} is missing
    ${tool_text} =  Get File  ${tool_path}
    Should Not Be Empty  ${tool_text}  msg=License terms for ${tool_dir} is empty
  END

The Main TPIP Contains The Expected Tool Names
  [Documentation]  Verifies that each expected tool has an entry in the main license file.
  ${tpip_root} =  Determine TPIP Root Path In Extracted Package
  ${root_path} =  Join Path  ${tpip_root}  third_party_licenses.txt
  ${root_text} =  Get File  ${root_path}
  ${root_text} =  Replace String  ${root_text}  \r\n  \n
  ${tool_names} =  Split String  ${EXPECTED_TPIP_TOOLS}
  FOR  ${tool}  IN  @{tool_names}
    Should Contain  ${root_text}  ${tool}  msg=Main TPIP missing mention for ${tool}
  END

The License Terms Should Be Valid
  [Documentation]  Verifies that the license terms in the extracted package are valid.
  The Extracted Package Contains Valid Main TPIP
  The Extracted Package Contains Valid Tool TPIP
  The Main TPIP Contains The Expected Tool Names

The Changelog Should Be Present
  [Documentation]  Verifies that the changelog is present in the extracted package.
  ${root} =  Determine Root Path In Extracted Package
  ${changelog_path} =  Join Path  ${root}  ${EXPECTED_CHANGELOG_FILE}
  File Should Exist  ${changelog_path}  msg=Changelog file is missing in the extracted package
