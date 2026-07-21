// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * An entry in a jitdump user actions file.
 * @typedef {Object} JitdumpActionEntry
 * @property {string} timestamp
 * @property {string} code
 * @property {Object.<string, any>} payload
 */

/**
 * A validated jitdump user action.
 * @typedef {Object} JitdumpAction
 * @property {string} code
 * @property {Object.<string, any>} params
 */

// jitdump-dotnet merge mode reads raw .NET inputs from this APX capture subdirectory.
const dotnetInputRelativeDir = 'external/jitdump-dotnet';

/**
 * Reformats files generated during jitdump captures.
 * Any jitdump files are moved into the APC directory as expected by sl-analyze.
 * Any user actions reported during the capture are handled appropriately.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<void>}
 */
async function reformatJitdumps(engine, ctx) {
  const jitdumpTargetDir = `${ctx.metadata.captureDirectory}/jitdumps`;

  await ensureJitdumpTargetDir(engine, ctx, jitdumpTargetDir);

  const stageTasks = [];

  // JVM support: handle jitdumps + actions
  if (ctx.metadata.jitdumpJvmAvailable) {
    stageTasks.push(
      () => handleJvmJitdumps(engine, ctx, jitdumpTargetDir),
      () => handleJvmActions(engine, ctx.metadata.jvmActionsFile),
    );
  }

  // .NET support: stage jitdumps from all reported output directories.
  if (ctx.metadata.dotnetAgentAvailable) {
    const dotnetInputDir = `${ctx.metadata.captureDirectory}/${dotnetInputRelativeDir}`;
    const originalJitdumpTargetDir = `${dotnetInputDir}/jitdumps-original`;
    const eventPipeTargetDir = `${dotnetInputDir}/eventpipe`;

    stageTasks.push(async () => {
      const dotnetDirs = await getDotnetJitdumpDirsFromActions(
        engine,
        ctx.metadata.dotnetActionsFile,
        ctx.metadata.dotnetJitdumpDir,
      );

      // 1. Preserve raw .NET inputs in the layout consumed by jitdump-dotnet merge mode.
      await ensureJitdumpTargetDir(engine, ctx, originalJitdumpTargetDir);
      await ensureJitdumpTargetDir(engine, ctx, eventPipeTargetDir);
      await handleDotnetJitdumps(
        engine,
        ctx,
        originalJitdumpTargetDir,
        eventPipeTargetDir,
        dotnetDirs,
      );

      // 2. Prefer source-enriched jitdumps in the directory consumed by sl-analyze.
      // If merge fails, stage the original jitdumps instead.
      await mergeDotnetJitdumps(
        engine,
        ctx,
        originalJitdumpTargetDir,
        jitdumpTargetDir,
      );

      await handleDotnetActions(engine, ctx.metadata.dotnetActionsFile);
    });
  }

  await Promise.all(stageTasks.map((t) => t()));
}

/**
 * Ensures the APC jitdumps target directory exists.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {string} jitdumpTargetDir
 * @returns {Promise<void>}
 */
async function ensureJitdumpTargetDir(engine, ctx, jitdumpTargetDir) {
  // Note: we use a shell since the target filesystem is remote.
  const mkdirResult = await engine.execCommand(
    ['bash', '-c', `mkdir -p "${jitdumpTargetDir}"`],
    { asPrivileged: ctx.metadata.neoprofAsPrivileged },
  );
  if (mkdirResult.rc !== 0) {
    throw {
      code: 'tool_integrations.neoprof.JITDUMP_MOVE_FAILED',
      metadata: {
        rc: mkdirResult.rc,
        sourceDir: '(mkdir)',
        destinationDir: jitdumpTargetDir,
      },
      cause: mkdirResult.stderr
        ? mkdirResult.stderr.trim()
        : `failed to create jitdump target dir ${jitdumpTargetDir}`,
    };
  }
}

/**
 * Stages JVM jitdumps into the APC jitdumps directory.
 * Also processes any JVM user actions and removes agent_properties* (sl-analyze expects only jitdump files).
 *
 * Provides an engine warning if no jitdump files were produced.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {string} jitdumpTargetDir
 * @returns {Promise<void>}
 */
