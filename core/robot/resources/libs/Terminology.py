# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from robot.api.deco import keyword, library
from robot.libraries.BuiltIn import BuiltIn
import os, sys
sys.path.append(os.path.join(os.path.dirname(__file__), "../../../scripts"))
from terminology import terminology


@library(scope="SUITE")
class Terminology:
    """
    Terminology provides a Robot keyword to populate the terminology robot variables.
    """
    def __init__(self):
        self._built_in = BuiltIn()

    @keyword("Populate Terms")
    def populate_terms(self):
        """
        Populate the following robot variables (from `terminology.json`):
        - $PRODUCT_FULL_NAME
        - $PRODUCT_BINARY_NAME
        - $AGENT_BINARY_NAME
        - $DAEMON_DIR_NAME
        - $ENV_VAR_PREFIX
        """
        self._set_global("PRODUCT_FULL_NAME", terminology.get_product_full_name())
        self._set_global("PRODUCT_BINARY_NAME", terminology.get_product_binary_name())
        self._set_global("AGENT_BINARY_NAME", terminology.get_agent_binary_name())
        self._set_global("DAEMON_DIR_NAME", terminology.get_daemon_dir_name())
        self._set_global("ENV_VAR_PREFIX", terminology.get_env_var_prefix())

    def _set_global(self, name, value) -> None:
        self._built_in.set_global_variable(f"${{{name}}}", value)
