// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const cpuCounters = require('../../../../../apap-cli/tool-integrations/neoprof_cpu_counters');
const { createFixture } = require('./harness');
function streamChunks(chunks) {
  let index = 0;
  return {
    [Symbol.asyncIterator]() {
      return this;
    },
    async next() {
      return index < chunks.length
        ? { value: chunks[index++], done: false }
        : { done: true };
    },
  };
}
function commandHandle(stdout, stderr = '', exitCode = 0) {
  return {
    stdout: streamChunks([stdout]),
    stderr: streamChunks([stderr]),
    wait: async () => ({ exitCode }),
    kill: async () => {},
  };
}
async function expectInternalFailure(stage) {
  const {
    arm,
    engine,
    verificationEngine,
    request,
    saved,
    warnings,
    assertEqual,
  } = createFixture(cpuCounters);
  const ctx = request('optional', arm);
  await cpuCounters.prepare(engine, ctx);
  const original = new Error(`Internal failure: ${stage}`);
  const broken = { ...verificationEngine };
  let capabilityID = (entry) => `series.${entry.series_id}`;
  let emitSeries = () => {};
  if (stage === 'capability_id')
    capabilityID = () => {
      throw original;
    };
  if (stage === 'emit_series')
    emitSeries = async () => {
      throw original;
    };
  if (stage === 'engine_command')
    broken.execCommand = async () => {
      throw original;
    };
  if (stage === 'host_file') {
    broken.getLocality = () => 'host';
    broken.readHostFile = async () => {
      throw original;
    };
  }
  if (stage === 'outcome_publication')
    broken.addToolCapability = async () => {
      throw original;
    };
  let caught;
  try {
    await cpuCounters.verify(
      broken,
      ctx,
      '/capture',
      [{ key_type: 8, series_id: 10 }],
      capabilityID,
      '/converter',
      emitSeries,
    );
  } catch (error) {
    caught = error;
  }
  assertEqual(
    caught === original,
    true,
    `${stage} must propagate the original error`,
  );
  assertEqual(
    saved.length,
    0,
    `${stage} must not be saved as output_unverified`,
  );
  assertEqual(
    warnings.length,
    0,
    `${stage} must not become an optional-counter warning`,
  );
}

