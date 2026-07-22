// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

// windows_system_info.go
// Build on Windows

package systeminfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

// Master switch for enabling slow command line retrieval fallback for processes
// that do not expose their command line via PEB (some system processes).
// Enabling this significantly downgrades performance of ListProcesses() --- it takes around 2 mins
// on a system that's not under load.
const AllowSlowCmdLineRetrieval = false

type userResolver func(string) (string, error)

// WindowsSystemInfo implements SystemInfo on Windows.
type WindowsSystemInfo struct{}

// ListProcesses enumerates all Windows processes using Toolhelp32 snapshot API.
// It retrieves command lines via PEB memory reading, falling back to WMI for
// processes that deny VM_READ access (if AllowSlowCmdLineRetrieval is enabled).
func (*WindowsSystemInfo) ListProcesses() ([]ProcessInfo, error) {
	processes, err := enumerateProcesses()
	if err != nil {
		return nil, message.New(message.AgentGrpcserverApiTargetAgentListProcesses).WithCause(err)
	}

	const processQueryFlags = windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.PROCESS_VM_READ
	for i := range processes {
		pid := uint32(processes[i].Pid)
		var cmd string

		// If it's an elevated process, we may get "access denied" here. In that case,
		// cmd and user remain empty.
		handle, openErr := windows.OpenProcess(processQueryFlags, false, pid)
		if openErr == nil {
			cmd, _ = getCmdLinePEB(handle)
			processes[i].User = lookupProcessOwnerFromHandle(handle)
			windows.CloseHandle(handle)
		}

		// Fallback to slow WMI method if PEB retrieval failed. Very slow!
		if cmd == "" && AllowSlowCmdLineRetrieval {
			if slowCmd, err := getCmdLineWMI(pid); err == nil {
				cmd = slowCmd
			}
		}
		processes[i].CmdLine = strings.TrimSpace(cmd)
	}

	sort.Slice(processes, func(i, j int) bool { return processes[i].Pid < processes[j].Pid })
	return processes, nil
}

