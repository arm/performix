// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const { getExecutableFromWorkload } = require('./workload');
const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';

// Stable Linux process capabilities
// See: https://github.com/torvalds/linux/blob/master/include/uapi/linux/capability.h
const LINUX_PROC_CAPABILITY_MAP = {
  cap_sys_admin: 21,
  cap_perfmon: 38,
};

/**
 * Checks python3 exists and version >= versionMajor.versionMinor
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {number} versionMajor major value of the minimum supported version
 * @param {number} versionMinor minor value of the minimum supported version
 * @param {string} toolName name of the tool requiring python, for error messages
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probePython(engine, versionMajor, versionMinor, toolName) {
  // Check Python is present.
  const pyCheck = await engine.execCommand(['python3', '--version'], {});
  if (pyCheck.rc !== 0) {
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message: `Python3 is not available on the target machine. Install the python3 system package in order to run ${toolName}.`,
      },
    };
  }

  // check python version is compatible
  const verCheck = await engine.execCommand(
    [
      'python3',
      '-c',
      `import sys; sys.exit(sys.version_info < (${versionMajor}, ${versionMinor}))`,
    ],
    {},
  );
  if (verCheck.rc !== 0) {
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message: `Python3 version is incompatible on the target machine. ${toolName} requires Python ${versionMajor}.${versionMinor}+.`,
      },
    };
  }

  return {
    level: 'ready',
    messageCode: '',
  };
}

/**
 * Checks python3 exists, version is compatible, and venv + pip work
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {number} versionMajor major value of the minimum supported version
 * @param {number} versionMinor minor value of the minimum supported version
 * @param {string} toolName name of the tool requiring python, for error messages
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probePythonVenv(engine, versionMajor, versionMinor, toolName) {
  // First check basic Python availability and version
  const basicCheck = await probePython(
    engine,
    versionMajor,
    versionMinor,
    toolName,
  );
  if (basicCheck.level !== 'ready') {
    return basicCheck;
  }

  // check venv can be created and used
  const tmpDir = await engine.createTempDir();
  const venvDir = `${tmpDir}/probe-venv`;
  const venvCreate = await engine.execCommand(
    ['python3', '-m', 'venv', venvDir],
    {},
  );
  if (venvCreate.rc !== 0) {
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message: `Could not create a Python venv on the target machine. Make sure the python3-venv system package is installed in order to run ${toolName}.`,
      },
    };
  }

  const venvRun = await engine.execCommand(
    [`${venvDir}/bin/python3`, '--version'],
    {},
  );
  if (venvRun.rc !== 0) {
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message: `A Python venv was created but Python could not be executed inside it. Ensure the python3-venv package is correctly installed to run ${toolName}.`,
      },
    };
  }

  const pipRun = await engine.execCommand(
    [`${venvDir}/bin/pip3`, '--version'],
    {},
  );
  if (pipRun.rc !== 0) {
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message: `A Python venv was created but pip could not be executed inside it. Ensure pip is installed in order to run ${toolName}.`,
      },
    };
  }

  return {
    level: 'ready',
    messageCode: '',
  };
}

/**
 * Checks if a file or directory exists at the specified path
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path - The path to check
 * @returns {Promise<boolean>} - True if the path exists, false otherwise
 */
async function pathExists(engine, path) {
  const checkResult = await engine.execCommand(['stat', path], {});
  return checkResult.rc === 0;
}

/**
 * Checks that a path is deployed and returns ProbeAdvice for use in probe functions
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} deployPath - Path to check
 * @param {string} toolName - Name of the tool for error metadata
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeDeployment(engine, deployPath, toolName) {
  if (!(await pathExists(engine, deployPath))) {
    return {
      level: 'error',
      messageCode: 'tool_integrations.common.TOOL_NOT_DEPLOYED',
      metadata: {
        tool: toolName,
        deployPath: deployPath,
        locality: engine.getLocality(),
      },
    };
  }
  return {
    level: 'ready',
    messageCode: '',
  };
}

/**
 * Checks that a wheel is deployed at the specified path (alias for probeDeployment)
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} deployPath
 * @param {string} toolName
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeWhl(engine, deployPath, toolName) {
  return await probeDeployment(engine, deployPath, toolName);
}

/**
 * Checks that a path is deployed and throws an error if not, for use in run functions
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} deployPath - Path to check
 * @param {string} toolName - Name of the tool for error metadata
 * @throws {Object} Error with code and metadata if path doesn't exist
 */
