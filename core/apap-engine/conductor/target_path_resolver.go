// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-agent/agentconfig"
)

const tools = "tools"

// ResolveToolsBaseDir returns a target-side tools base directory for current flows:
//   - POSIX: uses an explicit baseDir when provided (expands ~ / relative paths, checks existence/writability),
//     otherwise uses HOME/.local/share/<product>/tools if writable; no fallback.
//   - Windows: uses an explicit baseDir when provided (supports ~ via USERPROFILE and relative paths, checks existence/writability),
//     otherwise uses LOCALAPPDATA/<product>/tools; no fallback.
//   - Android: uses an explicit baseDir when provided, otherwise uses /data/local/tmp/<product>/tools.
func ResolveToolsBaseDir(baseDir string, platformOS OS, cmdRunner CommandRunner, localityName string) (string, error) {
	switch platformOS {
	case Linux, Darwin:
		return resolve(baseDir, cmdRunner, resolverOps{
			needsRoot: func(b string) bool { return b == "" || strings.HasPrefix(b, "~") || !isPosixAbs(util.ForceToSlash(b)) },
			readRoot:  readPosixHome,
			expand:    expandPosixBase,
			exists:    posixPathExists,
			writable:  checkPosixWritable,
			defaultBase: func(root string) string {
				return path.Join(root, ".local", "share", terminology.GetProductBinaryName(), tools)
			},
			join: path.Join,
			lockRoot: func() string {
				return agentconfig.GetDefaultLockRootDirectory(runtime.GOOS)
			},
			errRootMissing: func(base string) error {
				return message.New(message.EngineToolTargetPathTargetHomeUnavailable).WithMetadata(map[string]string{"path": base, "locality": localityName})
			},
			isWindows:    false,
			localityName: localityName,
		})
	case Win:
		readRoot := readWindowsLocalAppData
		if strings.HasPrefix(baseDir, "~") {
			readRoot = readWindowsUserProfile
		}
		return resolve(baseDir, cmdRunner, resolverOps{
			needsRoot: func(b string) bool { return b == "" || strings.HasPrefix(b, "~") || !isWindowsAbs(b) },
			readRoot:  readRoot,
			expand:    expandWindowsBase,
			exists:    windowsPathExists,
			writable:  checkWindowsWritable,
			defaultBase: func(root string) string {
				return windowsJoin(root, terminology.GetProductBinaryName(), tools)
			},
			join: windowsJoin,
			lockRoot: func() string {
				return agentconfig.GetDefaultLockRootDirectory(runtime.GOOS)
			},
			errRootMissing: func(_ string) error {
				return message.New(message.EngineToolTargetPathWinLocalappdataUnavailable).WithMetadata(map[string]string{"locality": localityName})
			},
			isWindows:    true,
			localityName: localityName,
		})
	case Android:
		return resolve(baseDir, cmdRunner, resolverOps{
			needsRoot: func(b string) bool { return b == "" || strings.HasPrefix(b, "~") || !isPosixAbs(util.ForceToSlash(b)) },
			readRoot: func(CommandRunner) (string, error) {
				return DefaultAndroidTempDir, nil
			},
			expand:   expandPosixBase,
			exists:   posixPathExists,
			writable: checkPosixWritable,
			defaultBase: func(root string) string {
				return path.Join(root, terminology.GetProductBinaryName(), tools)
			},
			join: path.Join,
			lockRoot: func() string {
				return agentconfig.GetDefaultLockRootDirectory(runtime.GOOS)
			},
			errRootMissing: func(base string) error {
				return message.New(message.EngineToolTargetPathTargetHomeUnavailable).WithMetadata(map[string]string{"path": base, "locality": localityName})
			},
			isWindows:    false,
			localityName: localityName,
		})
	default:
		return baseDir, nil
	}
}

type resolverOps struct {
	needsRoot      func(string) bool
	readRoot       func(CommandRunner) (string, error)
	expand         func(base, root string) string
	exists         func(CommandRunner, string) (bool, error)
	writable       func(CommandRunner, string) (bool, error)
	defaultBase    func(string) string
	join           func(elem ...string) string
	lockRoot       func() string
	errRootMissing func(base string) error
	isWindows      bool
	localityName   string
}

func resolve(baseDir string, cmdRunner CommandRunner, ops resolverOps) (string, error) {
	var root string
	if ops.needsRoot(baseDir) {
		var err error
		root, err = ops.readRoot(cmdRunner)
		if err != nil {
			return "", err
		}
		if root == "" {
			return "", ops.errRootMissing(baseDir)
		}
	}

	base := ops.expand(baseDir, root)

	lockRoot := normalizeForCompare(ops.isWindows, ops.lockRoot())
	normalisedBase := normalizeForCompare(ops.isWindows, base)
	if normalisedBase == lockRoot {
		return "", message.New(message.EngineToolTargetPathLockDirConflict).WithMetadata(map[string]string{"path": base, "locality": ops.localityName})
	}

	exists, err := ops.exists(cmdRunner, base)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", message.New(message.EngineToolTargetPathDirMissing).WithMetadata(map[string]string{"path": base, "locality": ops.localityName})
	}

	ok, err := ops.writable(cmdRunner, base)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", message.New(message.EngineToolTargetPathNoWritableToolsPath).WithMetadata(map[string]string{"path": base, "locality": ops.localityName})
	}

	if baseDir == "" && ops.defaultBase != nil {
		base = ops.defaultBase(base)
	} else {
		base = ops.join(base, terminology.GetProductBinaryName(), tools)
	}
	return base, nil
}

