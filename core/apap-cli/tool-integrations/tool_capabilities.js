// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Shared capability contract names. This module contains definitions only.
// Component types identify the persisted CDF schema name and version for each
// capability; CPU_CYCLES identifies the logical capability itself.
const CPU_CYCLES = 'counter.cpu_cycles';
const CPU_COUNTER_CAPABILITY_COMPONENT_TYPE = Object.freeze({
  name: 'tool_capabilities/cpu_counter',
  version: '1.0',
});
const COUNTER_CAPABILITY_COMPONENT_TYPE = Object.freeze({
  name: 'tool_capabilities/counter',
  version: '1.0',
});

module.exports = {
  CPU_CYCLES,
  CPU_COUNTER_CAPABILITY_COMPONENT_TYPE,
  COUNTER_CAPABILITY_COMPONENT_TYPE,
};
