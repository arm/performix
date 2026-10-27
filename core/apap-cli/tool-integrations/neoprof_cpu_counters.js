// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Adapt the deployed collector's native CPU counters to counter.cpu_cycles.
// Lifecycle:
// - discover reads counters.xml and events.xml with sl-collect --print, then
//   resolves a logical availability descriptor and retains the native catalogue.
// - prepare resolves the requested native selections and supplies the collector
//   configuration. An omitted request preserves defaults; an empty request
//   explicitly disables counters.
// - verify joins captured.xml identities to timeline series, validates their
//   output, and emits only verified series and logical capability bindings.
// - save publishes the final logical outcome once: collected, unavailable, or
//   incomplete. It does not publish the discovery descriptor. Required failures
//   stop collection; optional unavailable or unverified output produces a warning.
// The integration owns lifecycle orchestration and timeline output publication;
// this module owns CPU identity resolution, selection, and verification.
// The engine fills missing outcomes on recording or reformatting failures.
const { parse: parseXML } = require('performix/xml');
const {
  CPU_CYCLES: ID,
  CPU_COUNTER_CAPABILITY_COMPONENT_TYPE: TYPE,
} = require('./tool_capabilities');
const EMPTY_XML =
  '<?xml version="1.0" encoding="UTF-8"?>\n<configurations revision="3">\n</configurations>\n';

function failure(code, cause) {
  return {
    code,
    metadata: { capability: ID },
    cause: String(cause),
  };
}
function value(state, reason, bindings = []) {
  return {
    componentType: TYPE,
    state,
    payload: {
      title: 'CPU Cycles',
      units: 'cycles',
      bindings,
      ...(reason ? { reason } : {}),
    },
  };
}
function selected(ctx) {
  return (
    !!ctx.capabilityRequest &&
    [
      ...ctx.capabilityRequest.required,
      ...ctx.capabilityRequest.optional,
    ].includes(ID)
  );
}
function discoveryRequested(ctx) {
  return ctx.capabilityRequest === undefined || selected(ctx);
}
function initialProbeCapabilities(ctx) {
  return discoveryRequested(ctx)
    ? { [ID]: value('unavailable', 'not_supported') }
    : {};
}
function validateOptions(ctx) {
  if (ctx.capabilityRequest === undefined) return;
  if (
    ctx.params.mode !== 'samples' ||
    ctx.params.metrics_group ||
    ctx.params.get_ipc_metric_name
  ) {
    throw failure(
      'tool_integrations.neoprof.CPU_COUNTER_OPTIONS_INVALID',
      'Explicit selection requires samples mode without metric groups or IPC collection',
    );
  }
}

function xmlRoot(source, expected) {
  const root = parseXML(source);
  if (root.name !== expected) throw new Error(`Expected ${expected} XML`);
  return root;
}
function children(node, name) {
  return node.children.filter((child) => child.name === name);
}
function integer(text) {
  if (typeof text !== 'string' || !/^(?:0x[0-9a-f]+|[0-9]+)$/i.test(text))
    throw new Error('Invalid XML integer');
  const number = Number(text);
  if (!Number.isSafeInteger(number))
    throw new Error('XML integer out of range');
  return number;
}