func expandPosixBase(baseDir, root string) string {
	base := util.ForceToSlash(baseDir)
	if base == "" {
		base = root
	}
	if strings.HasPrefix(base, "~") || (base != "" && !isPosixAbs(base)) {
		base = path.Join(root, strings.TrimPrefix(base, "~"))
	}
	return base
}

func expandWindowsBase(baseDir, root string) string {
	base := baseDir
	if base == "" {
		base = root
	}
	if strings.HasPrefix(base, "~") || (base != "" && !isWindowsAbs(base)) {
		base = windowsJoin(root, strings.TrimPrefix(base, "~"))
	}
	return base
}

func isWindowsAbs(p string) bool {
	if p == "" {
		return false
	}
	// Treat drive-letter, UNC, or slash-prefixed as absolute.
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `/`) || strings.HasPrefix(p, `\`) {
		return true
	}
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	return false
}

func isPosixAbs(p string) bool {
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`)
}

// windowsJoin builds a Windows path using forward slashes to avoid host-specific separators.
func windowsJoin(elem ...string) string {
	clean := filepath.Clean(strings.Join(elem, "/"))
	return util.ForceToSlash(clean)
}

func normalizeForCompare(isWindows bool, p string) string {
	p = util.ForceToSlash(filepath.Clean(p))
	if isWindows {
		if len(p) >= 2 && p[1] == ':' {
			p = p[2:]
			if !strings.HasPrefix(p, "/") {
				p = "/" + p
			}
		}
	}
	return path.Clean(p)
}

// GetHomeDir fetches the user's home directory from the target, using $HOME for POSIX targets
// and %USERPROFILE% on Windows machines.
func GetHomeDir(platformOS OS, cmdRunner CommandRunner) (home string, err error) {
	switch platformOS {
	case Android, Linux, Darwin:
		return readPosixHome(cmdRunner)
	case Win:
		return readWindowsHome(cmdRunner)
	default:
		return "", message.New(message.EngineCommonUnsupportedTargetOs).WithMetadata(map[string]string{"os": string(platformOS)})
	}
}

// readPosixHome fetches $HOME from the target.
func readPosixHome(cmdRunner CommandRunner) (home string, err error) {
	homeOut, _, homeErr := cmdRunner.RunCommand("printenv HOME")
	if homeErr != nil {
		return "", homeErr
	}
	return strings.TrimSpace(homeOut), nil
}

// readWindowsHome fetches %USERPROFILE% from the target.
func readWindowsHome(cmdRunner CommandRunner) (home string, err error) {
	userprofileOut, _, userprofileErr := cmdRunner.RunCommand(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:USERPROFILE"`)
	if userprofileErr != nil {
		return "", userprofileErr
	}
	return strings.TrimSpace(strings.ReplaceAll(userprofileOut, `\`, `/`)), nil
}

// checkPosixWritable probes target writability with `test -w`.
func checkPosixWritable(cmdRunner CommandRunner, path string) (bool, error) {
	_, _, err := cmdRunner.RunCommand(fmt.Sprintf(`test -w %s`, filepath.ToSlash(path)))
	if err != nil {
		return false, nil
	}
	return true, nil
}

// readWindowsLocalAppData fetches LOCALAPPDATA from the target.
func readWindowsLocalAppData(cmdRunner CommandRunner) (localAppData string, err error) {
	stdout, _, envErr := cmdRunner.RunCommand(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:LOCALAPPDATA"`)
	localAppData = strings.TrimSpace(strings.ReplaceAll(stdout, `\`, `/`))
	if localAppData == "" && envErr != nil {
		return "", envErr
	}
	return localAppData, nil
}

// readWindowsUserProfile fetches USERPROFILE from the target.
func readWindowsUserProfile(cmdRunner CommandRunner) (userProfile string, err error) {
	stdout, _, envErr := cmdRunner.RunCommand(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "echo $env:USERPROFILE"`)
	userProfile = util.ForceToSlash(strings.TrimSpace(stdout))
	if userProfile == "" && envErr != nil {
		return "", envErr
	}
	return userProfile, nil
}

// checkWindowsWritable attempts a temporary write under the provided Windows path on the target.
func checkWindowsWritable(cmdRunner CommandRunner, path string) (bool, error) {
	script := fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$p = '%s'; $tmp = Join-Path $p '%v-writecheck.tmp'; try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"`, path, terminology.GetProductBinaryName())
	_, _, err := cmdRunner.RunCommand(script)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func posixPathExists(cmdRunner CommandRunner, path string) (bool, error) {
	_, _, err := cmdRunner.RunCommand(fmt.Sprintf(`test -e %s`, filepath.ToSlash(path)))
	if err != nil {
		return false, nil
	}
	return true, nil
}

func windowsPathExists(cmdRunner CommandRunner, path string) (bool, error) {
	script := fmt.Sprintf(`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "if (Test-Path '%s') { exit 0 } else { exit 1 }"`, path)
	_, _, err := cmdRunner.RunCommand(script)
	if err != nil {
		return false, nil
	}
	return true, nil
}
