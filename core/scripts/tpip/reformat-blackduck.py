#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import sys
import json
import textwrap
from typing import Any, TypedDict
from pathlib import Path

BLACKDUCK_COMPONENTS_JSON_KEY = "componentLicenses"
BLACKDUCK_COPYRIGHTS_JSON_KEY = "componentCopyrightTexts"
BLACKDUCK_LICENSES_JSON_KEY = "licenseTexts"

REFORMATTED_HEADER = """\
================================================================================
This file lists the package level copyright and license information for third
party software included in this release of 'Arm Performix'.

The information is grouped in two sections. First section lists out details of
third party software projects, including names of the applicable licenses as
per SPDX format (http://spdx.org/licenses). Second section includes full
license text of all applicable licenses referenced in first section.
================================================================================
"""
REFORMATTED_SECTION_1_HEADER = """\

SECTION 1: THIRD PARTY SOFTWARE PROJECTS
================================================================================
"""
REFORMATTED_SECTION_2_HEADER = """\

SECTION 2: APPLICABLE LICENSES
================================================================================
"""
REFORMATTED_FOOTER = """\

    END OF FILE: """

# ASCII Unit Separator -- used to convert quoted strings to a single 'word' for 
# text wrapping
UNIT_SEPARATOR = chr(31) 
LINE_WIDTH = 80
COMPONENT_FIELDS = ["Name:", "Home-page:", "License(s):", "Copyright(s):", "Source(s):"]
FIELD_WIDTH = max(len(k) for k in COMPONENT_FIELDS) + 1

class Component(TypedDict):
    name: str
    version: str
    homepage: str
    licenses: str
    copyrights: list[str]

class LicenseInfo(TypedDict):
    license: str
    components: list[str]
    text: str

def reformatted_component(name: str, homepage: str, licenses: str,
                          copyrights: str, sources: str) -> str:
    return  f"{COMPONENT_FIELDS[0]:<{FIELD_WIDTH}}{name}\n" \
            f"{COMPONENT_FIELDS[1]:<{FIELD_WIDTH}}{homepage}\n" \
            f"{COMPONENT_FIELDS[2]:<{FIELD_WIDTH}}{licenses}. See later section for a copy of license text.\n" \
            f"{COMPONENT_FIELDS[3]:<{FIELD_WIDTH}}{copyrights}\n" \
            f"{COMPONENT_FIELDS[4]:<{FIELD_WIDTH}}{sources}\n" \
            f"{'-' * LINE_WIDTH}\n"

def reformatted_copyrights(copyrights: list[str]) -> str:
    if not copyrights:
        return ""
    
    # First entry does not need indentation, but subsequent entries do
    return "\n".join(
        [copyrights[0]] + [f"{'':<{FIELD_WIDTH}}{c}" for c in copyrights[1:]]
    ) if copyrights else ""

def reformatted_license(idx: int, license_name: str, components: list[str], license_text: str) -> str:
    prefix = f"{idx}) License text ({license_name}) for "
    
    # Convert 'component version' string into 'component<UNIT_SEPARATOR>version'
    # So that, textwrap can treat it as a single word to correctly wrap lines
    # We convert it back to 'component version' afterwards  
    protected = []
    for component in components:
        protected.append(f"'{' '.join(component.split()).replace(' ', UNIT_SEPARATOR)}'")

    components_header = textwrap.fill(
        ", ".join(protected),
        width=LINE_WIDTH,
        initial_indent=prefix,
        subsequent_indent="   ",
        break_long_words=False,
        break_on_hyphens=False,
    ).replace(UNIT_SEPARATOR, ' ')

    return  f"{components_header}\n" \
            f"{license_text}\n" \
            f"\n" \
            f"{'=' * LINE_WIDTH}\n" \

def parse_blackduck_components(blackduck_json: dict[str, Any]) -> list[Component]:
    '''
    Parses the given Python dict obtained from the Blackduck JSON export and
    returns a list of dicts with "name", "version", "homepage", "licenses"
    for each software component. Some entries might have an empty "homepage".
    '''
    components: list[Component] = []
    
    if BLACKDUCK_COMPONENTS_JSON_KEY not in blackduck_json:
        log(f"Error: Input JSON does not contain '{BLACKDUCK_COMPONENTS_JSON_KEY}' key")
        exit(1)

    # Parse each software component
    for entry in blackduck_json[BLACKDUCK_COMPONENTS_JSON_KEY]:
        name = entry.get("component", {}).get("projectName", "N/A")
        version = entry.get("component", {}).get("versionName", "")
        homepage = entry.get("component", {}).get("homeUrl", "")
        licenses = ", ".join(license.get("name", "N/A") for license in entry.get("licenses", []))
            
        components.append({
            "name": name,
            "version": version,
            "homepage": homepage,
            "licenses": licenses,
            "copyrights": [],
        })
    return components

def sanitise_copyrights(copyrights: list[str]) -> None:
    '''
    Sanitises the given list of copyright texts in-place by doing the following:
    - keep only text before first newline
    - prune lines beginning with "(c)" (ignoring leading/trailing whitespace)
    - prune empty lines and "N/A" entries
    - remove duplicate lines while preserving original order
    '''
    sanitized: list[str] = []
    seen: set[str] = set()

    for entry in copyrights:
        line = entry.strip().split("\n", 1)[0].strip()
   
        if not line:
            continue
        if line.upper() == "N/A":
            continue
        if line.lower().startswith("(c)"):
            continue
        if line in seen:
            continue

        seen.add(line)
        sanitized.append(line)

    copyrights[:] = sanitized