async function ensureDeployed(engine, deployPath, toolName) {
  if (!(await pathExists(engine, deployPath))) {
    throw {
      code: 'tool_integrations.common.TOOL_NOT_DEPLOYED',
      metadata: {
        tool: toolName,
        deployPath: deployPath,
        locality: engine.getLocality(),
      },
    };
  }
}

/**
 * (Linux-only) Retrieves the value stored in /proc/sys/kernel/perf_event_paranoid.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} toolName
 * @returns {Promise<number|null>}
 */
async function getPerfSetting(engine, toolName) {
  // Skip on non-Linux targets
  const unameCmd = ['uname', '-s'];
  let platformCheck = await engine.execCommand(unameCmd, {});
  if (platformCheck.rc !== 0 || platformCheck.stdout.trim() !== 'Linux') {
    return null;
  }

  const perfParanoidCmd = ['cat', '/proc/sys/kernel/perf_event_paranoid'];
  let perfParanoidResult = await engine.execCommand(perfParanoidCmd, {});
  if (perfParanoidResult.rc !== 0) {
    throw {
      code: 'tool_integrations.common.PRIVILEGE_CHECK_FAILED_PERF_PARANOID',
      metadata: {
        tool: toolName,
        cmd: perfParanoidCmd.join(' '),
        exitCode: perfParanoidResult.rc,
      },
    };
  }

  return parseInt(perfParanoidResult.stdout.trim());
}

/**
 * (Linux-only) Retrieves the effective capabilities (i.e. CapEff) of the
 * target agent process and returns if target allows profiling for the neoprof tool.
 *
 * TODO: Ideally we would be returning the list of capabilities and delegate the
 * profiling decision to the caller, but the mechanism we rely on (capsh and manual)
 * returns different formats (names vs indexes). We need to normalised them, but
 * instead of spending time on that, we should work on APAP-3108 to introduce
 * a better mechanism to retrieve process capabilities.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<boolean>}
 */