async function handleJvmJitdumps(engine, ctx, jitdumpTargetDir) {
  // If no files were produced, skip moving jitdumps and continue (warning-only).
  const jvmCheck = await engine.execCommand(
    ['bash', '-c', `compgen -G "${ctx.metadata.jvmJitdumpDir}/*" >/dev/null`],
    { asPrivileged: ctx.metadata.neoprofAsPrivileged },
  );
  if (jvmCheck.rc !== 0) {
    engine.log(
      'warn',
      `No JVM jitdump files were produced; skipping jitdump staging from '${ctx.metadata.jvmJitdumpDir}'.`,
    );
    return;
  }

  const jvmMvResult = await engine.execCommand(
    [
      'bash',
      '-c',
      `mv "${ctx.metadata.jvmJitdumpDir}/"* "${jitdumpTargetDir}/"`,
    ],
    { asPrivileged: ctx.metadata.neoprofAsPrivileged },
  );
  if (jvmMvResult.rc !== 0) {
    throw {
      code: 'tool_integrations.neoprof.JITDUMP_MOVE_FAILED',
      metadata: {
        rc: jvmMvResult.rc,
        sourceDir: ctx.metadata.jvmJitdumpDir,
        destinationDir: jitdumpTargetDir,
      },
      cause: jvmMvResult.stderr
        ? jvmMvResult.stderr.trim()
        : `failed to move JVM jitdump files from ${ctx.metadata.jvmJitdumpDir} to ${jitdumpTargetDir}`,
    };
  }

  const rmResult = await engine.execCommand(
    ['bash', '-c', `rm -f ${jitdumpTargetDir}/agent_properties*`],
    { asPrivileged: ctx.metadata.neoprofAsPrivileged },
  );
  if (rmResult.rc !== 0) {
    throw {
      code: 'tool_integrations.neoprof.JITDUMP_MOVE_FAILED',
      metadata: {
        rc: rmResult.rc,
        sourceDir: ctx.metadata.jvmJitdumpDir,
        destinationDir: jitdumpTargetDir,
      },
      cause: rmResult.stderr
        ? rmResult.stderr.trim()
        : `failed to remove agent_properties file from ${jitdumpTargetDir}`,
    };
  }
}

/**
 * Stages original .NET jitdumps and EventPipe traces into separate APC input directories.
 * capture.apc/jitdumps is reserved for the final sl-analyze input after merge handling.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {string} originalJitdumpTargetDir
 * @param {string} eventPipeTargetDir
 * @param {{directories: string[], pids: number[]}} jitdumpDirs
 * @returns {Promise<void>}
 */