def parse_blackduck_copyrights(blackduck_json: dict[str, Any], components: list[Component], ) -> None:
    '''
    Parses the copyright texts in the given Python dict obtained from the
    Blackduck JSON export and populates each component's "copyrights" field if
    found.
    '''
    if BLACKDUCK_COPYRIGHTS_JSON_KEY not in blackduck_json:
        log(f"Error: Input JSON does not contain '{BLACKDUCK_COPYRIGHTS_JSON_KEY}' key")
        exit(1)
    
    for entry in blackduck_json[BLACKDUCK_COPYRIGHTS_JSON_KEY]:
        name = entry.get("componentVersionSummary", {}).get("projectName", "N/A")
        version = entry.get("componentVersionSummary", {}).get("versionName", "")

        for component in components:
            if component.get("name") == name and component.get("version", "") == version:
                copyright_texts = entry.get("copyrightTexts", [])
                sanitise_copyrights(copyright_texts)
                component["copyrights"] = copyright_texts
                break
        else:
            log(f"Warning: Could not find a matching component for copyright entry '{name} {version}'")

def parse_blackduck_licenses(blackduck_dict: dict[str, Any]) -> list[LicenseInfo]:
    '''
    Parses the license texts from the give Pyhon dict obtained from the
    Blackduck JSON export and returns a list of dicts in the form of:
    
    [
    "MIT License": {"components": ["cmpA", "cmpB"], "text": "Lorem ipsum..."}
    "Apache License 2.0": {"components": ["cmpC"], "text": "Lorem ipsum..."}
    ]
    '''
    licenses: list[LicenseInfo] = []

    if BLACKDUCK_LICENSES_JSON_KEY not in blackduck_dict:
        log(f"Error: Input JSON does not contain '{BLACKDUCK_LICENSES_JSON_KEY}' key")
        exit(1)
    
    # The first entry is simply the first line after the sentinal that's not
    # empty
    for entry in blackduck_dict[BLACKDUCK_LICENSES_JSON_KEY]:
        license_name = entry.get("name", "N/A")
        components = [component.get("projectName", "N/A") + " " + component.get("versionName", "") for component in entry.get("components", [])]
        license_text = entry.get("text", "")

        log(f"Parsed license '{license_name}' with {len(components)} components and text length of {len(license_text)} characters")
        licenses.append({
            "license": license_name,
            "components": components,
            "text": license_text
        })

    return licenses

def log(msg):
    print(f"[reformat-blackduck] {msg}")

def main():
    # Parse args --input and --output
    parser = argparse.ArgumentParser(description="Reformat the BlackDuck notices report in JSON format to match Arm Performix's third_party_licenses.txt format")
    parser.add_argument("--input", required=True, help="Path to the BlackDuck report in JSON format")
    parser.add_argument("--output", required=True, help="Path to write the reformatted report to")
    parser.add_argument("--remove-after", action="store_true", help="Delete the input file after successful reformatting")
    args = parser.parse_args()

    if not Path(args.input).is_file():
        log(f"Warning: input file not found at '{args.input}'. Skipping reformat (non-release build).")
        return

    log(f"Reformatting BlackDuck report '{args.input}' to '{args.output}'")
    with open(args.input, "r", encoding="utf-8") as f:
        # Read into a Python dict from JSON
        blackduck_json = None
        try:
            blackduck_json = json.load(f)
        except json.JSONDecodeError as exc:
            log(f"Error: Failed to parse input file as JSON: {exc}")
            exit(1)

        # 1. Parse Components
        components = parse_blackduck_components(blackduck_json)
        log(f"Parsed {len(components)} components from the input file")
        
        # 2. Parse Copyrights
        parse_blackduck_copyrights(blackduck_json, components)
        log(f"Parsed copyright text for {sum(1 for component in components if component['copyrights'])} out of {len(components)} components")

        # Parse license texts
        licenses = parse_blackduck_licenses(blackduck_json)
        log(f"Parsed {len(licenses)} licenses from the input file")

    # Write reformatted output to args.output
    with open(args.output, "w", encoding="utf-8") as f:
        # Header
        f.write(REFORMATTED_HEADER)

        # Section 1
        f.write(REFORMATTED_SECTION_1_HEADER)
        for component in components:
            copyrights_str = reformatted_copyrights(component["copyrights"])

            entry = reformatted_component(
                name=f"{component['name']} {component['version']}".strip(),
                homepage=component["homepage"],
                licenses=component["licenses"],
                copyrights=copyrights_str,
                sources=component["homepage"])
            # Blackduck doesn't report sources. So, we copy homepage for now
            f.write("\n")
            f.write(entry)

        # Section 2
        f.write(REFORMATTED_SECTION_2_HEADER)
        for idx, license in enumerate(licenses, start=1):
            entry = reformatted_license(
                idx=idx,
                license_name=license["license"],
                components=license["components"],
                license_text=license["text"])
            f.write("\n")
            f.write(entry)
        # Footer
        f.write(REFORMATTED_FOOTER + args.output)
    log(f"Successfully wrote reformatted report to '{args.output}'")

    if args.remove_after:
        Path(args.input).unlink()
        log(f"Deleted input file '{args.input}'")

        
if __name__ == "__main__":
    try:
        main()
        log("Success")
    except Exception as exc:  # pragma: no cover - defensive top-level guard
        print(f"Error: {exc}", file=sys.stderr)
        sys.exit(1)