const scenarios = {
  discovery_drains_both_streams: async () => {
    const { arm, assertEqual } = createFixture(cpuCounters);
    const engine = {
      log: () => {},
      startProcess: async (args) => {
        let started = 0;
        let finished = 0;
        let releaseReads;
        let releaseExit;
        const bothReading = new Promise((resolve) => {
          releaseReads = resolve;
        });
        const exited = new Promise((resolve) => {
          releaseExit = resolve;
        });
        function stream(text) {
          let read = false;
          return {
            [Symbol.asyncIterator]() {
              return this;
            },
            async next() {
              if (read) {
                if (++finished === 2) releaseExit();
                return { done: true };
              }
              read = true;
              // Model backpressure: neither pipe can finish until the other
              // pipe has a reader, and the process cannot exit until both drain.
              if (++started === 2) releaseReads();
              await bothReading;
              return { value: text, done: false };
            },
          };
        }
        return {
          stdout: stream(arm[args[2].split('.')[0]]),
          stderr: stream('collector diagnostic'),
          wait: async () => {
            await exited;
            return { exitCode: 0 };
          },
          kill: async () => {
            releaseReads();
            releaseExit();
          },
        };
      },
    };
    let timer;
    try {
      const discovered = await Promise.race([
        cpuCounters.discover(engine, { metadata: {} }, '/collector'),
        new Promise((_, reject) => {
          timer = setTimeout(
            () =>
              reject(
                new Error(
                  'Discovery stalled: both output streams must be drained before waiting for exit',
                ),
              ),
            2000,
          );
        }),
      ]);
      assertEqual(
        discovered[cpuCounters.ID].state,
        'available',
        'Concurrent drains complete discovery',
      );
      assertEqual(
        discovered.neoprof_cpu_counter_catalogue.counters,
        arm.counters,
        'Complete counters',
      );
      assertEqual(
        discovered.neoprof_cpu_counter_catalogue.events,
        arm.events,
        'Complete events',
      );
    } finally {
      clearTimeout(timer);
    }
  },
  large_streamed_catalogues: async () => {
    const { arm, assertEqual } = createFixture(cpuCounters);
    const padding = '<!--' + 'x'.repeat(4 * 1024 * 1024) + '-->';
    const catalogue = {
      counters: arm.counters.replace('</counters>', padding + '</counters>'),
      events: arm.events.replace('</events>', padding + '</events>'),
    };
    for (const asPrivileged of [false, true]) {
      const calls = [];
      const discovered = await cpuCounters.discover(
        {
          startProcess: async (args, opts) => {
            assertEqual(opts.asPrivileged, asPrivileged, 'Preserve privilege');
            assertEqual(opts.stdout.redirect, 'stream', 'Stream stdout');
            assertEqual(opts.stderr.redirect, 'stream', 'Stream stderr');
            calls.push(args[2]);
            const text =
              'WARN: catalogue warning\n' + catalogue[args[2].split('.')[0]];
            const chunks = [text.slice(0, 3), text.slice(3, 29)];
            for (let i = 29; i < text.length; i += 4093)
              chunks.push(text.slice(i, i + 4093));
            const handle = commandHandle('');
            handle.stdout = streamChunks(chunks);
            return handle;
          },
        },
        { metadata: { neoprofAsPrivileged: asPrivileged } },
        '/collector',
      );
      assertEqual(
        calls.join(','),
        'counters.xml,events.xml',
        'Sequential discovery',
      );
      assertEqual(
        discovered[cpuCounters.ID].state,
        'available',
        'Resolve CPU cycles',
      );
      assertEqual(
        discovered.neoprof_cpu_counter_catalogue.counters,
        catalogue.counters,
        'Complete counters',
      );
      assertEqual(
        discovered.neoprof_cpu_counter_catalogue.events,
        catalogue.events,
        'Complete events',
      );
    }
  },
  streamed_discovery_failures: async () => {
    const { arm, assertEqual } = createFixture(cpuCounters);
    for (const stage of ['launch', 'stdout', 'stderr', 'wait']) {
      const original = new Error(stage);
      let kills = 0;
      const cleanupWarnings = [];
      let launches = 0;
      let caught;
      try {
        await cpuCounters.discover(
          {
            log: (level, text) => cleanupWarnings.push({ level, text }),
            startProcess: async () => {
              launches++;
              if (stage === 'launch') throw original;
              const handle = commandHandle(arm.counters);
              handle.kill = async () => {
                kills++;
                throw new Error('cleanup failure');
              };
              if (stage === 'wait') {
                handle.wait = async () => {
                  throw original;
                };
              } else {
                let reads = 0;
                handle[stage] = {
                  [Symbol.asyncIterator]() {
                    return this;
                  },
                  async next() {
                    if (reads++ === 0)
                      return { value: '<partial', done: false };
                    throw original;
                  },
                };
              }
              return handle;
            },
          },
          { metadata: {} },
          '/collector',
        );
      } catch (error) {
        caught = error;
      }
      assertEqual(caught, original, 'Preserve ' + stage + ' error');
      assertEqual(
        cleanupWarnings.length,
        stage === 'launch' ? 0 : 1,
        'Report termination failure',
      );
      if (cleanupWarnings.length) {
        assertEqual(
          cleanupWarnings[0].level,
          'warn',
          'Cleanup diagnostic level',
        );
        assertEqual(
          cleanupWarnings[0].text.includes('cleanup failure'),
          true,
          'Retain cleanup cause',
        );
      }
      assertEqual(kills, stage === 'launch' ? 0 : 1, 'Clean up ' + stage);
      assertEqual(launches, 1, 'Do not continue discovery after ' + stage);
    }
    for (const [stdout, stderr, rc] of [
      [arm.counters, '', 1],
      ['<broken', '', 0],
    ]) {
      let caught;
      try {
        await cpuCounters.discover(
          { startProcess: async () => commandHandle(stdout, stderr, rc) },
          { metadata: {} },
          '/collector',
        );
      } catch (error) {
        caught = error;
      }
      assertEqual(
        caught?.code,
        'common.UNKNOWN_ERROR',
        'Invalid catalog must fail',
      );
    }
  },
  capability_id_errors_propagate: () => expectInternalFailure('capability_id'),
  output_registration_errors_propagate: () =>
    expectInternalFailure('emit_series'),
  engine_command_errors_propagate: () =>
    expectInternalFailure('engine_command'),
  host_file_api_errors_propagate: () => expectInternalFailure('host_file'),
  outcome_publication_errors_propagate: () =>
    expectInternalFailure('outcome_publication'),
  malformed_capture_and_registry_are_unverified: async () => {
    for (const artifact of [
      'captured_xml',
      'registry_json',
      'registry_shape',
      'registry_exit_code',
    ]) {
      const {
        arm,
        engine,
        verificationEngine,
        request,
        saved,
        warnings,
        assertEqual,
      } = createFixture(cpuCounters);
      const ctx = request('optional', arm);
      await cpuCounters.prepare(engine, ctx);
      const broken = {
        ...verificationEngine,
        execCommand: async (args) => {
          if (artifact === 'captured_xml' && args[0] === 'cat')
            return { rc: 0, stdout: '<broken' };
          if (args[1]?.endsWith('counter_series_files.json')) {
            if (artifact === 'registry_json')
              return { rc: 0, stdout: '{broken' };
            if (artifact === 'registry_shape')
              return { rc: 0, stdout: '[null]' };
            if (artifact === 'registry_exit_code') return { rc: 1, stdout: '' };
          }
          return verificationEngine.execCommand(args);
        },
      };
      await cpuCounters.verify(
        broken,
        ctx,
        '/capture',
        [{ key_type: 8, series_id: 10 }],
        (entry) => `series.${entry.series_id}`,
        '/converter',
      );
      assertEqual(
        saved[0].state,
        'incomplete',
        `${artifact} is unverified output`,
      );
      assertEqual(
        warnings.length,
        1,
        `${artifact} reports the optional outcome`,
      );
    }
  },
  malformed_registry_entries_reject_partial_rows: async () => {
    for (const [name, addInvalidEntry] of [
      ['group', (groups) => groups.push({ key_type: 8, counters: null })],
      ['counter', (groups) => groups[0].counters.push({ id: 11, files: null })],
      [
        'file',
        (groups) =>
          groups[0].counters[0].files.push({
            bin_duration: -1,
            relative_path: 'invalid.parquet',
          }),
      ],
    ]) {
      const {
        arm,
        engine,
        verificationEngine,
        request,
        saved,
        warnings,
        assertEqual,
      } = createFixture(cpuCounters);
      const ctx = request('optional', arm);
      await cpuCounters.prepare(engine, ctx);
      const published = [];
      const broken = {
        ...verificationEngine,
        execCommand: async (args) => {
          const result = await verificationEngine.execCommand(args);
          if (!args[1]?.endsWith('counter_series_files.json')) return result;
          const registry = JSON.parse(result.stdout);
          addInvalidEntry(registry.counter_groups);
          return { ...result, stdout: JSON.stringify(registry) };
        },
      };

      const rows = await cpuCounters.loadSeriesRegistry(
        broken,
        ctx,
        '/capture',
      );
      await cpuCounters.verify(
        broken,
        ctx,
        '/capture',
        [{ key_type: 8, series_id: 10 }],
        (entry) => `series.${entry.series_id}`,
        '/converter',
        (series) => published.push(series),
      );

      assertEqual(
        rows,
        undefined,
        `Invalid ${name} rejects the whole registry`,
      );
      assertEqual(
        saved[0].state,
        'incomplete',
        `Invalid ${name} is unverified`,
      );
      assertEqual(published.length, 0, `Invalid ${name} prevents publication`);
      assertEqual(
        warnings.length,
        1,
        `Invalid ${name} reports the optional outcome`,
      );
    }
  },
  malformed_or_missing_series_output_is_unverified: async () => {
    const {
      arm,
      saved,
      warnings,
      engine,
      verificationEngine,
      request,
      assertEqual,
    } = createFixture(cpuCounters);
    for (const stdout of ['not-json', '{}', '[{}]']) {
      const ctx = request('optional', arm);
      await cpuCounters.prepare(engine, ctx);
      const broken = {
        ...verificationEngine,
        execCommand: async (args) =>
          args[0] === 'cat' || args[1]?.endsWith('counter_series_files.json')
            ? verificationEngine.execCommand(args)
            : { rc: 0, stdout },
      };
      await cpuCounters.verify(
        broken,
        ctx,
        '/capture',
        [{ key_type: 8, series_id: 10 }],
        (m) => 'series.' + m.series_id,
        '/converter',
      );
      assertEqual(
        saved[saved.length - 1].state,
        'incomplete',
        `Malformed or empty output must remain incomplete (stdout: ${stdout})`,
      );
    }
    assertEqual(
      warnings.length,
      3,
      'Each unusable optional output must produce a warning',
    );
  },
  verified_output_is_collected_and_published: async () => {
    const { arm, engine, verificationEngine, request, saved, assertEqual } =
      createFixture(cpuCounters);
    const ctx = request('required', arm);
    await cpuCounters.prepare(engine, ctx);
    const emitted = [];
    await cpuCounters.verify(
      verificationEngine,
      ctx,
      '/capture',
      [{ key_type: 8, series_id: 10 }],
      (m) => 'series.' + m.series_id,
      '/converter',
      (...args) => emitted.push(args),
    );
    assertEqual(emitted.length, 1, 'Only the verified series is published');
    assertEqual(emitted[0][0].series_id, 10, 'Published series has ID 10');
    assertEqual(saved[0].state, 'collected', 'Verified output is collected');
    assertEqual(
      saved[0].payload.bindings[0].seriesCapabilityId,
      'series.10',
      'Collected outcome references the published series',
    );
  },
  heterogeneous_known_pmus_and_duplicate_selection: async () => {
    const { arm, x86, combine, assertEqual } = createFixture(cpuCounters);
    const duplicate = {
      ...arm,
      counters: arm.counters
        .replaceAll('id="0"', 'id="2"')
        .replaceAll('cluster="0"', 'cluster="2"'),
    };
    const resolved = cpuCounters.resolveCatalogue(combine(arm, x86, duplicate));
    assertEqual(
      resolved.native.length,
      2,
      'Duplicate PMU sections must not duplicate native selections',
    );
  },
  unknown_and_ambiguous_pmus_do_not_collect_subset: async () => {
    const { arm, assertEqual } = createFixture(cpuCounters);
    const missing = {
      ...arm,
      counters: arm.counters.replace(
        '</counters>',
        '<cluster id="1" name="Unknown"/><cpu id="1" cluster="1"/></counters>',
      ),
    };
    assertEqual(
      cpuCounters.resolveCatalogue(missing).capability.payload.reason,
      'not_supported',
      'A missing group counter prevents partial selection',
    );
    const conflicting = {
      ...arm,
      events: arm.events.replace(
        '</events>',
        '<category per_cpu="yes" counter_set="ARMv9_Neoverse_V2_cnt"><event counter="ARMv9_Neoverse_V2_ccnt" event="0xff" units="cycles"/></category></events>',
      ),
    };
    assertEqual(
      cpuCounters.resolveCatalogue(conflicting).capability.payload.reason,
      'ambiguous',
      'Conflicting event identities are ambiguous',
    );
  },
  invalid_scope_and_topology_do_not_resolve: async () => {
    const { arm, assertEqual } = createFixture(cpuCounters);
    for (const [label, catalogue, expected] of [
      [
        'uncore',
        { ...arm, events: arm.events.replace('per_cpu="yes"', 'per_cpu="no"') },
        'not_supported',
      ],
      [
        'wrong counter set',
        {
          ...arm,
          events: arm.events.replace(
            'counter_set="ARMv9_Neoverse_V2_cnt"',
            'counter_set="Unrelated_cnt"',
          ),
        },
        'not_supported',
      ],
    ])
      assertEqual(
        cpuCounters.resolveCatalogue(catalogue).capability.payload.reason,
        expected,
        label,
      );
    for (const [label, catalogue] of [
      [
        'missing cpu membership',
        { ...arm, counters: arm.counters.replace(/<cpu[^>]+\/>/, '') },
      ],
      [
        'unknown group reference',
        {
          ...arm,
          counters: arm.counters.replace('cluster="0"', 'cluster="1"'),
        },
      ],
      [
        'missing event identity',
        { ...arm, events: arm.events.replace('event="0x11"', '') },
      ],
    ]) {
      let failed = false;
      try {
        cpuCounters.resolveCatalogue(catalogue);
      } catch (e) {
        failed = e.code === 'common.UNKNOWN_ERROR';
      }
      assertEqual(failed, true, label + ' must fail discovery');
    }
  },
  event_descriptions_are_not_discovery_errors: async () => {
    const { arm, assertEqual } = createFixture(cpuCounters);
    const catalogue = {
      ...arm,
      events: arm.events.replace(
        'units="cycles"',
        'units="cycles" description="Pre-decode error &amp; ERROR: valid text"',
      ),
    };
    const discovered = await cpuCounters.discover(
      {
        startProcess: async (args) =>
          commandHandle(catalogue[args[2].split('.')[0]]),
      },
      { metadata: {} },
      '/collector',
    );
    assertEqual(
      discovered[cpuCounters.ID].state,
      'available',
      'Descriptions are XML data, not command failures',
    );
  },
  stderr_diagnostics_do_not_block_cpu_discovery: async () => {
    const { arm, assertEqual } = createFixture(cpuCounters);
    const diagnostics = ['smmuv3_pmcg_bc00402', 'smmuv3_pmcg_4bc00402'].map(
      (device) =>
        `ERROR: Cannot determine SMMUv3 PMU type for device [${device}]. No identifier file found and no manual identifier specified`,
    );
    diagnostics.push(
      'ERROR: another PMU could not be discovered',
      'FATAL: diagnostic with successful catalog output',
      'Unprefixed collector diagnostic',
    );
    const stderr = '  ' + diagnostics.join('\r\n') + '\r\n\r\n';
    const logs = [];
    const commands = [];
    const discovered = await cpuCounters.discover(
      {
        log: (level, text) => logs.push({ level, text }),
        startProcess: async (args) => {
          commands.push(args[2]);
          return commandHandle(arm[args[2].split('.')[0]], stderr);
        },
      },
      { metadata: {} },
      '/collector',
    );
    assertEqual(
      commands.join(','),
      'counters.xml,events.xml',
      'Read both catalogues',
    );
    assertEqual(
      discovered[cpuCounters.ID].state,
      'available',
      'CPU cycles remain available',
    );
    assertEqual(
      discovered.neoprof_cpu_counter_catalogue.counters,
      arm.counters,
      'Preserve counters',
    );
    assertEqual(
      discovered.neoprof_cpu_counter_catalogue.events,
      arm.events,
      'Preserve events',
    );
    assertEqual(
      JSON.stringify(logs),
      JSON.stringify(
        [...diagnostics, ...diagnostics].map((text) => ({
          level: 'warn',
          text,
        })),
      ),
      'Retain every nonempty diagnostic in logs',
    );

    for (const [label, output, errors, rc] of [
      ['nonzero exit', arm, stderr, 1],
      ['invalid counters', { ...arm, counters: '<broken' }, stderr, 0],
      ['invalid events', { ...arm, events: '<broken' }, stderr, 0],
    ]) {
      let caught;
      try {
        await cpuCounters.discover(
          {
            log: () => {},
            startProcess: async (args) =>
              commandHandle(output[args[2].split('.')[0]], errors, rc),
          },
          { metadata: {} },
          '/collector',
        );
      } catch (error) {
        caught = error;
      }
      assertEqual(
        caught?.code,
        'common.UNKNOWN_ERROR',
        label + ' must still fail discovery',
      );
    }
  },
  discovery_failures_are_not_unavailable: async () => {
    const { arm, assertEqual } = createFixture(cpuCounters);
    for (const text of [
      '',
      { ...arm, counters: 'ERROR: permission denied\n' + arm.counters },
      { ...arm, events: arm.events + '  FATAL: counter discovery failed\n' },
      'not a catalogue',
    ]) {
      let failed = false;
      try {
        cpuCounters.resolveCatalogue(text);
      } catch (e) {
        failed = e.code === 'common.UNKNOWN_ERROR';
      }
      assertEqual(
        failed,
        true,
        `Invalid catalogue must raise a discovery error (catalogue: ${text})`,
      );
    }
    let failed = false;
    try {
      await cpuCounters.discover(
        {
          startProcess: async () =>
            commandHandle(arm.counters, 'ERROR: permission denied', 1),
        },
        { metadata: {} },
        '/collector',
      );
    } catch (e) {
      failed = e.code === 'common.UNKNOWN_ERROR';
    }
    assertEqual(failed, true, 'Nonzero collector exit must fail discovery');
  },
  empty_series_requires_registered_schema_checked_output: async () => {
    const { arm, engine, verificationEngine, request, saved, assertEqual } =
      createFixture(cpuCounters);
    for (const scenario of [
      'valid_empty',
      'unregistered',
      'unreadable',
      'unexpected_cpu',
    ]) {
      const ctx = request('optional', arm);
      await cpuCounters.prepare(engine, ctx);
      const verifier = {
        ...verificationEngine,
        execCommand: async (args) => {
          if (args[1].endsWith('counter_series_files.json'))
            return scenario === 'unregistered'
              ? { rc: 0, stdout: '{"counter_groups":[]}' }
              : verificationEngine.execCommand(args);
          if (args[0] === 'cat') return verificationEngine.execCommand(args);
          assertEqual(
            args.includes('--validate-counter-series'),
            true,
            'Schema validation must run even for empty output',
          );
          if (scenario === 'unreadable')
            return { rc: 1, stderr: 'missing or corrupt parquet' };
          return {
            rc: 0,
            stdout:
              scenario === 'unexpected_cpu'
                ? '{"valid":true,"device_nos":[99]}'
                : '{"valid":true,"device_nos":[]}',
          };
        },
      };
      await cpuCounters.verify(
        verifier,
        ctx,
        '/capture',
        [{ key_type: 8, series_id: 10 }],
        (m) => 'series.' + m.series_id,
        '/converter',
      );
      assertEqual(
        saved[saved.length - 1].state,
        scenario === 'valid_empty' ? 'collected' : 'incomplete',
        scenario + ' must retain the correct outcome',
      );
    }
  },
  option_conflicts_and_legacy_defaults: async () => {
    const { empty, assertEqual } = createFixture(cpuCounters);
    for (const params of [
      { mode: 'metrics' },
      { mode: 'spe' },
      { mode: 'samples', metrics_group: 'basic' },
      { mode: 'samples', get_ipc_metric_name: true },
    ]) {
      let failed = false;
      try {
        cpuCounters.validateOptions({ params, capabilityRequest: empty });
      } catch (e) {
        failed = true;
      }
      assertEqual(
        failed,
        true,
        `Explicit selection rejects conflicting options (${JSON.stringify(params)})`,
      );
      cpuCounters.validateOptions({ params });
    }
    assertEqual(
      (await cpuCounters.prepare({}, { params: { mode: 'samples' } })).length,
      0,
      'Omitted selection adds no collector arguments',
    );
    assertEqual(
      cpuCounters.initialProbeCapabilities({
        params: { mode: 'metrics' },
      })[cpuCounters.ID].state,
      'unavailable',
      'Legacy unsupported configurations retain a typed unavailable descriptor',
    );
    assertEqual(
      Object.keys(
        cpuCounters.initialProbeCapabilities({
          params: { mode: 'samples' },
          capabilityRequest: empty,
        }),
      ).length,
      0,
      'Explicit-empty probes skip CPU enumeration and descriptors',
    );
    assertEqual(
      cpuCounters.discoveryRequested({
        params: { mode: 'samples' },
        capabilityRequest: empty,
      }),
      false,
      'Explicit-empty requests do not discover counters',
    );
  },
  explicit_empty_writes_complete_configuration: async () => {
    const { empty, commands, saved, engine, assertEqual } =
      createFixture(cpuCounters);
    const ctx = {
      params: { mode: 'samples' },
      capabilityRequest: empty,
      metadata: {},
    };
    const args = await cpuCounters.prepare(engine, ctx);
    assertEqual(
      args.join(' '),
      '-c /tmp/cpu/counter_collection.xml',
      'Empty selection uses only the configuration file',
    );
    assertEqual(
      commands[0][4],
      cpuCounters.EMPTY_XML,
      'Configuration contains the complete empty XML document',
    );
    assertEqual(saved.length, 0, 'Empty selection saves no counter outcome');
  },
  required_unavailable_stops_before_configuration: async () => {
    const { commands, saved, engine, request, assertEqual } =
      createFixture(cpuCounters);
    const ctx = request('required', {
      counters: '<counters/>',
      events: '<events/>',
    });
    let failed = false;
    try {
      await cpuCounters.prepare(engine, ctx);
    } catch (e) {
      failed = e.code === 'engine.tool.REQUIRED_CAPABILITY_UNAVAILABLE';
    }
    assertEqual(failed, true, 'Required unavailable counter fails preparation');
    assertEqual(
      commands.length,
      0,
      'Unavailable required counter executes no configuration commands',
    );
    assertEqual(
      saved[0].state,
      'unavailable',
      'Unavailable outcome is saved before failure',
    );
  },
  optional_unavailable_uses_explicit_empty_and_warns: async () => {
    const { saved, warnings, engine, request, assertEqual } =
      createFixture(cpuCounters);
    const ctx = request('optional', {
      counters: '<counters/>',
      events: '<events/>',
    });
    assertEqual(
      (await cpuCounters.prepare(engine, ctx)).length,
      2,
      'Unavailable optional counter uses only empty configuration',
    );
    assertEqual(
      warnings.length,
      1,
      'Unavailable optional counter produces one warning',
    );
    assertEqual(
      saved[0].state,
      'unavailable',
      'Optional unavailable outcome is saved',
    );
  },
  configuration_write_or_readback_failure: async () => {
    const { arm, engine, request, assertEqual } = createFixture(cpuCounters);
    for (const bad of [
      { rc: 1, stderr: 'write failed' },
      { rc: 0, stdout: 'wrong XML' },
    ]) {
      const ctx = request('required', arm);
      let failed = false;
      try {
        await cpuCounters.prepare(
          { ...engine, execCommand: async () => bad },
          ctx,
        );
      } catch (e) {
        failed = e.code === 'common.UNKNOWN_ERROR';
      }
      assertEqual(
        failed,
        true,
        `Failed write or incorrect readback raises a configuration error (${JSON.stringify(bad)})`,
      );
    }
  },
  native_event_key_join_and_multiple_series: async () => {
    const { arm, x86, combine, assertEqual } = createFixture(cpuCounters);
    const native = cpuCounters.resolveCatalogue(combine(arm, x86)).native;
    const xml =
      '<captured version="1"><counters><counter key="0xa" type="ARMv9_Neoverse_V2_ccnt" event="0x11"/><counter key="0xb" type="Perf_Hardware_ccnt" event="0x0"/></counters></captured>';
    assertEqual(
      cpuCounters.bindSeries(
        native,
        xml,
        [
          { key_type: 8, series_id: 10 },
          { key_type: 8, series_id: 11 },
        ],
        (m) => 'series.' + m.series_id,
      ).length,
      2,
      'Both native counters bind to their analyzer series',
    );
    assertEqual(
      cpuCounters.bindSeries(
        native,
        xml.replace('event="0x11"', 'event="0x0"'),
        [{ key_type: 8, series_id: 10 }],
        (m) => 'series.' + m.series_id,
      ).length,
      0,
      'Mismatched event must not bind a series',
    );
    assertEqual(
      cpuCounters.bindSeries(
        native,
        xml,
        [{ key_type: 9, series_id: 10 }],
        (m) => 'series.' + m.series_id,
      ).length,
      0,
      'Non-counter key type must not bind a series',
    );
  },
  required_partial_output_fails_and_optional_warns: async () => {
    const {
      arm,
      x86,
      combine,
      saved,
      warnings,
      engine,
      verificationEngine,
      request,
      assertEqual,
    } = createFixture(cpuCounters);
    for (const policy of ['required', 'optional']) {
      const ctx = request(policy, combine(arm, x86));
      await cpuCounters.prepare(engine, ctx);
      let failed = false;
      try {
        await cpuCounters.verify(
          verificationEngine,
          ctx,
          '/capture',
          [{ key_type: 8, series_id: 10 }],
          (m) => 'series.' + m.series_id,
          '/converter',
        );
      } catch (e) {
        failed = true;
      }
      assertEqual(
        failed,
        policy === 'required',
        `Only a required request fails for partial output (policy: ${policy})`,
      );
      assertEqual(
        saved[saved.length - 1].state,
        'incomplete',
        'Partial output remains incomplete',
      );
      assertEqual(
        saved[saved.length - 1].payload.bindings.length,
        1,
        'Partial output retains only the verified binding',
      );
    }
    assertEqual(
      warnings.length,
      1,
      'Optional partial output produces one warning',
    );
  },
};
module.exports = scenarios;