// counters.xml identifies the actual CPU groups and selectable names; events.xml
// also contains unrelated catalogue entries and must never determine availability.
function resolveCatalogue(catalogue) {
  try {
    const counters = xmlRoot(catalogue.counters, 'counters');
    const events = xmlRoot(catalogue.events, 'events');
    const clusters = children(counters, 'cluster').map(
      (node) => node.attributes,
    );
    const cpus = children(counters, 'cpu').map((node) => node.attributes);
    const ids = new Set(clusters.map((cluster) => integer(cluster.id)));
    if (ids.size !== clusters.length) throw new Error('Duplicate CPU group');
    const cpuIDs = new Set();
    for (const cpu of cpus) {
      const id = integer(cpu.id);
      if (cpuIDs.has(id) || !ids.has(integer(cpu.cluster)))
        throw new Error('Invalid CPU topology');
      cpuIDs.add(id);
    }
    const available = children(counters, 'counter').map(
      (node) => node.attributes.name,
    );
    if (available.some((name) => typeof name !== 'string'))
      throw new Error('Missing native counter identity');
    const definitions = [];
    function visit(node) {
      if (node.name === 'category') {
        for (const event of children(node, 'event'))
          definitions.push({
            category: node.attributes,
            event: event.attributes,
          });
      }
      for (const child of node.children) visit(child);
    }
    visit(events);
    const native = new Map();
    let reason;
    for (const cluster of clusters) {
      if (!/^[A-Za-z0-9_.-]+$/.test(cluster.name || ''))
        throw new Error('Invalid CPU group identity');
      const canonical = `${cluster.name}_ccnt`.toLowerCase();
      const name = available.find(
        (candidate) => candidate.toLowerCase() === canonical,
      );
      const devices = cpus
        .filter((cpu) => integer(cpu.cluster) === integer(cluster.id))
        .map((cpu) => integer(cpu.id));
      if (!devices.length) throw new Error('CPU group has no CPU membership');
      const matches = definitions.filter(
        ({ event }) => event.counter?.toLowerCase() === canonical,
      );
      if (!name || !matches.length) {
        reason ||= 'not_supported';
        continue;
      }
      const identities = new Set(
        matches.map(({ category, event }) =>
          JSON.stringify([
            category.per_cpu,
            category.counter_set?.toLowerCase(),
            event.units,
            integer(event.event),
          ]),
        ),
      );
      if (identities.size !== 1) {
        reason = 'ambiguous';
        continue;
      }
      const { category, event } = matches[0];
      if (
        category.per_cpu !== 'yes' ||
        category.counter_set?.toLowerCase() !==
          `${cluster.name}_cnt`.toLowerCase() ||
        event.units !== 'cycles'
      ) {
        reason ||= 'not_supported';
        continue;
      }
      const previous = native.get(canonical);
      native.set(canonical, {
        name,
        event: integer(event.event),
        devices: [...new Set([...(previous?.devices || []), ...devices])],
      });
    }
    if (reason || !native.size)
      return {
        capability: value('unavailable', reason || 'not_supported'),
        native: [],
      };
    return { capability: value('available'), native: [...native.values()] };
  } catch (error) {
    throw failure(
      'common.UNKNOWN_ERROR',
      `Invalid CPU counter catalogue: ${String(error)}`,
    );
  }
}

async function readCatalogue(engine, ctx, command) {
  const handle = await engine.startProcess(command, {
    asPrivileged: ctx.metadata.neoprofAsPrivileged,
    stdout: { redirect: 'stream' },
    stderr: { redirect: 'stream' },
  });
  const stdout = [];
  const stderr = [];
  try {
    // Drain both pipes while waiting: either pipe can fill before process exit.
    const [, , result] = await Promise.all([
      forAwait(handle.stdout, (chunk) => stdout.push(chunk)),
      forAwait(handle.stderr, (chunk) => stderr.push(chunk)),
      handle.wait(),
    ]);
    return {
      rc: result.exitCode,
      stdout: stdout.join(''),
      stderr: stderr.join(''),
    };
  } catch (error) {
    try {
      await handle.kill();
    } catch (cleanupError) {
      engine.log(
        'warn',
        `Failed to terminate counter discovery: ${String(cleanupError)}`,
      );
    }
    throw error;
  }
}

async function discover(engine, ctx, slRecordPath) {
  const catalogue = {};
  for (const name of ['counters', 'events']) {
    const result = await readCatalogue(engine, ctx, [
      slRecordPath,
      '--print',
      `${name}.xml`,
    ]);
    if (result.rc !== 0)
      throw failure(
        'common.UNKNOWN_ERROR',
        `CPU counter discovery failed: ${JSON.stringify(result)}`,
      );
    // Catalogues describe discovered counters, not a guarantee of collection.
    // Individual PMU diagnostics do not invalidate a successful XML response.
    for (const line of result.stderr
      .split(/\r?\n/)
      .map((line) => line.trim())) {
      if (line) engine.log('warn', line);
    }
    catalogue[name] = result.stdout.replace(/^(?:WARN:[^\r\n]*\r?\n)+/, '');
  }
  const resolved = resolveCatalogue(catalogue);
  return {
    [ID]: resolved.capability,
    neoprof_cpu_counter_catalogue: catalogue,
  };
}

