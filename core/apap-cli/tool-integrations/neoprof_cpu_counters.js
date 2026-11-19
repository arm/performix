// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Bind the deployed collector's native CPU counters to logical CPU cycles and
// explicitly selected native events.
// Lifecycle:
// - discover reads counters.xml and events.xml with sl-record --print, then
//   resolves a logical availability descriptor and retains the native catalogue.
// - discoverNativeEvents lists programmable events for available CPU PMUs.
// - prepare resolves the requested native selections and supplies the collector
//   configuration. An omitted request preserves defaults; an empty request
//   disables implicit counters. Native events are selected in the request.
// - verify joins captured.xml identities to timeline series, validates their
//   output, and saves capability bindings for verified data.
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
  NATIVE_CPU_COUNTER_CAPABILITY_COMPONENT_TYPE,
  NATIVE_CPU_COUNTER_ID_PREFIX,
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
function cyclesSelected(ctx) {
  return (
    !!ctx.capabilityRequest &&
    [
      ...ctx.capabilityRequest.required,
      ...ctx.capabilityRequest.optional,
    ].includes(ID)
  );
}
function requestedNativeEvents(ctx) {
  if (!ctx.capabilityRequest) return [];
  return ctx.capabilityRequest.required.filter((id) =>
    id.startsWith(NATIVE_CPU_COUNTER_ID_PREFIX),
  );
}
function discoveryRequested(ctx) {
  return (
    ctx.capabilityRequest === undefined ||
    cyclesSelected(ctx) ||
    requestedNativeEvents(ctx).length > 0
  );
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

function eventDefinitions(events) {
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
  return definitions;
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
    const definitions = eventDefinitions(events);
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
      'tool_integrations.neoprof.CPU_COUNTER_CATALOGUE_INVALID',
      error,
    );
  }
}

function resolveNativeEvents(catalogue) {
  return resolveNativeCatalogue(catalogue).capabilities;
}

function resolveNativeCatalogue(catalogue) {
  try {
    const counters = xmlRoot(catalogue.counters, 'counters');
    const definitions = eventDefinitions(xmlRoot(catalogue.events, 'events'));
    const cpus = children(counters, 'cpu').map((node) => node.attributes);
    const available = children(counters, 'counter').map(
      (node) => node.attributes.name,
    );
    const capabilities = {};
    const pmus = new Map();
    for (const { attributes: cluster } of children(counters, 'cluster')) {
      const pmuID = cluster.name.toLowerCase();
      const devices = [];
      for (const cpu of cpus) {
        if (integer(cpu.cluster) === integer(cluster.id)) {
          devices.push(integer(cpu.id));
        }
      }
      if (!devices.length) throw new Error('CPU group has no CPU membership');
      const counterSet = `${cluster.name}_cnt`.toLowerCase();
      const slots = new Map();
      for (const name of available) {
        if (!name.toLowerCase().startsWith(counterSet)) continue;
        // Programmable counters end in a slot number, for example _cnt0.
        const slot = name.slice(counterSet.length);
        if (/^\d+$/.test(slot)) slots.set(Number(slot), name);
      }
      const previous = pmus.get(pmuID);
      pmus.set(pmuID, {
        name: cluster.name,
        devices: [...new Set([...(previous?.devices || []), ...devices])],
        counters: [...slots].sort(([a], [b]) => a - b).map(([, name]) => name),
      });
      // A programmable counter slot must be available on this PMU.
      if (!slots.size) continue;

      for (const { category, event } of definitions) {
        if (category.per_cpu !== 'yes') continue;
        if (category.counter_set?.toLowerCase() !== counterSet) continue;
        // Expose programmable events.
        if (event.counter) continue;
        const hexCode = integer(event.event).toString(16);
        const eventCode = `0x${hexCode}`;
        const id = `${NATIVE_CPU_COUNTER_ID_PREFIX}${pmuID}.${hexCode}`;
        if (capabilities[id]) continue;
        const payload = {
          title: `${event.title}: ${event.name}`,
          category: event.title,
          pmu: { id: pmuID, name: category.name },
          eventCode,
        };
        if (event.description) payload.description = event.description;
        if (event.units) payload.units = event.units;
        capabilities[id] = {
          componentType: NATIVE_CPU_COUNTER_CAPABILITY_COMPONENT_TYPE,
          state: 'available',
          payload,
        };
      }
    }
    return { capabilities, pmus };
  } catch (error) {
    throw failure(
      'tool_integrations.neoprof.CPU_COUNTER_CATALOGUE_INVALID',
      error,
    );
  }
}

function cpuEventAdvice(code, metadata) {
  return {
    level: 'error',
    messageCode: 'tool_integrations.neoprof.' + code,
    metadata,
  };
}