// enumerateProcesses uses Toolhelp32 snapshot to collect PID and Name quickly.
func enumerateProcesses() ([]ProcessInfo, error) {
	hSnap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(hSnap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := windows.Process32First(hSnap, &entry); err != nil {
		return nil, err
	}

	var results []ProcessInfo
	for {
		pid := int32(entry.ProcessID)
		name := windows.UTF16ToString(entry.ExeFile[:])
		name = strings.TrimRight(name, "\x00")

		results = append(results, ProcessInfo{
			Pid:  pid,
			Name: name,
		})

		if err := windows.Process32Next(hSnap, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return results, err
		}
	}
	return results, nil
}

// lookupProcessOwner resolves DOMAIN\User for the given PID. Returns empty string on failure.
func lookupProcessOwner(pid uint32) string {
	hProc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(hProc)

	return lookupProcessOwnerFromHandle(hProc)
}

// lookupProcessOwnerFromHandle resolves DOMAIN\User for the given process handle. Returns empty string on failure.
func lookupProcessOwnerFromHandle(hProc windows.Handle) string {
	var hTok windows.Token
	if err := windows.OpenProcessToken(hProc, windows.TOKEN_QUERY, &hTok); err != nil {
		return ""
	}
	defer hTok.Close()

	tokenUser, err := hTok.GetTokenUser()
	if err != nil || tokenUser == nil {
		return ""
	}
	return resolveAccountSid(tokenUser.User.Sid)
}

// resolveAccountSid converts a SID (security identifier) to "DOMAIN\User" format.
func resolveAccountSid(sid *windows.SID) string {
	if sid == nil {
		return ""
	}

	var nameLen, domLen uint32
	var use uint32
	_ = lookupAccountSidAPI(nil, sid, nil, &nameLen, nil, &domLen, &use)

	var nameBuf, domainBuf []uint16
	var namePtr, domainPtr *uint16
	if nameLen > 0 {
		nameBuf = make([]uint16, nameLen)
		namePtr = &nameBuf[0]
	}
	if domLen > 0 {
		domainBuf = make([]uint16, domLen)
		domainPtr = &domainBuf[0]
	}

	if err := lookupAccountSidAPI(nil, sid, namePtr, &nameLen, domainPtr, &domLen, &use); err != nil {
		return ""
	}

	n := windows.UTF16ToString(nameBuf)
	d := windows.UTF16ToString(domainBuf)
	if d != "" && n != "" {
		return d + `\` + n
	}
	return n
}

// getCmdLinePEB walks the target process' PEB to retrieve its UNICODE_STRING command
// line directly from memory. This requires PROCESS_VM_READ plus NtQueryInformationProcess,
// making it very fast and dependency-free, but it fails for protected processes that
// deny VM_READ access or when the PEB/parameters are unavailable.
func getCmdLinePEB(hProc windows.Handle) (string, error) {
	var pbi processBasicInformation
	var retLen uint32
	if err := ntQueryInformationProcess(hProc, processBasicInfoClass, unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), &retLen); err != nil {
		return "", err
	}
	if pbi.PebBaseAddress == 0 {
		return "", errors.New("peb not available")
	}

	var p peb
	if err := readProcessMemory(hProc, pbi.PebBaseAddress, (*[unsafe.Sizeof(p)]byte)(unsafe.Pointer(&p))[:]); err != nil {
		return "", err
	}
	if p.ProcessParameters == 0 {
		return "", errors.New("process parameters not available")
	}

	var upp rtlUserProcessParameters
	if err := readProcessMemory(hProc, p.ProcessParameters, (*[unsafe.Sizeof(upp)]byte)(unsafe.Pointer(&upp))[:]); err != nil {
		return "", err
	}

	us := upp.CommandLine
	if us.Buffer == nil || us.Length == 0 {
		return "", nil
	}

	wsz := int(us.Length) / 2
	buf := make([]uint16, wsz)
	if err := readProcessMemory(hProc, uintptr(unsafe.Pointer(us.Buffer)), bytesFromU16(buf)); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf), nil
}

// getCmdLineWMI shells out to PowerShell + WMI (Get-CimInstance Win32_Process) and
// decodes the CommandLine property. This succeeds even when VM_READ access is denied
// but is orders of magnitude slower because it spawns PowerShell, queries CIM, and
// marshals JSON for each PID.
func getCmdLineWMI(pid uint32) (string, error) {
	ps := `Get-CimInstance Win32_Process -Filter "ProcessId=` + strconv.FormatUint(uint64(pid), 10) + `" | Select-Object -ExpandProperty CommandLine | ConvertTo-Json -Compress`
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if s == "" || s == "null" {
		return "", nil
	}
	var decoded string
	if json.Unmarshal([]byte(s), &decoded) == nil {
		return decoded, nil
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// ----------------- Native structs/syscalls -----------------

type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

type rtlUserProcessParameters struct {
	_             [16]byte
	_             [10]uintptr
	ImagePathName unicodeString
	CommandLine   unicodeString
}

type peb struct {
	_                 [2]byte
	BeingDebugged     byte
	_                 [1]byte
	_                 [2]uintptr
	Ldr               uintptr
	ProcessParameters uintptr
}

type processBasicInformation struct {
	Reserved1       uintptr
	PebBaseAddress  uintptr
	Reserved2       [2]uintptr
	UniqueProcessId uintptr
	Reserved3       uintptr
}

var (
	lookupAccountSidAPI = windows.LookupAccountSid

	// ntdll
	modntdll               = windows.NewLazySystemDLL("ntdll.dll")
	procNtQueryInformation = modntdll.NewProc("NtQueryInformationProcess")
	procRtlGetVersion      = modntdll.NewProc("RtlGetVersion")

	// kernel32
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procReadProcessMemoryAPI = modkernel32.NewProc("ReadProcessMemory")
)

const processBasicInfoClass = 0

func ntQueryInformationProcess(h windows.Handle, class uint32, info unsafe.Pointer, infoLen uint32, retLen *uint32) error {
	if err := modntdll.Load(); err != nil {
		return err
	}
	r1, _, e1 := procNtQueryInformation.Call(
		uintptr(h),
		uintptr(class),
		uintptr(info),
		uintptr(infoLen),
		uintptr(unsafe.Pointer(retLen)),
	)
	if r1 != 0 {
		if e1 != windows.ERROR_SUCCESS {
			return e1
		}
		return syscallErr("NtQueryInformationProcess", r1)
	}
	return nil
}

// readProcessMemory wraps the Windows ReadProcessMemory to read memory from another process.
func readProcessMemory(h windows.Handle, base uintptr, dst []byte) error {
	if len(dst) == 0 {
		return nil
	}
	if err := modkernel32.Load(); err != nil {
		return err
	}
	var read uintptr
	r1, _, e1 := procReadProcessMemoryAPI.Call(
		uintptr(h),
		base,
		uintptr(unsafe.Pointer(&dst[0])),
		uintptr(len(dst)),
		uintptr(unsafe.Pointer(&read)),
	)
	if r1 == 0 {
		if e1 != windows.ERROR_SUCCESS {
			return e1
		}
		return errors.New("ReadProcessMemory failed")
	}
	return nil
}

func bytesFromU16(s []uint16) []byte {
	hdr := (*[3]uintptr)(unsafe.Pointer(&s))
	bhdr := [3]uintptr{hdr[0], hdr[1] * 2, hdr[2] * 2}
	return *(*[]byte)(unsafe.Pointer(&bhdr))
}

func syscallErr(name string, r1 uintptr) error {
	return errors.New(name + " failed, NTSTATUS=0x" + strconv.FormatUint(uint64(r1), 16))
}

// ----------------- OS version (version-proof) -----------------

// OSVERSIONINFOEXW (subset)
type osVersionInfoExW struct {
	DwOSVersionInfoSize uint32
	DwMajorVersion      uint32
	DwMinorVersion      uint32
	DwBuildNumber       uint32
	DwPlatformId        uint32
	SzCSDVersion        [128]uint16
	WServicePackMajor   uint16
	WServicePackMinor   uint16
	WSuiteMask          uint16
	WProductType        byte
	WReserved           byte
}

// rtlGetVersion calls ntdll!RtlGetVersion directly to avoid x/sys version differences.
func rtlGetVersion() (osVersionInfoExW, error) {
	if err := modntdll.Load(); err != nil {
		return osVersionInfoExW{}, err
	}
	if err := procRtlGetVersion.Find(); err != nil {
		return osVersionInfoExW{}, err
	}

	var vi osVersionInfoExW
	vi.DwOSVersionInfoSize = uint32(unsafe.Sizeof(vi))

	ntStatus, _, callErr := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&vi)))
	if ntStatus != 0 {
		if callErr != windows.ERROR_SUCCESS {
			return osVersionInfoExW{}, callErr
		}
		return osVersionInfoExW{}, errors.New("RtlGetVersion failed")
	}
	return vi, nil
}

// GetKernelVersion returns the OS version as "major.minor.build".
func (*WindowsSystemInfo) GetKernelVersion() (string, error) {
	vi, err := rtlGetVersion()
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		strconv.FormatUint(uint64(vi.DwMajorVersion), 10),
		strconv.FormatUint(uint64(vi.DwMinorVersion), 10),
		strconv.FormatUint(uint64(vi.DwBuildNumber), 10),
	}, "."), nil
}

// Constructors remain consistent with other platforms.
func NewSystemInfo() SystemInfo {
	return NewSystemInfoWithOpts(afero.NewOsFs(), nil)
}

func NewSystemInfoWithOpts(_ afero.Fs, _ userResolver) SystemInfo {
	return &WindowsSystemInfo{}
}

func getString(k registry.Key, name string) string {
	v, _, err := k.GetStringValue(name)
	if err == nil {
		return v
	}
	return ""
}

func (*WindowsSystemInfo) GetOSDescription() (string, error) {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`,
		registry.QUERY_VALUE|registry.WOW64_64KEY,
	)
	if err != nil {
		return "", err
	}
	defer k.Close()

	product := getString(k, "ProductName")

	displayVersion := getString(k, "DisplayVersion")
	if displayVersion == "" {
		displayVersion = getString(k, "ReleaseId")
	}

	return fmt.Sprintf("%s %s", product, displayVersion), nil
}

func getProcessorName() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`,
		registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()

	s, _, err := k.GetStringValue("ProcessorNameString")
	return s, err
}

func (*WindowsSystemInfo) GetCPUTopology() (CPUTopology, error) {
	processorName, _ := getProcessorName()
	processorCount := windows.GetActiveProcessorCount(windows.ALL_PROCESSOR_GROUPS)

	// Assume a single cluster for now
	clusterInfo := []ClusterDescription{{
		ClusterID: uint32(0),
		Name:      "Cluster0",
	}}

	var cpus []CPUDescription
	for i := uint32(0); i < processorCount; i++ {
		cpus = append(cpus, CPUDescription{
			CoreNumber: i,
			ClusterID:  uint32(0),
			Midr:       "", // Not readily available on Windows
			Name:       processorName,
		})
	}

	return CPUTopology{
		PrimaryCPUName: processorName,
		CPUs:           cpus,
		ClusterInfo:    clusterInfo,
	}, nil
}