async function save(engine, ctx, outcome) {
  if (!selected(ctx) || ctx.metadata.cpuCounterOutcomeSaved) return;
  // Claim the final outcome before writing so cleanup does not replace a failed
  // publication with a different outcome; the saved request remains unresolved.
  ctx.metadata.cpuCounterOutcomeSaved = true;
  await engine.addToolCapability(ID, TYPE, {
    state: outcome.state,
    payload: outcome.payload,
  });
}
async function prepare(engine, ctx) {
  validateOptions(ctx);
  if (ctx.capabilityRequest === undefined) return [];
  let native = [];
  if (selected(ctx)) {
    const resolved = resolveCatalogue(
      ctx.discovery?.capabilities.neoprof_cpu_counter_catalogue,
    );
    if (resolved.capability.state === 'unavailable') {
      await save(engine, ctx, resolved.capability);
      if (ctx.capabilityRequest.required.includes(ID))
        throw failure(
          'engine.tool.REQUIRED_CAPABILITY_UNAVAILABLE',
          resolved.capability.payload.reason,
        );
      engine.writeCatalogMessage(
        'tool_integrations.neoprof.CPU_COUNTER_OPTIONAL_UNAVAILABLE',
        { capability: ID },
      );
    } else native = resolved.native;
  }
  ctx.metadata.cpuCounterNative = native;
  const directory = await engine.createTempDir();
  const path = `${directory}/counter_collection.xml`;
  // Data and path are positional arguments, not interpolated shell source.
  const written = await engine.execCommand(
    ['sh', '-c', 'printf "%s" "$1" > "$2"', 'sh', EMPTY_XML, path],
    {},
  );
  if (written.rc !== 0)
    throw failure(
      'common.UNKNOWN_ERROR',
      `Could not write CPU counter configuration: ${written.stderr}`,
    );
  const checked = await engine.execCommand(['cat', path], {});
  if (checked.rc !== 0 || checked.stdout !== EMPTY_XML)
    throw failure(
      'common.UNKNOWN_ERROR',
      `Counter configuration did not round trip: ${JSON.stringify(checked)}`,
    );
  return [
    '-c',
    path,
    ...(native.length ? ['-C', native.map((n) => n.name).join(',')] : []),
  ];
}

function capturedCounters(xml) {
  const root = xmlRoot(xml, 'captured');
  const sections = children(root, 'counters');
  if (sections.length !== 1)
    throw new Error('Missing or duplicate captured counters');
  return children(sections[0], 'counter').map((node) => node.attributes);
}

// Native counter key -> key_type 8 / series_id is a pinned collector/analyzer
// contract, corroborated on Arm and x86. Never join by display title or name.
function bindSeries(native, xml, metadata, capabilityID) {
  let counters;
  try {
    counters = capturedCounters(xml);
  } catch {
    // Only XML parsing and captured-counter structure validation occur here.
    return [];
  }
  if (!Array.isArray(metadata)) return [];
  const bindings = [];
  for (const selection of native) {
    const matches = counters.filter(
      (c) => c.type?.toLowerCase() === selection.name.toLowerCase(),
    );
    if (
      matches.length !== 1 ||
      !/^0x[0-9a-f]+$/i.test(matches[0].event || '') ||
      Number(matches[0].event) !== selection.event ||
      !/^0x[0-9a-f]+$/i.test(matches[0].key || '')
    )
      continue;
    const key = Number(matches[0].key);
    if (counters.filter((c) => c.type && Number(c.key) === key).length !== 1)
      continue;
    const series = metadata.filter(
      (m) => m && m.key_type === 8 && m.series_id === key,
    );
    if (!Number.isSafeInteger(key) || series.length !== 1) continue;
    bindings.push({
      seriesCapabilityId: capabilityID(series[0]),
      series: series[0],
      devices: selection.devices,
    });
  }
  return bindings;
}

async function unverified(engine, ctx, bindings = []) {
  await save(engine, ctx, value('incomplete', 'output_unverified', bindings));
  if (ctx.capabilityRequest.required.includes(ID)) {
    throw failure(
      'tool_integrations.neoprof.CPU_COUNTER_OUTPUT_UNVERIFIED',
      'Native selection or output coverage could not be verified',
    );
  }
  engine.writeCatalogMessage(
    'tool_integrations.neoprof.CPU_COUNTER_OPTIONAL_INCOMPLETE',
    { capability: ID },
  );
}

function parseArtifactObject(stdout) {
  let value;
  try {
    value = JSON.parse(stdout);
  } catch (error) {
    if (!(error instanceof SyntaxError)) throw error;
    return undefined;
  }
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value
    : undefined;
}

function artifactReadOptions(engine, ctx) {
  return {
    asPrivileged:
      engine.getLocality() === 'host'
        ? false
        : ctx.metadata.neoprofAsPrivileged,
  };
}