function validateCPUEvents(catalogue, ids, logicalEventIDs, resolved) {
  if (!ids.length) return [];
  const { capabilities: native, pmus } =
    resolved || resolveNativeCatalogue(catalogue);
  const logicalEvents = new Set(logicalEventIDs);

  const advice = [];
  const eventsByPMU = new Map();
  for (const id of ids) {
    const event = id.slice(NATIVE_CPU_COUNTER_ID_PREFIX.length);
    if (logicalEvents.has(id)) {
      advice.push(
        cpuEventAdvice('CPU_EVENT_ALREADY_REQUESTED', {
          event,
        }),
      );
    } else if (!native[id]) {
      advice.push(
        cpuEventAdvice('CPU_EVENT_UNAVAILABLE', {
          event,
          cmd: 'apx recipe info code_hotspots --target <target-name>',
        }),
      );
    } else {
      const pmuID = native[id].payload.pmu.id;
      const events = eventsByPMU.get(pmuID) || [];
      events.push(event);
      eventsByPMU.set(pmuID, events);
    }
  }
  for (const [pmuID, events] of eventsByPMU) {
    const pmu = pmus.get(pmuID);
    const count = events.length;
    if (count > pmu.counters.length) {
      advice.push(
        cpuEventAdvice('CPU_EVENTS_CAPACITY_EXCEEDED', {
          pmu: pmu.name,
          selected: String(count),
          capacity: String(pmu.counters.length),
          remove: String(count - pmu.counters.length),
          selection: events.join(', '),
        }),
      );
    }
  }
  return advice;
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

async function loadCatalogue(engine, ctx, slRecordPath) {
  const catalogue = {};
  for (const name of ['counters', 'events']) {
    let result;
    try {
      result = await readCatalogue(engine, ctx, [
        slRecordPath,
        // Without an explicit scope, --print defaults to system-wide profiling.
        ...(ctx.workload?.type === 'launch' ? ['-S', 'no'] : []),
        '--print',
        `${name}.xml`,
      ]);
    } catch (error) {
      if (error?.code) throw error;
      throw failure(
        'tool_integrations.neoprof.CPU_COUNTER_DISCOVERY_FAILED',
        error,
      );
    }
    const xml = result.stdout.replace(/^(?:WARN:[^\r\n]*\r?\n)+/, '');
    // NeoProf can exit successfully after reporting an initialization error.
    if (result.rc !== 0 || !xml.trim())
      throw failure(
        'tool_integrations.neoprof.CPU_COUNTER_DISCOVERY_FAILED',
        JSON.stringify(result),
      );
    // Catalogues describe discovered counters, not a guarantee of collection.
    // Individual PMU diagnostics do not invalidate a successful XML response.
    for (const line of result.stderr.split(/\r?\n/)) {
      const warning = line.trim();
      if (warning) engine.log('warn', warning);
    }
    catalogue[name] = xml;
  }
  return catalogue;
}

async function discover(engine, ctx, slRecordPath) {
  const catalogue = await loadCatalogue(engine, ctx, slRecordPath);
  const resolved = resolveCatalogue(catalogue);
  const capabilities = {
    [ID]: resolved.capability,
    neoprof_cpu_counter_catalogue: catalogue,
  };
  const logicalEventIDs = [];
  if (cyclesSelected(ctx)) {
    for (const { name, event } of resolved.native) {
      const pmuID = name.toLowerCase().replace(/_ccnt$/, '');
      const hexCode = event.toString(16);
      logicalEventIDs.push(
        `${NATIVE_CPU_COUNTER_ID_PREFIX}${pmuID}.${hexCode}`,
      );
    }
  }
  const nativeEvents = requestedNativeEvents(ctx);
  const native = nativeEvents.length
    ? resolveNativeCatalogue(catalogue)
    : undefined;
  if (native) Object.assign(capabilities, native.capabilities);
  return {
    catalogue,
    capabilities,
    advice: validateCPUEvents(catalogue, nativeEvents, logicalEventIDs, native),
  };
}

async function discoverNativeEvents(engine, ctx, slRecordPath) {
  const catalogue = await loadCatalogue(engine, ctx, slRecordPath);
  const capabilities = resolveNativeEvents(catalogue);
  if (discoveryRequested(ctx)) {
    capabilities[ID] = resolveCatalogue(catalogue).capability;
    capabilities.neoprof_cpu_counter_catalogue = catalogue;
  }
  return capabilities;
}

async function save(engine, ctx, outcome) {
  if (!cyclesSelected(ctx) || ctx.metadata.cpuCounterOutcomeSaved) return;
  // Claim the final outcome before writing so cleanup does not replace a failed
  // publication with a different outcome; the saved request remains unresolved.
  ctx.metadata.cpuCounterOutcomeSaved = true;
  await engine.addToolCapability(ID, TYPE, {
    state: outcome.state,
    payload: outcome.payload,
  });
}
async function prepare(
  engine,
  ctx,
  catalogue = ctx.discovery?.capabilities.neoprof_cpu_counter_catalogue,
) {
  validateOptions(ctx);
  if (ctx.capabilityRequest === undefined) return [];
  let native = [];
  if (cyclesSelected(ctx)) {
    const resolved = resolveCatalogue(catalogue);
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
  const counters = native.map((n) => n.name);
  ctx.metadata.cpuEventNative = [];
  const nativeEvents = requestedNativeEvents(ctx);
  if (nativeEvents.length) {
    // The integration validates this selection against the fresh catalogue
    // before preparation. Consume one available slot per event on each PMU.
    const { capabilities, pmus } = resolveNativeCatalogue(catalogue);
    for (const id of nativeEvents) {
      const { payload } = capabilities[id];
      const { pmu, eventCode } = payload;
      const nativePMU = pmus.get(pmu.id);
      const name = nativePMU.counters.shift();
      ctx.metadata.cpuEventNative.push({
        id,
        payload,
        name,
        event: Number(eventCode),
        devices: nativePMU.devices,
      });
      counters.push(`${name}:${eventCode}`);
    }
  }
  const counterArgs = counters.length ? ['-C', counters.join(',')] : [];
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
      `Counter configuration verification failed: ${JSON.stringify(checked)}`,
    );
  return ['-c', path, ...counterArgs];
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
      selection,
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
  const verifyCycles =
    cyclesSelected(ctx) && !ctx.metadata.cpuCounterOutcomeSaved;
  const native = verifyCycles ? ctx.metadata.cpuCounterNative : [];
  const additional = ctx.metadata.cpuEventNative || [];
  if (!verifyCycles && !additional.length) return;
  const captured = await readCapturedCounters(engine, ctx, captureDirectory);
  const bindings =
    captured === undefined
      ? []
      : bindSeries(
          [...native, ...additional],
          captured,
          metadata,
          capabilityID,
        );
  const fileList =
    captured === undefined
      ? []
      : await loadSeriesRegistry(engine, ctx, captureDirectory);
  const verified = new Map();
  for (const binding of fileList ? bindings : []) {
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
    verified.set(binding.selection.name, {
      seriesCapabilityId: binding.seriesCapabilityId,
    });
  }
  if (verifyCycles) {
    const cycleBindings = native.flatMap(
      (selection) => verified.get(selection.name) || [],
    );
    const complete =
      native.length > 0 &&
      cycleBindings.length === native.length &&
      new Set(cycleBindings.map((b) => b.seriesCapabilityId)).size ===
        cycleBindings.length;
    if (complete)
      await save(engine, ctx, value('collected', undefined, cycleBindings));
    else await unverified(engine, ctx, cycleBindings);
  }
  for (const selection of additional) {
    const binding = verified.get(selection.name);
    await engine.addToolCapability(
      selection.id,
      NATIVE_CPU_COUNTER_CAPABILITY_COMPONENT_TYPE,
      {
        state: binding ? 'collected' : 'incomplete',
        payload: {
          ...selection.payload,
          ...(binding ? { binding } : { reason: 'output_unverified' }),
        },
      },
    );
  }
}

async function readCapturedCounters(engine, ctx, captureDirectory) {
  if (engine.getLocality() === 'host') {
    try {
      return await engine.readHostFile(`${captureDirectory}/captured.xml`);
    } catch (error) {
      // Go marks missing and permission-denied reads explicitly. Internal API
      // failures still propagate, even for optional requests.
      if (error.value?.unavailable !== true) throw error;
      engine.log(
        'debug',
        `Could not read captured counter identities: ${String(error)}`,
      );
      return undefined;
    }
  }
  const result = await engine.execCommand(
    ['cat', `${captureDirectory}/captured.xml`],
    artifactReadOptions(engine, ctx),
  );
  return result.rc === 0 ? result.stdout : undefined;
}

module.exports = {
  ID,
  TYPE,
  EMPTY_XML,
  cyclesSelected,
  discoveryRequested,
  requestedNativeEvents,
  initialProbeCapabilities,
  validateOptions,
  resolveCatalogue,
  resolveNativeEvents,
  validateCPUEvents,
  discover,
  discoverNativeEvents,
  prepare,
  loadSeriesRegistry,
  verify,
  unverified,
  bindSeries,
};