async function isPerfCapable(engine) {
  // Skip on non-Linux targets
  const unameCmd = ['uname', '-s'];
  let platformCheck = await engine.execCommand(unameCmd, {});
  if (platformCheck.rc !== 0 || platformCheck.stdout.trim() !== 'Linux') {
    return null;
  }

  // 0. Find the target agent's PID from the command's parent PID, then read the
  // agent's effective capabilities. Read the parent directly because a child
  // executable can lose capabilities during exec.
  // Format is `CapEff: hexadecimal_value`
  // See: https://man7.org/linux/man-pages/man5/proc_pid_status.5.html
  const childStatusCmd = ['cat', '/proc/self/status'];
  const childStatusResult = await engine.execCommand(childStatusCmd, {});
  const parentPidMatch = childStatusResult.stdout.match(/^PPid:\s*(\d+)\s*$/m);
  if (childStatusResult.rc !== 0 || parentPidMatch === null) {
    throw {
      code: 'tool_integrations.common.PRIVILEGE_CHECK_FAILED_PERF_CAPS',
      metadata: {
        capabilities: 'cap_perfmon, cap_sys_admin',
        cmd: childStatusCmd.join(' '),
        exitCode: childStatusResult.rc,
      },
    };
  }

  const statusCmd = ['cat', `/proc/${parentPidMatch[1]}/status`];
  const statusResult = await engine.execCommand(statusCmd, {});
  const capEffMatch = statusResult.stdout.match(
    /^CapEff:\s*([0-9a-fA-F]+)\s*$/m,
  );
  if (statusResult.rc !== 0 || capEffMatch === null) {
    throw {
      code: 'tool_integrations.common.PRIVILEGE_CHECK_FAILED_PERF_CAPS',
      metadata: {
        capabilities: 'cap_perfmon, cap_sys_admin',
        cmd: statusCmd.join(' '),
        exitCode: statusResult.rc,
      },
    };
  }

  const capEff = capEffMatch[1];

  // 1. Decode using `capsh`
  const capshResult = await engine.execCommand(
    ['capsh', `--decode=${capEff}`],
    {},
  );
  if (capshResult.rc === 0) {
    const decodedCapabilities = capshResult.stdout.trim();
    const separatorIndex = decodedCapabilities.indexOf('=');
    const capabilityList =
      separatorIndex === -1
        ? decodedCapabilities
        : decodedCapabilities.slice(separatorIndex + 1);
    const capNames = capabilityList
      .split(',')
      .map((name) => name.trim())
      .filter((name) => name.length > 0);

    for (const capName of capNames) {
      if (capName === 'cap_perfmon' || capName === 'cap_sys_admin') {
        return true;
      }
    }
  }

  // 2. Decode manually -- in case `capsh` is not available
  // Maximum of 64 capabilities supported; see:
  // https://github.com/torvalds/linux/blob/master/include/uapi/linux/capability.h
  const capBits = BigInt('0x' + capEff);
  const capabilities = [];
  for (let capIndex = 0; capIndex < 64; capIndex++) {
    if ((capBits & (BigInt(1) << BigInt(capIndex))) !== BigInt(0)) {
      capabilities.push(capIndex);
    }
  }

  for (const capIndex of capabilities) {
    if (
      capIndex === LINUX_PROC_CAPABILITY_MAP.cap_perfmon ||
      capIndex === LINUX_PROC_CAPABILITY_MAP.cap_sys_admin
    ) {
      return true;
    }
  }

  return false;
}

/**
 * Attempts to find the login name (a.k.a current user) on the target connection.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<string>} The login name.
 */
async function resolveLoginName(engine) {
  const lognameCmd = await engine.execCommand(['logname'], {});
  if (lognameCmd.rc === 0) {
    return lognameCmd.stdout.trim();
  }

  // Fallback: SUDO_USER or USER (in that order)
  const envCmd = await engine.execCommand(['env'], {});
  if (envCmd.rc === 0) {
    const envLines = envCmd.stdout.split('\n');
    for (const key of ['SUDO_USER=', 'USER=']) {
      const line = envLines.find((l) => l.startsWith(key));
      if (line) {
        return line.substring(key.length).trim();
      }
    }
  }

  throw {
    code: 'tool_integrations.common.LOGIN_NAME_NOT_FOUND',
    metadata: { lognameRc: lognameCmd.rc, envRc: envCmd.rc },
  };
}

/**
 * Gets the primary group for the specified user on the target connection.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} loginName
 * @returns {Promise<string>}
 */
async function getPrimaryGroup(engine, loginName) {
  const cmd = ['id', '-gn', loginName];
  const result = await engine.execCommand(cmd, {});
  if (result.rc === 0) {
    return result.stdout.trim();
  }
  throw {
    code: 'tool_integrations.common.GET_PRIMARY_GROUP_FAILED',
    metadata: { username: loginName, cmd: cmd.join(` `), exitCode: result.rc },
    cause: result.stderr,
  };
}

/**
 * Runs the POSIX `test` command on target to check workload permissions.
 * See: https://pubs.opengroup.org/onlinepubs/9699919799/utilities/test.html
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").WorkloadLaunch} workload
 * @param {string[]} args
 * @returns {Promise<boolean>}
 */
