# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import json
import sys
from jsonschema import validate, Draft202012Validator, exceptions

def print_validation_error(error: exceptions.ValidationError, matrix_data: dict):
    print("Validation failed")
    print(f"Error: {error.message}")
    
    if error.absolute_path:
        path = " -> ".join(str(p) for p in error.absolute_path)
        print(f"Path: {path}")
    
    if error.instance is not None:
        print(f"Property: {error.instance}")

def main(matrix_file, schema_file):
    with open(matrix_file, "r") as f:
        matrix_data = json.load(f)

    with open(schema_file, "r") as f:
        schema_data = json.load(f)

    try:
        validate(instance=matrix_data, schema=schema_data, cls=Draft202012Validator)
        print("Validation succeeded.")
    except exceptions.ValidationError as e:
        print_validation_error(e, matrix_data)
        sys.exit(1)

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print("Usage: python3 validate_matrix.py <matrix>.json <matrix schema>.json")
        sys.exit(1)

    matrix_file = sys.argv[1]
    schema_file = sys.argv[2]
    main(matrix_file, schema_file)
