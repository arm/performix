// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const originalRun = tool.run;
async function captureCPUEvents(selection) {
  const { arm, engine, request } = createFixture(cpuCounters);
  arm.counters = `<counters>
    <cluster id="0" name="ARMv8_Cortex_A78"/><cpu id="0" cluster="0"/>
    <counter name="ARMv8_Cortex_A78_cnt10"/><counter name="ARMv8_Cortex_A78_cnt2"/>
    <cluster id="1" name="ARMv8_Cortex_A55"/><cpu id="1" cluster="1"/>
    <counter name="ARMv8_Cortex_A55_cnt3"/>
  </counters>`;
  arm.events = `<events>
    <category name="Cortex-A78" counter_set="ARMv8_Cortex_A78_cnt" per_cpu="yes">
      <event event="0x08" title="Instructions" name="Retired"/>
      <event event="0x09" title="Branches" name="Retired"/>
      <event event="0x10" title="Branches" name="Mispredicted"/>
    </category>
    <category name="Cortex-A55" counter_set="ARMv8_Cortex_A55_cnt" per_cpu="yes">
      <event event="0x08" title="Instructions" name="Retired"/>
    </category>
  </events>`;
  const ctx = request('empty', arm);
  delete ctx.discovery;
  ctx.capabilityRequest.required = selection.split(',');
  ctx.params.sampling_frequency = 'normal';
  ctx.params.reformat_on_host = true;
  ctx.timeout = 3;
  ctx.env = {};
  ctx.workload = { type: 'systemWide' };
  isPrivilegeRequired = async () => false;
  prepareWritableTargetFile = async () => {};
  immediateEmitSlRecordFiles = () => {};
  stopJitdumpAgents = async () => {};
  let launched;
  runSlRecord = async (engine, ctx, args) => {
    launched = args;
  };
  const discovered = [];

  const error = await originalRun(
    {
      ...engine,
      toolsRoot: () => '/tools',
      getPlatform: () => ({ OS: 'Linux' }),
      log: () => {},
      emitOutput: () => {},
      startProcess: async (args) => {
        discovered.push(args[2]);
        let done = false;
        return {
          stdout: {
            next: async () => {
              if (done) return { done: true };
              done = true;
              return { done: false, value: arm[args[2].split('.')[0]] };
            },
          },
          stderr: { next: async () => ({ done: true }) },
          wait: async () => ({ exitCode: 0 }),
        };
      },
    },
    ctx,
  ).then(
    () => undefined,
    (error) => error,
  );
  return { error, launched, discovered };
}