async function handleDotnetJitdumps(
  engine,
  ctx,
  originalJitdumpTargetDir,
  eventPipeTargetDir,
  jitdumpDirs,
) {
  const dirs = Array.from(
    new Set(
      jitdumpDirs.directories.length > 0
        ? jitdumpDirs.directories
        : [ctx.metadata.dotnetJitdumpDir],
    ),
  );
  const pids = new Set(jitdumpDirs.pids);

  if (ctx.workload.type === 'attach') {
    pids.add(ctx.workload.pid);
  }

  const moveExistingFiles = async (filePaths, targetDir) => {
    const script = `
target_dir="$1"
shift
moved=0
for file_path in "$@"; do
  if [ -f "$file_path" ]; then
    mv "$file_path" "$target_dir/" || exit 1
    chmod u+rw,go+r "$target_dir/\${file_path##*/}" || exit 1
    moved=1
  fi
done
[ "$moved" -eq 1 ] || exit 3
`;
    const result = await engine.execCommand(
      ['bash', '-c', script, 'bash', targetDir, ...filePaths],
      { asPrivileged: ctx.metadata.neoprofAsPrivileged },
    );

    if (result.rc === 0) {
      return true;
    }

    if (result.rc !== 3) {
      engine.log(
        'warn',
        result.stderr.trim() || `Failed to move .NET files to ${targetDir}`,
      );
    }

    return false;
  };

  const moveExistingGlob = async (sourceDir, pattern, targetDir) => {
    const script = `
source_dir="$1"
pattern="$2"
target_dir="$3"
moved=0
for file_path in "$source_dir"/$pattern; do
  if [ -f "$file_path" ]; then
    mv "$file_path" "$target_dir/" || exit 1
    chmod u+rw,go+r "$target_dir/\${file_path##*/}" || exit 1
    moved=1
  fi
done
[ "$moved" -eq 1 ] || exit 3
`;
    const result = await engine.execCommand(
      ['bash', '-c', script, 'bash', sourceDir, pattern, targetDir],
      { asPrivileged: ctx.metadata.neoprofAsPrivileged },
    );

    if (result.rc === 0) {
      return true;
    }

    if (result.rc !== 3) {
      engine.log(
        'warn',
        result.stderr.trim() ||
          `Failed to move .NET files from ${sourceDir} to ${targetDir}`,
      );
    }

    return false;
  };

  let movedAnyJitdump = false;
  let movedAnyEventPipe = false;

  for (const dir of dirs) {
    if (dir === ctx.metadata.dotnetJitdumpDir) {
      // This directory is created by APX for this run, so every jitdump/EventPipe
      // file in it belongs to the current capture, including child .NET PIDs.
      movedAnyJitdump =
        (await moveExistingGlob(dir, 'jit-*.dump', originalJitdumpTargetDir)) ||
        movedAnyJitdump;

      movedAnyEventPipe =
        (await moveExistingGlob(
          dir,
          'eventpipe-*.nettrace',
          eventPipeTargetDir,
        )) || movedAnyEventPipe;

      continue;
    }

    if (pids.size === 0) {
      // External directories may be user-owned shared locations such as /tmp.
      // Without a PID from jitdump-dotnet, broad globs can pick up stale files.
      engine.log(
        'warn',
        `Skipping external .NET jitdump directory '${dir}' because jitdump-dotnet did not report any PID for it.`,
      );
      continue;
    }

    // For non-APX directories, stage only files for PIDs reported by jitdump-dotnet.
    const jitdumpFiles = Array.from(pids, (pid) => `${dir}/jit-${pid}.dump`);
    const eventPipeFiles = Array.from(
      pids,
      (pid) => `${dir}/eventpipe-${pid}.nettrace`,
    );

    movedAnyJitdump =
      (await moveExistingFiles(jitdumpFiles, originalJitdumpTargetDir)) ||
      movedAnyJitdump;

    movedAnyEventPipe =
      (await moveExistingFiles(eventPipeFiles, eventPipeTargetDir)) ||
      movedAnyEventPipe;
  }

  if (!movedAnyJitdump) {
    const where =
      dirs.length === 1
        ? `'${dirs[0]}'`
        : `[${dirs.map((d) => `'${d}'`).join(', ')}]`;
    engine.log(
      'warn',
      `No .NET jitdump files were produced; skipping jitdump staging from ${where}.`,
    );
  }

  if (!movedAnyEventPipe) {
    const where =
      dirs.length === 1
        ? `'${dirs[0]}'`
        : `[${dirs.map((d) => `'${d}'`).join(', ')}]`;
    engine.log(
      'warn',
      `No .NET EventPipe trace files were produced; .NET source attribution may be unavailable from ${where}.`,
    );
  }
}

async function writeDotnetMergeLogs(engine, mergeArgs, mergeResult) {
  try {
    const stdoutHandle = await engine.createRunFile('jitdumpdotnet_merge.log', {
      name: 'log-text',
      version: '1.0',
    });
    await stdoutHandle.append(`Command: ${mergeArgs.join(' ')}\n\n`);
    await stdoutHandle.append(mergeResult.stdout || '');
    await stdoutHandle.close();

    const stderrHandle = await engine.createRunFile(
      'jitdumpdotnet_merge_stderr.txt',
      {
        name: 'log-text',
        version: '1.0',
      },
    );
    await stderrHandle.append(mergeResult.stderr || '');
    await stderrHandle.close();
  } catch (err) {
    engine.log(
      'warn',
      `Failed to write .NET jitdump merge logs: ${err?.message ?? err}`,
    );
  }
}

