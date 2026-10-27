// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const originalRun = tool.run;
const scenarios = {
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
    let caught;
    try {
      validateCPUCounterOptions({ isNeoprofTimelineEnabled: () => false }, ctx);
    } catch (error) {
      caught = error;
    }
    assertEqual(
      caught?.code,
      'tool_integrations.neoprof.CPU_COUNTER_OPTIONS_INVALID',
      'Selected CPU counters are rejected when timeline export is disabled',
    );

    ctx.capabilityRequest = { required: [], optional: [] };
    validateCPUCounterOptions({ isNeoprofTimelineEnabled: () => false }, ctx);

    ctx.capabilityRequest = { required: [cpuCounters.ID], optional: [] };
    ctx.params.workflow = 'sys_util';
    validateCPUCounterOptions({ isNeoprofTimelineEnabled: () => false }, ctx);
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