const scenarios = {
  cpu_event_outcomes: async (_, failure) => {
    // Given two selected native events on a PMU shared by two CPU clusters.
    const { arm, engine, request, verificationEngine, warnings, assertEqual } =
      createFixture(cpuCounters);
    arm.counters = arm.counters.replace(
      '</counters>',
      '<counter name="ARMv9_Neoverse_V2_cnt3"/><counter name="ARMv9_Neoverse_V2_cnt4"/>' +
        '<cluster id="1" name="ARMv9_Neoverse_V2"/><cpu id="1" cluster="1"/></counters>',
    );
    arm.events = arm.events.replace(
      '</category>',
      '<event event="0x08" title="Instructions" name="Retired"/><event event="0x10" title="Branches" name="Mispredicted"/></category>',
    );
    // The success case also collects CPU cycles; failures exercise events alone.
    const ctx = request(failure ? 'empty' : 'optional', arm);
    ctx.capabilityRequest.required = [
      'nativecounter.armv9_neoverse_v2.8',
      'nativecounter.armv9_neoverse_v2.10',
    ];
    const args = await cpuCounters.prepare(engine, ctx);
    assertEqual(
      args.join(' '),
      '-c /tmp/cpu/counter_collection.xml -C ' +
        (failure ? '' : 'ARMv9_Neoverse_V2_ccnt,') +
        'ARMv9_Neoverse_V2_cnt3:0x8,ARMv9_Neoverse_V2_cnt4:0x10',
      'CPU cycles and additional events share one explicit counter selection',
    );
    const metadata = [10, 11, 12].map((series_id) => ({
      key_type: 8,
      series_id,
      title: 'Counter',
      name: 'Same display name',
    }));
    const captured = `<captured><counters><counter key="0xa" type="ARMv9_Neoverse_V2_cnt3" event="${failure === 'identity' ? '0x09' : '0x08'}"/><counter key="0xb" type="ARMv9_Neoverse_V2_cnt4" event="0x10"/><counter key="0xc" type="ARMv9_Neoverse_V2_ccnt" event="0x11"/></counters></captured>`;
    readToolCapabilityMetadata = async () => metadata;
    const registryJSON = JSON.stringify({
      counter_groups: [
        {
          key_type: 8,
          counters: metadata.map((series) => ({
            id: series.series_id,
            files: NEOPROF_TIMELINE_BIN_DURATIONS_NS.map((bin_duration) => ({
              bin_duration,
              relative_path: `report-new/apx/timeline/key_type=8/series_id=${series.series_id}/bin_duration=${bin_duration}/counter.parquet`,
            })),
          })),
        },
      ],
    });
    const published = [];
    const outcomes = [];
    publishToolCapability = async (_, __, series) =>
      published.push(series.series_id);
    const verifier = {
      ...verificationEngine,
      getLocality: () => 'host',
      getPlatform: () => ({ OS: 'Linux' }),
      toolsRoot: () => '/tools',
      readHostFile: async (path) =>
        path.endsWith('counter_series_files.json') ? registryJSON : captured,
      execCommand: async (args) => ({
        rc: 0,
        stdout: JSON.stringify({
          valid: true,
          device_nos:
            failure === 'devices' && args[1].includes('series_id=10/')
              ? [2]
              : [0, 1],
        }),
      }),
      addToolCapability: async (id, type, data) =>
        outcomes.push({ id, type, ...data }),
    };

    // When finalizing output using the identities retained during preparation.
    await finalizeCounterCapabilities(verifier, ctx, '/capture', false);

    // Then each event gets its own verified binding, without relying on display names.
    assertEqual(warnings.length, 0, 'No counter warnings');
    if (!failure) {
      const cycleOutcome = outcomes.shift();
      assertEqual(cycleOutcome.id, cpuCounters.ID, 'CPU-cycle identity');
      assertEqual(cycleOutcome.state, 'collected', 'CPU-cycle outcome');
      assertEqual(
        cycleOutcome.payload.bindings
          .map((binding) => binding.seriesCapabilityId)
          .join(','),
        metadataEntryToCapabilityID(metadata[2]),
        'CPU cycles only bind their own series',
      );
    }
    assertEqual(outcomes.length, 2, 'One outcome per selected event');
    for (const [index, outcome] of outcomes.entries()) {
      const unverified = index === 0 && failure;
      assertEqual(
        outcome.id,
        ctx.capabilityRequest.required[index],
        'Event identity',
      );
      assertEqual(
        outcome.state,
        unverified ? 'incomplete' : 'collected',
        'Event outcome',
      );
      assertEqual(
        outcome.payload.binding?.seriesCapabilityId,
        unverified ? undefined : metadataEntryToCapabilityID(metadata[index]),
        'Only verified events get bindings',
      );
      assertEqual(
        outcome.payload.reason,
        unverified ? 'output_unverified' : undefined,
        'Outcome reason',
      );
    }
    assertEqual(
      published.sort().join(','),
      failure === 'devices' ? '11,12' : '10,11,12',
      'Valid series are published once; invalid data is excluded',
    );
  },
  host_capture_read_failure: async (hostEngine, policy) => {
    const {
      arm,
      engine,
      verificationEngine,
      request,
      saved,
      warnings,
      assertEqual,
    } = createFixture(cpuCounters);
    const ctx = request(policy, arm);
    await cpuCounters.prepare(engine, ctx);
    const metadata = {
      key_type: 8,
      series_id: 10,
      title: 'CPU',
      name: 'Cycles',
    };
    readToolCapabilityMetadata = async () => [metadata];
    cpuCounters.loadSeriesRegistry = async () =>
      NEOPROF_TIMELINE_BIN_DURATIONS_NS.map((bin_duration) => ({
        ...metadata,
        bin_duration,
        relative_path: `report-new/apx/timeline/key_type=8/series_id=10/bin_duration=${bin_duration}/counter.parquet`,
      }));
    publishToolCapability = async () => {};
    let caught;
    try {
      await finalizeCounterCapabilities(
        {
          ...verificationEngine,
          getLocality: () => 'host',
          getPlatform: () => ({ OS: 'Linux' }),
          toolsRoot: () => '/tools',
          readHostFile: hostEngine.readHostFile,
        },
        ctx,
        '/capture',
        false,
      );
    } catch (error) {
      if (
        error.code !== 'tool_integrations.neoprof.CPU_COUNTER_OUTPUT_UNVERIFIED'
      )
        throw error;
      caught = error;
    }
    assertEqual(
      saved.length,
      1,
      'Host file availability failure saves one outcome',
    );
    assertEqual(
      saved[0].payload.reason,
      'output_unverified',
      'Host file availability failure is unverified',
    );
    assertEqual(
      saved[0].state,
      'incomplete',
      'Missing capture cannot be collected',
    );
    assertEqual(
      saved[0].payload.bindings.length,
      0,
      'Missing capture has no verified bindings',
    );
    assertEqual(
      warnings.length,
      policy === 'optional' ? 1 : 0,
      'Only optional requests warn',
    );
    assertEqual(
      !!caught,
      policy === 'required',
      'Only required requests fail verification',
    );
  },
  failed_optional_cpu_series_is_not_published_by_fallback: async () => {
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
    const metadata = [
      { key_type: 8, series_id: 10, title: 'CPU', name: 'Cycles' },
      { key_type: 1, series_id: 20, title: 'Other', name: 'Counter' },
    ];
    readToolCapabilityMetadata = async () => metadata;
    cpuCounters.loadSeriesRegistry = async () =>
      metadata.flatMap((series) =>
        NEOPROF_TIMELINE_BIN_DURATIONS_NS.map((bin_duration) => ({
          ...series,
          bin_duration,
          relative_path: `report-new/apx/timeline/key_type=${series.key_type}/series_id=${series.series_id}/bin_duration=${bin_duration}/counter.parquet`,
        })),
      );
    const published = [];
    publishToolCapability = async (engine, ctx, series) =>
      published.push(series);
    await finalizeCounterCapabilities(
      {
        ...verificationEngine,
        getPlatform: () => ({ OS: 'Linux' }),
        toolsRoot: () => '/tools',
        execCommand: async (args) =>
          args.includes('--validate-counter-series')
            ? {
                rc: 0,
                stdout: JSON.stringify({ valid: false, device_nos: [] }),
              }
            : verificationEngine.execCommand(args),
      },
      ctx,
      '/capture',
      false,
    );
    assertEqual(
      saved[0].state,
      'incomplete',
      'Invalid optional CPU output is incomplete',
    );
    assertEqual(
      saved[0].payload.bindings.length,
      0,
      'Invalid series has no logical binding',
    );
    assertEqual(warnings.length, 1, 'Optional verification failure warns');
    assertEqual(published.length, 1, 'Only unrelated output is published');
    assertEqual(
      published[0],
      metadata[1],
      'Fallback preserves unrelated valid output',
    );
  },
  selected_counters_require_timeline_export: async () => {
    const { arm, request, assertEqual } = createFixture(cpuCounters);
    const ctx = request('required', arm);
    ctx.params.export_timeline = false;
    let caught;
    try {
      validateCPUCounterOptions(
        {
          isNeoprofTimelineEnabled: () => false,
          isCPUSchedulingEnabled: () => false,
        },
        ctx,
      );
    } catch (error) {
      caught = error;
    }
    assertEqual(
      caught?.code,
      'tool_integrations.neoprof.CPU_COUNTER_OPTIONS_INVALID',
      'Selected CPU counters are rejected when timeline export is disabled',
    );

    ctx.capabilityRequest = { required: [], optional: [] };
    validateCPUCounterOptions(
      {
        isNeoprofTimelineEnabled: () => false,
        isCPUSchedulingEnabled: () => false,
      },
      ctx,
    );

    ctx.capabilityRequest = { required: [cpuCounters.ID], optional: [] };
    ctx.params.export_timeline = true;
    validateCPUCounterOptions({ isNeoprofTimelineEnabled: () => false }, ctx);
    ctx.params.export_timeline = false;
    ctx.params.workflow = 'sys_util';
    validateCPUCounterOptions(
      {
        isNeoprofTimelineEnabled: () => false,
        isCPUSchedulingEnabled: () => false,
      },
      ctx,
    );
  },
  collector_arguments_apply_selection_before_workload: async () => {
    const { arm, engine, request, assertEqual } = createFixture(cpuCounters);
    isPrivilegeRequired = async () => true;
    prepareWritableTargetFile = async () => {};
    emitAnalysisFiles = () => {};
    isRichDataCaptureEnabled = () => false;
    immediateEmitSlRecordFiles = () => {};
    stopJitdumpAgents = async () => {};
    let launched;
    runSlRecord = async (e, c, args) => {
      launched = args;
    };
    const ctx = request('required', arm);
    Object.assign(ctx, {
      timeout: 3,
      workload: { type: 'systemWide' },
      env: {},
    });
    ctx.params.sampling_frequency = 'normal';
    await originalRun(
      {
        ...engine,
        toolsRoot: () => '/tools',
        getPlatform: () => ({ OS: 'Linux' }),
        log: () => {},
        emitOutput: () => {},
      },
      ctx,
    );
    assertEqual(
      launched.includes('-c'),
      true,
      'Collector receives the explicit configuration argument',
    );
    assertEqual(
      launched.includes('ARMv9_Neoverse_V2_ccnt'),
      true,
      'Collector receives the selected Arm native counter',
    );
    assertEqual(
      ctx.metadata.cpuCounterAttempted,
      true,
      'Collection attempt is marked before collector execution',
    );
  },
  invalid_cpu_event_stop_capture: async () => {
    // Given more selected events than programmable counters.
    const { assertEqual } = createFixture(cpuCounters);

    // When the integration validates the selection before capture.
    const { error, launched } = await captureCPUEvents(
      'nativecounter.armv8_cortex_a78.8,nativecounter.armv8_cortex_a78.9,nativecounter.armv8_cortex_a78.10',
    );

    // Then the capacity error blocks capture.
    assertEqual(
      error?.code,
      'tool_integrations.neoprof.CPU_EVENTS_CAPACITY_EXCEEDED',
      'Over-capacity selection',
    );
    assertEqual(
      launched,
      undefined,
      'Invalid events never reach the collector',
    );
  },
  cpu_event_reach_collector: async () => {
    // Given interleaved PMU selections, unordered slots, and no cached catalogue or CPU-cycle request.
    const { assertEqual } = createFixture(cpuCounters);

    // When the integration prepares and starts capture.
    const { error, launched, discovered } = await captureCPUEvents(
      'nativecounter.armv8_cortex_a78.8,nativecounter.armv8_cortex_a55.8,nativecounter.armv8_cortex_a78.10',
    );

    // Then the fresh catalogue supplies the actual counter slot and event code.
    if (error) throw error;
    assertEqual(
      discovered.join(','),
      'counters.xml,events.xml',
      'Fresh discovery',
    );
    assertEqual(
      launched.filter((arg) => arg === '-C').length,
      1,
      'One counter list',
    );
    assertEqual(
      launched[launched.indexOf('-C') + 1],
      'ARMv8_Cortex_A78_cnt2:0x8,ARMv8_Cortex_A55_cnt3:0x8,ARMv8_Cortex_A78_cnt10:0x10',
      'Each event reaches the collector in a distinct available slot on its own PMU',
    );
  },
  android_launch_preserves_explicit_counter_selection: async () => {
    for (const selected of [false, true]) {
      const { arm, empty, engine, request, assertEqual } =
        createFixture(cpuCounters);
      isPrivilegeRequired = async () => false;
      prepareWritableTargetFile = async () => {};
      immediateEmitSlRecordFiles = () => {};
      stopJitdumpAgents = async () => {};
      let launched;
      runSlRecord = async (engine, ctx, args) => {
        launched = args;
      };
      const ctx = request('required', arm);
      if (!selected) ctx.capabilityRequest = empty;
      ctx.timeout = 3;
      ctx.env = {};
      ctx.workload = {
        type: 'androidLaunch',
        packageName: 'com.example.app',
        activityName: '.MainActivity',
      };
      ctx.params.sampling_frequency = 'normal';
      ctx.params.reformat_on_host = true;
      let preserved;
      await originalRun(
        {
          ...engine,
          toolsRoot: () => '/tools',
          getPlatform: () => ({ OS: 'Android' }),
          preserveTempDir: async (path) => {
            preserved = path;
          },
          log: () => {},
          emitOutput: () => {},
        },
        ctx,
      );
      assertEqual(
        launched.includes('-c'),
        true,
        'Android receives the explicit configuration',
      );
      assertEqual(
        launched.includes('-C'),
        selected,
        'Android adds native counters only when selected',
      );
      assertEqual(
        launched.indexOf('-c') < launched.indexOf('--android-pkg'),
        true,
        'Counter options precede the Android workload',
      );
      assertEqual(
        launched[launched.indexOf('--android-pkg') + 1],
        'com.example.app',
        'Android package is preserved',
      );
      assertEqual(
        preserved,
        ctx.metadata.outputDirectory,
        'Android capture directory is preserved for host analysis',
      );
    }
  },
  timeline_metadata_failures_preserve_original_error_with_cpu_selection:
    async () => {
      for (const code of [
        'tool_integrations.neoprof.METADATA_READ_FAILED',
        'tool_integrations.neoprof.METADATA_INVALID',
      ]) {
        for (const outcomeSaved of [false, true]) {
          const { arm, saved, warnings, engine, request, assertEqual } =
            createFixture(cpuCounters);
          const ctx = request('optional', arm);
          await cpuCounters.prepare(engine, ctx);
          ctx.metadata.cpuCounterOutcomeSaved = outcomeSaved;
          const failure = Object.assign(
            new Error('Metadata conversion failed'),
            {
              code,
            },
          );
          readToolCapabilityMetadata = async () => {
            throw failure;
          };
          let caught;
          try {
            await finalizeCounterCapabilities(
              {
                ...engine,
                log: () => {},
                isNeoprofTimelineEnabled: () => true,
              },
              ctx,
              '/capture',
              false,
            );
          } catch (err) {
            caught = err;
          }
          const scenario = `${code}, CPU outcome already saved: ${outcomeSaved}`;
          assertEqual(
            caught,
            failure,
            `Timeline failure preserves the original error (${scenario})`,
          );
          assertEqual(
            saved.length,
            0,
            `Timeline failure is not converted to an unverified CPU outcome (${scenario})`,
          );
          assertEqual(
            warnings.length,
            0,
            `Timeline failure is not replaced by an optional CPU warning (${scenario})`,
          );
        }
      }
    },
  missing_timeline_output_is_not_published: async () => {
    const { arm, saved, engine, request, assertEqual } =
      createFixture(cpuCounters);
    const ctx = request('optional', arm);
    await cpuCounters.prepare(engine, ctx);
    readToolCapabilityMetadata = async () => [
      {
        key_type: 8,
        series_id: 10,
        title: 'CPU',
        name: 'Cycles',
        description: '',
        units: 'cycles',
      },
    ];
    cpuCounters.loadSeriesRegistry = async () => [
      { key_type: 8, series_id: 10, bin_duration: 10000000000 },
    ];
    let caught;
    try {
      await finalizeCounterCapabilities(
        {
          ...engine,
          getPlatform: () => ({ OS: 'Linux' }),
          toolsRoot: () => '/tools',
        },
        ctx,
        '/capture',
        false,
      );
    } catch (err) {
      caught = err;
    }
    assertEqual(
      String(caught).includes('required bin durations'),
      true,
      'Missing timeline output remains a failure',
    );
    assertEqual(
      saved.length,
      0,
      'No capability is published before validation',
    );
  },
};