/**
 * Runs jitdump-dotnet merge mode after .NET jitdumps/EventPipe traces are staged into the APC directory.
 * If merge succeeds, enriched jitdumps replace the original .NET jitdumps before sl-analyze runs.
 * The merge command is best-effort: merge failures are logged and original jitdumps are moved into place.
 * Staging failures are fatal because sl-analyze would otherwise miss available jitdump inputs.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {string} originalJitdumpTargetDir
 * @param {string} jitdumpTargetDir
 * @returns {Promise<void>}
 */
async function mergeDotnetJitdumps(
  engine,
  ctx,
  originalJitdumpTargetDir,
  jitdumpTargetDir,
) {
  const mergeArgs = [
    `${ctx.metadata.dotnetAgentDeployPath}/jitdump-dotnet`,
    '--input-dir',
    ctx.metadata.captureDirectory,
  ];

  engine.log(
    'info',
    `Starting dotnet jitdump merge with args: ${mergeArgs.join(' ')}`,
  );

  const mergeResult = await engine.execCommand(mergeArgs, {
    asPrivileged: ctx.metadata.neoprofAsPrivileged,
  });
  await writeDotnetMergeLogs(engine, mergeArgs, mergeResult);

  if (mergeResult.stdout.trim()) {
    engine.log('info', mergeResult.stdout.trim());
  }

  if (mergeResult.stderr.trim()) {
    engine.log('warn', mergeResult.stderr.trim());
  }

  const jitdumpSourceDir =
    mergeResult.rc === 0
      ? `${ctx.metadata.captureDirectory}/${dotnetInputRelativeDir}/jitdumps-enriched`
      : originalJitdumpTargetDir;

  if (mergeResult.rc !== 0) {
    engine.log(
      'warn',
      `.NET jitdump merge failed with rc=${mergeResult.rc}; continuing with original jitdumps.`,
    );
  }

  const stageResult = await engine.execCommand(
    [
      'bash',
      '-c',
      `for file in "${jitdumpSourceDir}"/jit-*.dump; do [ -e "$file" ] || continue; mv -f "$file" "${jitdumpTargetDir}/" || exit 1; done`,
    ],
    { asPrivileged: ctx.metadata.neoprofAsPrivileged },
  );

  if (stageResult.rc !== 0) {
    throw {
      code: 'tool_integrations.neoprof.JITDUMP_MOVE_FAILED',
      metadata: {
        rc: stageResult.rc,
        sourceDir: jitdumpSourceDir,
        destinationDir: jitdumpTargetDir,
      },
      cause: stageResult.stderr
        ? stageResult.stderr.trim()
        : `failed to move .NET jitdump files from ${jitdumpSourceDir} to ${jitdumpTargetDir}`,
    };
  }

  if (mergeResult.rc === 0) {
    engine.log('info', '.NET enriched jitdumps staged for sl-analyze.');
  }
}

/**
 * Handles any JVM actions reported during a JVM jitdump capture.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @returns {Promise<void>}
 */
async function handleJvmActions(engine, path) {
  const actionsMap = new Map([
    [
      'MustAddPreserveFramePointerOption',
      {
        params: ['pid', 'must_add'],
        message: `JVM process {pid} was not run with the {must_add} option. Without this option, JVM stack traces may be incomplete or inaccurate. Rerun with this option to ensure accurate JVM stack traces.`,
        severity: 'warn',
      },
    ],
    [
      'ShouldAddEnableDynamicAgentLoading',
      {
        params: ['pid', 'should_add'],
        message: `JVM process {pid} was not run with the {should_add} option. Newer JVMs may remove the ability to dynamically load agents without this option. Include this option to ensure profiling will continue to work in future JVM versions.`,
        severity: 'info',
      },
    ],
  ]);

  await emitUserActionMessage(engine, path, actionsMap);
}

/**
 * Handles any .NET actions reported during a .NET jitdump capture.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @returns {Promise<void>}
 */
