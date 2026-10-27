// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

function describe(value) {
  return value instanceof Error ? String(value) : JSON.stringify(value);
}

function assertEqual(actual, expected, message) {
  if (actual !== expected) {
    throw new Error(
      `${message}: expected ${describe(expected)}, got ${describe(actual)}`,
    );
  }
}
exports.createFixture = function (cpuCounters) {
  function catalogue(name, event, cluster) {
    return {
      counters: `<counters><counter name="${name}_ccnt"/><cluster id="${cluster}" name="${name}"/><cpu id="${cluster}" cluster="${cluster}"/></counters>`,
      events: `<events><category per_cpu="yes" counter_set="${name}_cnt"><event counter="${name}_ccnt" event="${event}" units="cycles"/></category></events>`,
    };
  }
  const arm = catalogue('ARMv9_Neoverse_V2', '0x11', 0);
  const x86 = catalogue('Perf_Hardware', '0', 1);
  function combine(...items) {
    return Object.fromEntries(
      ['counters', 'events'].map((key) => [
        key,
        `<${key}>` +
          items
            .map((item) =>
              item[key].replace(new RegExp(`^<${key}>|</${key}>$`, 'g'), ''),
            )
            .join('') +
          `</${key}>`,
      ]),
    );
  }
  const unavailable = { counters: '<counters/>', events: '<events/>' };
  const empty = {
    required: [],
    optional: [],
  };
  const commands = [];
  const saved = [];
  const warnings = [];
  const engine = {
    isNeoprofTimelineEnabled: () => true,
    createTempDir: async () => '/tmp/cpu',
    execCommand: async (args) => {
      commands.push(args);
      return {
        rc: 0,
        stdout: args[0] === 'cat' ? cpuCounters.EMPTY_XML : '',
        stderr: '',
      };
    },
    addToolCapability: async (id, type, data) => saved.push(data),
    writeCatalogMessage: (...args) => warnings.push(args),
  };
  const registryJSON = JSON.stringify({
    counter_groups: [
      {
        key_type: 8,
        counters: [
          {
            id: 10,
            files: [
              10000000000, 5000000000, 1000000000, 500000000, 100000000,
              50000000, 10000000,
            ].map((bin_duration) => ({
              bin_duration,
              relative_path: `report-new/apx/timeline/key_type=8/series_id=10/bin_duration=${bin_duration}/counter.parquet`,
            })),
          },
        ],
      },
    ],
  });
  const verificationEngine = {
    ...engine,
    getLocality: () => 'target',
    log: () => {},
    execCommand: async (args) => ({
      rc: 0,
      stdout: args[1]?.endsWith('counter_series_files.json')
        ? registryJSON
        : args[0] === 'cat'
          ? '<captured version="1"><counters><counter key="0xa" type="ARMv9_Neoverse_V2_ccnt" event="0x11"/></counters></captured>'
          : JSON.stringify({ valid: true, device_nos: [0] }),
    }),
  };
  function request(policy, text) {
    return {
      params: { mode: 'samples' },
      metadata: {},
      discovery: { capabilities: { neoprof_cpu_counter_catalogue: text } },
      capabilityRequest: {
        required: policy === 'required' ? [cpuCounters.ID] : [],
        optional: policy === 'optional' ? [cpuCounters.ID] : [],
      },
    };
  }

  return {
    arm,
    x86,
    combine,
    unavailable,
    empty,
    commands,
    saved,
    warnings,
    engine,
    verificationEngine,
    request,
    assertEqual,
  };
};
