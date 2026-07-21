# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation  A resource file containing keywords for downloading Arm Performix packages.
Library        ../libs/NetworkUtils.py
Resource       common.resource
Resource       install.resource


*** Variables ***
${GITHUB_API_URL}       api.github.com
${GITHUB_ORG}           Arm-Debug
${GITHUB_REPO}          performix
${RELEASES_API}         https://${GITHUB_API_URL}/repos/${GITHUB_ORG}/${GITHUB_REPO}/releases
${RELEASES_API_LATEST}  ${RELEASES_API}/latest
${TAGS_API}             ${RELEASES_API}/tags
${ASSETS_API}           ${RELEASES_API}/assets
${DOWNLOADED_PKG_DIR}   ${TEMPDIR}/apx
${DOWNLOADED_PKG_PATH}  ${EMPTY}


*** Keywords ***
GitHub Token Is Set
  [Documentation]  Checks that a GitHub token is set in the environment variables and skips if not.
  Skip If  '%{GITHUB_TOKEN}' == '${EMPTY}'  msg=GITHUB_TOKEN environment variable is not set, skipping

GitHub Is Reachable
  [Documentation]  Pings GitHub to check it is reachable, skips if not.
  ${reachable} =  Run Keyword And Return Status  Ping GitHub
  Skip If  not ${reachable}  msg=GitHub is not reachable, skipping

Ping GitHub
  [Documentation]  Pings GitHub to check it is reachable.
  Ping Host  ${GITHUB_API_URL}  count=1  timeout=5

Get Latest Release Tag
  [Documentation]  Gets the latest release tag from GitHub using the REST API.
  Run Keyword Without Logging  Run Host Command  curl -L -H "Authorization: Bearer %{GITHUB_TOKEN}" ${RELEASES_API_LATEST} | jq -r .tag_name
  ${tag} =  Strip String  ${G_LAST_RESULT.stdout}
  VAR  ${TAG}  ${tag}  scope=SUITE
  Should Not Be Empty  ${TAG}  msg=Failed to get latest release tag from GitHub
  RETURN  ${TAG}

Get Release Asset ID
  [Documentation]  Gets the asset ID of the specified package for the specified release tag from GitHub using the REST API.
  [Arguments]  ${tag}  ${pkg}
  Run Keyword Without Logging  Run Host Command
  ...  curl -s -H "Authorization: Bearer %{GITHUB_TOKEN}" ${TAGS_API}/${tag} | jq -r '.assets[] | select(.name == "${pkg}").id'
  VAR  ${ASSET_ID}  ${G_LAST_RESULT.stdout}  scope=SUITE
  Should Not Be Empty  ${ASSET_ID}  msg=Failed to get release asset ID from GitHub
  RETURN  ${ASSET_ID}

Download Asset By ID
  [Documentation]  Downloads a release asset from GitHub using the REST API and asset ID. Creates destination directory
  ...  if it does not already exist.
  [Arguments]  ${asset_id}  ${dst_dir}  ${pkg_name}
  Log  Downloading package ${pkg_name} with asset ID ${asset_id} to ${dst_dir}/${pkg_name}
  Create Directory  ${dst_dir}
  Run Keyword Without Logging  Run Host Command
  ...  curl -L -H "Authorization: Bearer %{GITHUB_TOKEN}" -H "Accept: application/octet-stream" -o ${dst_dir}/${pkg_name} ${ASSETS_API}/${asset_id}
  File Should Exist  ${dst_dir}/${pkg_name}
  Get File Size  ${dst_dir}/${pkg_name}

The Latest Debian Package Is Downloaded Successfully
  [Arguments]  ${os}  ${arch}
  Download Latest Debian Package  ${os}  ${arch}
  The Last Command Succeeded

Download Latest Debian Package
  [Documentation]  Downloads the latest .deb package from GitHub.
  [Arguments]  ${os}  ${arch}
  ${tag} =  Get Latest Release Tag
  Download Package  ${tag}  ${os}  ${arch}  deb

The Latest Tar Package Is Downloaded Successfully
  [Arguments]  ${os}  ${arch}
  Download Latest Tar Package  ${os}  ${arch}
  The Last Command Succeeded

Download Latest Tar Package
  [Documentation]  Downloads the latest .tar package from GitHub.
  [Arguments]  ${os}  ${arch}
  ${tag} =  Get Latest Release Tag
  Download Package  ${tag}  ${os}  ${arch}  tar.gz

The Latest Zip Package Is Downloaded Successfully
  [Arguments]  ${os}  ${arch}
  Download Latest Zip Package  ${os}  ${arch}
  The Last Command Succeeded

Download Latest Zip Package
  [Documentation]  Downloads the latest .zip package from GitHub.
  [Arguments]  ${os}  ${arch}
  ${tag} =  Get Latest Release Tag
  Download Package  ${tag}  ${os}  ${arch}  zip

Download Package
  [Documentation]  Downloads an asset from GitHub using the REST API. Fetches the version specified by tag (must
  ...  include leading 'v'). Stores it in a subdir within the OS temporary directory unless a destination is specified.
  [Arguments]  ${tag}  ${os}  ${arch}  ${extension}  ${destination}=${DOWNLOADED_PKG_DIR}
  ${pkg_name} =  Build APX Package Name  ${tag}  ${os}  ${arch}  ${extension}
  ${asset_id} =  Get Release Asset ID  ${tag}  ${pkg_name}
  Download Asset By ID  ${asset_id}  ${destination}  ${pkg_name}
  VAR  ${DOWNLOADED_PKG_PATH}  ${destination}/${pkg_name}  scope=SUITE

Select First File
  [Documentation]  Selects the first file from the provided list.
  [Arguments]  ${files}
  RETURN  ${files}[0]

Build APX Package Name
  [Documentation]  Builds the APX package name based on the tag, OS, architecture, and extension. Removes the
  ...  leading 'v' from the tag to get the version.
  [Arguments]  ${tag}  ${os}  ${arch}  ${extension}
  VAR  ${version}  ${tag}[1:]  scope=LOCAL
  RETURN  apx_${version}_${os}_${arch}.${extension}

Remove Download Packages
  [Documentation]  Removes the downloaded packages directory.
  Remove Directory  ${DOWNLOADED_PKG_DIR}  recursive=True