async function handleDotnetActions(engine, path) {
  const actionsMap = new Map([
    [
      'ShouldDisableWriteXORExecuteMemoryProtection',
      {
        params: ['pid', 'environmentVariable', 'expectedValue', 'currentValue'],
        message: `.NET process {pid} should set {environmentVariable}={expectedValue} (current value: {currentValue}). Without this setting, .NET stack/jitdump collection may be incomplete or fail.`,
        severity: 'warn',
      },
    ],
    [
      'MustSetJitFramedCompilationDisabled',
      {
        params: ['pid', 'environmentVariable', 'expectedValue', 'currentValue'],
        message: `.NET process {pid} must set {environmentVariable}={expectedValue} (current value: {currentValue}). Without this setting, .NET stack/jitdump collection may be incomplete or fail.`,
        severity: 'warn',
      },
    ],
    [
      'JitdumpOutputDirectory',
      {
        params: ['pid', 'environmentVariable', 'outputDirectory'],
        message: `.NET process {pid} is using {environmentVariable}={outputDirectory} for jitdump output.`,
        severity: 'info',
      },
    ],
  ]);

  await emitUserActionMessage(engine, path, actionsMap);
}

/**
 *
 * Formats user action messages and then emits them to the user message system.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @param {Map<string, {params: string[], message: string}>} actionsMap
 * @returns {Promise<void>}
 */
async function emitUserActionMessage(engine, path, actionsMap) {
  let actions = await getJitdumpActions(engine, path, actionsMap);
  for (const action of actions) {
    const actionInfo = actionsMap.get(action.code);
    if (!actionInfo) {
      continue;
    }
    let message = actionInfo.message;
    for (const key of actionInfo.params) {
      message = message.replaceAll(`{${key}}`, action.params[key]);
    }
    engine.writeUserMessage(actionInfo.severity, message);
  }
}

/**
 * Loads the jitdump user actions from the given path and validates the contained actions based on the actions map.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @param {Object.<string, any>} actionsMap
 * @returns {Promise<import("../recipes/docs/jsdocs").JitdumpAction[]>} Validated jitdump actions
 */
async function getJitdumpActions(engine, path, actionsMap) {
  const actions = await parseJitdumpActionsFile(engine, path);
  if (actions.length === 0) {
    return [];
  }

  const unexpectedPayloadMsg = `Payload for {code} jitdump user action is missing keys: {keys}`;
  const unexpectedCodeMsg = `Unknown jitdump user action code '{code}' reported`;

  let validatedActions = [];

  for (const action of actions) {
    const code = action.code;
    const payload = action.payload || {};

    const actionInfo = actionsMap.get(code);
    if (!actionInfo) {
      engine.log('error', unexpectedCodeMsg.replace('{code}', code));
      continue;
    }

    const expectedParams = actionInfo.params || [];
    let missingParams = [];
    for (const key of expectedParams) {
      if (!(key in payload)) {
        missingParams.push(key);
      }
    }
    if (missingParams.length > 0) {
      engine.log(
        'error',
        unexpectedPayloadMsg
          .replace('{code}', code)
          .replace('{keys}', missingParams.join(', ')),
      );
      continue;
    }

    validatedActions.push({ code: code, params: payload });
  }
  return validatedActions;
}

/**
 * Collects .NET jitdump output directories and PIDs from user actions.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} actionsFilePath
 * @param {string} fallbackDir
 * @returns {Promise<{directories: string[], pids: number[]}>}
 */
async function getDotnetJitdumpDirsFromActions(
  engine,
  actionsFilePath,
  fallbackDir,
) {
  const entries = await parseJitdumpActionsFile(engine, actionsFilePath);

  /** @type {Set<string>} */
  const dirs = new Set();
  /** @type {Set<number>} */
  const pids = new Set();

  if (fallbackDir) {
    dirs.add(fallbackDir);
  }

  for (const entry of entries) {
    if (entry.code !== 'JitdumpOutputDirectory') {
      continue;
    }

    const outDir = entry.payload && entry.payload.outputDirectory;
    if (typeof outDir === 'string') {
      const trimmed = outDir.trim();
      if (trimmed) {
        dirs.add(trimmed);
      }
    }

    const pid = Number(entry.payload && entry.payload.pid);
    if (Number.isInteger(pid) && pid > 0) {
      pids.add(pid);
    }
  }

  return { directories: Array.from(dirs), pids: Array.from(pids) };
}