async function posixTestWorkload(engine, workload, args) {
  if (workload.length === 0) {
    return true;
  }

  // Binary (stripped from args)
  let targetBinary = getExecutableFromWorkload(workload.command);
  if (targetBinary.length === 0) {
    return true;
  }

  // Run `test` against it
  let filePath = await resolveWorkloadPath(engine, targetBinary);
  let testResult = await engine.execCommand(['test', ...args, filePath], {
    workingDirectory: workload.workingDir,
  });
  if (testResult.rc !== 0) {
    return false;
  }

  return true;
}

/**
 * Resolve a workload command to an executable path on the target.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} commandPath
 * @returns {Promise<string>}
 */
async function resolveWorkloadPath(engine, commandPath) {
  // If it's a path already, return it directly
  if (commandPath.includes('/')) {
    return commandPath;
  }

  // For commands visible to PATH (e.g., `ls`)
  let whichResult = await engine.execCommand(['which', commandPath], {});
  if (whichResult.rc === 0) {
    let resolved = whichResult.stdout.trim();
    if (resolved.length > 0) {
      return resolved;
    }
  }

  // Fallback to original
  return commandPath;
}

/**
 * Checks if the given error is an elevate privilege error.
 *
 * @param {error} err
 * @returns {boolean}
 */
function isElevatePrivilegeError(err) {
  const str = String(err);
  if (str.includes('engine.tool.service.ELEVATE_PRIVILEGES_FAILED')) {
    return true;
  }

  return false;
}

/**
 * Normalize ownership and permissions of output files or directories created via privilege elevation.
 * This ensures output created via privilege elevation is readable by the underlying user.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputPath
 * @param {boolean} recursive
 * @returns {Promise<void>}
 */
async function normalizeRootOutputAccess(engine, outputPath, recursive) {
  const loginName = await resolveLoginName(engine);
  const primaryGroup = await getPrimaryGroup(engine, loginName);
  const chownArgs = recursive
    ? ['chown', '-R', `${loginName}:${primaryGroup}`, outputPath]
    : ['chown', `${loginName}:${primaryGroup}`, outputPath];
  const chmodArgs = recursive
    ? ['chmod', '-R', 'u+rwX,go+rX', outputPath]
    : ['chmod', 'u+rwX,go+rX', outputPath];

  const chownResult = await engine.execCommand(chownArgs, {
    asPrivileged: true,
  });
  if (chownResult.rc !== 0) {
    engine.log(
      'error',
      `Failed to normalize root output ownership for '${outputPath}': ${chownResult.stderr}`,
    );
    throw {
      code: 'tool_integrations.common.NORMALIZE_ROOT_OUTPUT_ACCESS_FAILED',
      metadata: {
        cmd: chownArgs.join(' '),
        path: outputPath,
        exitCode: chownResult.rc,
      },
    };
  }

  const chmodResult = await engine.execCommand(chmodArgs, {
    asPrivileged: true,
  });
  if (chmodResult.rc !== 0) {
    engine.log(
      'error',
      `Failed to normalize root output permissions for '${outputPath}': ${chmodResult.stderr}`,
    );
    throw {
      code: 'tool_integrations.common.NORMALIZE_ROOT_OUTPUT_ACCESS_FAILED',
      metadata: {
        cmd: chmodArgs.join(' '),
        path: outputPath,
        exitCode: chmodResult.rc,
      },
    };
  }
}

/**
 * Build the deployed bundle path under the tools root.
 * TODO: When engine provides bundle lookup by name/version, replace this helper with engine-supplied paths.
 * @param {string} toolsRoot
 * @param {string} bundleName
 * @param {string} bundleVersion
 * @returns {string}
 */
function buildToolBundlePath(toolsRoot, bundleName, bundleVersion) {
  return `${toolsRoot}/${bundleName}/${bundleVersion}/${bundleName}`;
}

module.exports = {
  probePython,
  probePythonVenv,
  probeDeployment,
  probeWhl,
  ensureDeployed,
  getPerfSetting,
  isPerfCapable,
  resolveLoginName,
  isElevatePrivilegeError,
  normalizeRootOutputAccess,
  posixTestWorkload,
  buildToolBundlePath,
};