async function loadSeriesRegistry(engine, ctx, captureDirectory) {
  const path = `${captureDirectory}/report-new/apx/metadata/counter_series_files.json`;
  let contents;
  if (engine.getLocality() === 'host') {
    try {
      contents = await engine.readHostFile(path);
    } catch (error) {
      if (error.value?.unavailable !== true) throw error;
      return undefined;
    }
  } else {
    const result = await engine.execCommand(
      ['cat', path],
      artifactReadOptions(engine, ctx),
    );
    if (result.rc !== 0) return undefined;
    contents = result.stdout;
  }

  const document = parseArtifactObject(contents);
  if (!Array.isArray(document?.counter_groups)) return undefined;
  const rows = [];
  for (const group of document.counter_groups) {
    if (
      !Number.isSafeInteger(group?.key_type) ||
      group.key_type < 0 ||
      !Array.isArray(group.counters)
    ) {
      return undefined;
    }
    for (const counter of group.counters) {
      if (
        !Number.isSafeInteger(counter?.id) ||
        counter.id < 0 ||
        !Array.isArray(counter.files)
      ) {
        return undefined;
      }
      for (const file of counter.files) {
        if (
          !Number.isSafeInteger(file?.bin_duration) ||
          file.bin_duration < 0 ||
          typeof file.relative_path !== 'string'
        ) {
          return undefined;
        }
        rows.push({
          key_type: group.key_type,
          series_id: counter.id,
          bin_duration: file.bin_duration,
          relative_path: file.relative_path,
        });
      }
    }
  }
  return rows;
}

async function verifySeries(
  engine,
  ctx,
  captureDirectory,
  converter,
  fileList,
  binding,
) {
  const registered = fileList.filter(
    (entry) =>
      entry &&
      entry.key_type === 8 &&
      entry.series_id === binding.series.series_id,
  );
  if (!registered.length) return false;
  for (const entry of registered) {
    const relativePath = `report-new/apx/timeline/key_type=8/series_id=${binding.series.series_id}/bin_duration=${entry.bin_duration}/counter.parquet`;
    if (entry.relative_path !== relativePath) return false;
    const result = await engine.execCommand(
      [
        converter,
        `${captureDirectory}/${relativePath}`,
        '--stdout',
        '--validate-counter-series',
      ],
      artifactReadOptions(engine, ctx),
    );
    if (result.rc !== 0) return false;
    const summary = parseArtifactObject(result.stdout);
    if (
      !summary ||
      summary.valid !== true ||
      !Array.isArray(summary.device_nos) ||
      !summary.device_nos.every(
        (device) =>
          Number.isInteger(device) && binding.devices.includes(device),
      )
    )
      return false;
  }
  return true;
}

async function verify(
  engine,
  ctx,
  captureDirectory,
  metadata,
  capabilityID,
  converter,
  emitSeries = () => {},
  attemptSeries = () => {},
) {
  if (!selected(ctx) || ctx.metadata.cpuCounterOutcomeSaved) return;
  const native = ctx.metadata.cpuCounterNative;
  let result;
  if (engine.getLocality() === 'host') {
    try {
      result = {
        rc: 0,
        stdout: await engine.readHostFile(`${captureDirectory}/captured.xml`),
      };
    } catch (error) {
      // Go marks missing and permission-denied reads explicitly. Internal API
      // failures still propagate, even for optional requests.
      if (error.value?.unavailable !== true) throw error;
      engine.log(
        'debug',
        `Could not read captured counter identities: ${String(error)}`,
      );
      return unverified(engine, ctx);
    }
  } else {
    result = await engine.execCommand(
      ['cat', `${captureDirectory}/captured.xml`],
      artifactReadOptions(engine, ctx),
    );
  }
  if (result.rc !== 0) return unverified(engine, ctx);
  const bindings = bindSeries(native, result.stdout, metadata, capabilityID);
  const fileList = await loadSeriesRegistry(engine, ctx, captureDirectory);
  if (!fileList) return unverified(engine, ctx);
  const verified = [];
  for (const binding of bindings) {
    attemptSeries(binding.series);
    const valid = await verifySeries(
      engine,
      ctx,
      captureDirectory,
      converter,
      fileList,
      binding,
    );
    if (!valid) continue;
    // Capability construction and publication are internal operations. Their
    // failures must propagate, even when counter collection is optional.
    await emitSeries(binding.series);
    verified.push({ seriesCapabilityId: binding.seriesCapabilityId });
  }
  const complete =
    native.length > 0 &&
    verified.length === native.length &&
    new Set(verified.map((b) => b.seriesCapabilityId)).size === verified.length;
  if (complete)
    await save(engine, ctx, value('collected', undefined, verified));
  else await unverified(engine, ctx, verified);
}

module.exports = {
  ID,
  TYPE,
  EMPTY_XML,
  selected,
  discoveryRequested,
  initialProbeCapabilities,
  validateOptions,
  resolveCatalogue,
  discover,
  prepare,
  loadSeriesRegistry,
  verify,
  unverified,
  bindSeries,
};