/**
 * Parses a jitdump user actions file.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @returns {Promise<import("../recipes/docs/jsdocs").JitdumpActionEntry[]>}
 */
async function parseJitdumpActionsFile(engine, path) {
  const contents = await engine.execCommand(['cat', path], {});
  if (contents.rc !== 0) {
    return [];
  }

  let actions = [];
  const lines = contents.stdout.split('\n');
  for (let idx = 0; idx < lines.length; idx++) {
    const line = lines[idx].trim();
    if (!line) {
      continue;
    }
    try {
      let parsed = JSON.parse(line);
      actions.push(parsed);
    } catch (err) {
      engine.log(
        'error',
        `Failed to parse jitdump user action on line ${idx + 1} of '${path}': ${err.message}`,
      );
    }
  }
  return actions;
}

/**
 * Filters jitdump agent collection flags based on runtime detection for an attach PID.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {number} pid
 * @param {boolean} asPrivileged
 * @returns {Promise<{ isJvmPid: boolean, isDotnetPid: boolean }>}
 */
async function filterJitdumpAgentsForPid(engine, pid, asPrivileged) {
  const [isJvmPid, isDotnetPid] = await Promise.all([
    isJvmProcessPid(engine, pid, asPrivileged),
    isDotnetProcessPid(engine, pid, asPrivileged),
  ]);

  return { isJvmPid, isDotnetPid };
}

/**
 * Checks if a JVM hsperfdata file exists for the given PID.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {number} pid
 * @param {boolean} asPrivileged
 * @returns {Promise<boolean>}
 */

// TODO: This check needs to be replaced with an error code check from the JVM Agent.
//       This is the same check that is made in the JVM agent, we do not want to duplicate this logic.
async function isJvmProcessPid(engine, pid, asPrivileged) {
  const pidStr = String(pid);
  const check = await engine.execCommand(
    ['find', '/tmp', '-path', `/tmp/hsperfdata_*/${pidStr}`, '-print', '-quit'],
    { asPrivileged: asPrivileged },
  );
  return check.rc === 0 && check.stdout.trim().length > 0;
}

/**
 * Checks if a .NET diagnostic socket exists for the given PID.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {number} pid
 * @param {boolean} asPrivileged
 * @returns {Promise<boolean>}
 */

// TODO: This check needs to be replaced with an error code check from the .NET Agent.
//       This is the same check that is made in the .NET agent, we do not want to duplicate this logic.
async function isDotnetProcessPid(engine, pid, asPrivileged) {
  const pidStr = String(pid);
  const check = await engine.execCommand(
    [
      'find',
      '/tmp',
      '-path',
      `/tmp/dotnet-diagnostic-${pidStr}*`,
      '-print',
      '-quit',
    ],
    { asPrivileged: asPrivileged },
  );
  return check.rc === 0 && check.stdout.trim().length > 0;
}

/**
 * Registers jitdump log files as run artifacts and begins transferring them immediately.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {void}
 */
function immediateEmitJitdumpLogs(engine, ctx) {
  let files = [];
  if (ctx.metadata.jitdumpJvmAvailable) {
    files.push('jitdumpjvm.log', 'jitdumpjvm_stderr.txt');
  }
  if (ctx.metadata.dotnetAgentAvailable) {
    files.push('jitdumpdotnet.log', 'jitdumpdotnet_stderr.txt');
  }

  const outputDir = ctx.metadata.outputDirectory;
  for (const file of files) {
    engine.emitOutput(
      `${outputDir}/${file}`,
      file,
      {
        name: 'log-text',
        version: '1.0',
      },
      {
        immediateRetrieval: true,
      },
    );
  }
}

module.exports = {
  reformatJitdumps,
  immediateEmitJitdumpLogs,
  isJvmProcessPid,
  isDotnetProcessPid,
  filterJitdumpAgentsForPid,
  getDotnetJitdumpDirsFromActions,
  handleDotnetJitdumps,
};
