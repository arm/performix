#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import io
import os
import time
from typing import Optional
from zipfile import ZipFile

import blackduck

# The blackduck package is an implementation of the REST API
# described here:
#
#    https://arm.app.blackduck.com/api-doc/public.html
#
# Code based on example from
#
#    https://github.com/blackducksoftware/hub-rest-api-python/blob/master/examples/generate_notices_report_for_project_version.py
#

BLACKDUCK_URL = "https://arm.app.blackduck.com"


def parse_args():
    parser = argparse.ArgumentParser(description="Fetch Black Duck reports")
    parser.add_argument("--token", default=os.environ.get("BLACKDUCK_TOKEN"), help="Black Duck API token")
    parser.add_argument('--project', default=os.environ.get("DETECT_PROJECT_NAME"), help='Black Duck project name')
    parser.add_argument("--version", default=os.environ.get("DETECT_PROJECT_VERSION_NAME"), help="Black Duck project version")
    parser.add_argument("--sbom", help="Path to store SBOM JSON file")
    parser.add_argument("--notices", help="Path to store notices JSON file")
    parser.add_argument("--timeout", help="Timeout in seconds for report generation (default: 600)", type=int, default=600)
    args = parser.parse_args()

    if not args.token:
        parser.error("API token must be provided via --token or BLACKDUCK_TOKEN environment variable")

    if not args.project:
        parser.error("Project name must be provided via --project or DETECT_PROJECT_NAME environment variable") 

    if not args.version:
        parser.error("Project version must be provided via --version or DETECT_PROJECT_VERSION_NAME environment variable")

    if not args.sbom and not args.notices:
        parser.error("At least one of --sbom or --notices must be specified")

    return args


def request_sbom(bd: blackduck.Client, version: dict) -> str:
    params = {
        "reportType": "SBOM",
        "specification": "SPDX_23",
        "reportFormat": "JSON"
    }

    report_url = version['_meta']['href'] + "/sbom-reports"

    r = bd.session.post(report_url, json=params)
    r.raise_for_status()
    return r.headers['Location']


def request_notices(bd: blackduck.Client, version: dict) -> str:
    params = {
        "reportFormat": "JSON",
        "reportType": "VERSION_LICENSE",
        "includeSubprojects": True,
        "categories": [
            "LICENSE_DATA",
            "LICENSE_TEXT",
            "COPYRIGHT_TEXT"
        ]
    }

    report_url = next(l["href"] for l in version['_meta']['links'] if l["rel"] == "licenseReports")
    r = bd.session.post(report_url, json=params)
    r.raise_for_status()
    return r.headers['Location']


def fetch_report(bd: blackduck.Client, status_url: str, output_file: str, end_time: Optional[float]):
    status = None
    while end_time is None or time.time() < end_time:
        status_response = bd.session.get(status_url)
        status_response.raise_for_status()

        status = status_response.json().get('status')
        if status == 'COMPLETED':
            download_url = next(l["href"] for l in status_response.json()['_meta']['links'] if l["rel"] == "download")

            download_response = bd.session.get(download_url)
            download_response.raise_for_status()

            # Reports are downloaded as zip files. Extract the single file inside to the specified location
            with ZipFile(io.BytesIO(download_response.content)) as zip_file:
                # Assume only 1 file (excluding directories)
                member = next(f for f in zip_file.namelist() if not f.endswith("/"))
                # Write output as UTF-8 text (which also converted to native line endings)
                with zip_file.open(member) as f_in, open(output_file, "w", encoding="utf-8") as f_out:
                    text_in = io.TextIOWrapper(f_in, encoding="utf-8")
                    f_out.write(text_in.read())

            return
        elif status != "IN_PROGRESS":
            raise RuntimeError(f"Report generation failed with status: {status}")

        time.sleep(5)

    raise TimeoutError(f"Report generation did not complete in time. Last status: {status}")



def main():
    args = parse_args()
      
    assert args.project
    assert args.version
    assert args.sbom or args.notices

    bd = blackduck.Client(token=args.token, base_url=BLACKDUCK_URL)


    project = next((p for p in bd.get_resource(name='projects') if p["name"] == args.project), None)
    if not project:
        raise ValueError(f"Project '{args.project}' not found in Black Duck")

    version = next((v for v in bd.get_resource("versions", project) if v["versionName"] == args.version), None)
    if not version:
        raise ValueError(f"Version '{args.version}' not found in project '{args.project}'")

    # Request first (to allow concurrent generation on the server)

    if args.sbom:
        print("Requesting SBOM report...")
        sbom_url = request_sbom(bd, version)

    if args.notices:
        print("Requesting notices report...")
        notices_url = request_notices(bd, version)

    # Now fetch and save
    end_time = time.time() + args.timeout

    if args.sbom:
        print("Fetching SBOM report...")
        fetch_report(bd, sbom_url, args.sbom, end_time) # pyright: ignore[reportPossiblyUnboundVariable]

    if args.notices:
        print("Fetching notices report...")
        fetch_report(bd, notices_url, args.notices, end_time) # pyright: ignore[reportPossiblyUnboundVariable]

if __name__ == "__main__":
    main()
