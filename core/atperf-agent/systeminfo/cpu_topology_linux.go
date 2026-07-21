// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package systeminfo

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/afero"

	armcpus "github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo/armcpus"
)

const cpuSysfsPath = "/sys/devices/system/cpu"

type SysfsCPUTopologyProvider struct {
	fs afero.Fs
}

func NewSysfsCPUTopologyProvider(fs afero.Fs) CPUTopologyProvider {
	return &SysfsCPUTopologyProvider{fs: fs}
}

type cpuInfo struct {
	modelName      string
	cpuImplementer string
	cpuPart        string
}

type detectedCPU struct {
	cpuID      int
	clusterID  uint32
	hasCluster bool
	midr       string
	name       string
}

func (p *SysfsCPUTopologyProvider) GetCPUTopology() (CPUTopology, error) {
	cpuIDs, err := listCPUCoreIDs(p.fs)
	if err != nil {
		return CPUTopology{}, err
	}
	if len(cpuIDs) == 0 {
		return CPUTopology{}, fmt.Errorf("found no CPU directories under %s", cpuSysfsPath)
	}

	cpuInfoByID := map[int]cpuInfo{}
	if parsedCPUInfo, err := parseCPUInfo(p.fs); err == nil {
		cpuInfoByID = parsedCPUInfo
	}

	clusterIDs := map[uint32]struct{}{}
	detected := make([]detectedCPU, 0, len(cpuIDs))
	for _, cpuID := range cpuIDs {
		clusterID, ok := readClusterID(p.fs, cpuID)
		midr := readMIDR(p.fs, cpuID)
		name := resolveCPUName(cpuInfoByID[cpuID], midr)

		if ok {
			clusterIDs[clusterID] = struct{}{}
		}
		detected = append(detected, detectedCPU{
			cpuID:      cpuID,
			clusterID:  clusterID,
			hasCluster: ok,
			midr:       midr,
			name:       name,
		})
	}

	unknownClusterID := uint32(0xffffffff)
	for _, cpu := range detected {
		if !cpu.hasCluster {
			clusterIDs[unknownClusterID] = struct{}{}
			break
		}
	}

	cpus := make([]CPUDescription, 0, len(detected))
	for _, cpu := range detected {
		clusterID := cpu.clusterID
		if !cpu.hasCluster {
			clusterID = unknownClusterID
		}
		cpus = append(cpus, CPUDescription{
			CoreNumber: uint32(cpu.cpuID), // #nosec G115
			ClusterID:  clusterID,
			Midr:       cpu.midr,
			Name:       cpu.name,
		})
	}

	sortedClusterIDs := make([]uint32, 0, len(clusterIDs))
	for clusterID := range clusterIDs {
		sortedClusterIDs = append(sortedClusterIDs, clusterID)
	}
	sort.Slice(sortedClusterIDs, func(i, j int) bool {
		return sortedClusterIDs[i] < sortedClusterIDs[j]
	})

	var clusterInfo []ClusterDescription
	for i, clusterID := range sortedClusterIDs {
		clusterInfo = append(clusterInfo, ClusterDescription{
			ClusterID: clusterID,
			Name:      fmt.Sprintf("Cluster %d", i),
		})
	}

	primaryCPUName := ""
	if len(cpus) > 0 {
		primaryCPUName = cpus[0].Name
	}

	return CPUTopology{
		PrimaryCPUName: primaryCPUName,
		CPUs:           cpus,
		ClusterInfo:    clusterInfo,
	}, nil
}

func listCPUCoreIDs(fs afero.Fs) ([]int, error) {
	entries, err := afero.ReadDir(fs, cpuSysfsPath)
	if err != nil {
		return nil, err
	}

	var cpuIDs []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "cpu") {
			continue
		}
		cpuID, err := strconv.Atoi(strings.TrimPrefix(name, "cpu"))
		if err != nil {
			continue
		}
		cpuIDs = append(cpuIDs, cpuID)
	}

	sort.Ints(cpuIDs)
	return cpuIDs, nil
}

func parseCPUInfo(fs afero.Fs) (map[int]cpuInfo, error) {
	bytes, err := afero.ReadFile(fs, "/proc/cpuinfo")
	if err != nil {
		return nil, err
	}

	result := map[int]cpuInfo{}
	var current cpuInfo
	var currentCPU int
	hasCurrentCPU := false

	flush := func() {
		if hasCurrentCPU {
			result[currentCPU] = current
		}
		current = cpuInfo{}
		currentCPU = 0
		hasCurrentCPU = false
	}

	for _, line := range strings.Split(string(bytes), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}

		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch key {
		case "processor":
			cpuID, err := strconv.Atoi(value)
			if err != nil {
				continue
			}
			currentCPU = cpuID
			hasCurrentCPU = true
		case "model name":
			current.modelName = value
		case "cpu implementer":
			current.cpuImplementer = value
		case "cpu part":
			current.cpuPart = value
		}
	}
	flush()

	return result, nil
}

func readClusterID(fs afero.Fs, cpuID int) (uint32, bool) {
	clusterIDPath := filepath.Join(cpuSysfsPath, fmt.Sprintf("cpu%d", cpuID), "topology/cluster_id")
	if value, err := readTrimmedFile(fs, clusterIDPath); err == nil && value != "" {
		clusterID, err := strconv.ParseUint(value, 0, 32)
		if err == nil && uint32(clusterID) != uint32(0xffffffff) {
			return uint32(clusterID), true
		}
	}

	physicalPackageIDPath := filepath.Join(cpuSysfsPath, fmt.Sprintf("cpu%d", cpuID), "topology/physical_package_id")
	if value, err := readTrimmedFile(fs, physicalPackageIDPath); err == nil && value != "" {
		clusterID, err := strconv.ParseUint(value, 0, 32)
		if err == nil {
			return uint32(clusterID), true
		}
	}

	return 0, false
}

// readMIDR returns the value of the Arm CPU MIDR (Main ID Register)
// See https://developer.arm.com/documentation/ddi0601/2026-03/AArch64-Registers/MIDR-EL1--Main-ID-Register
// MIDR is Arm specific. For x86 CPUs readMIDR will return an empty string.
func readMIDR(fs afero.Fs, cpuID int) string {
	path := filepath.Join(cpuSysfsPath, fmt.Sprintf("cpu%d", cpuID), "regs/identification/midr_el1")
	value, err := readTrimmedFile(fs, path)
	if err != nil || value == "" {
		return ""
	}

	parsed, err := strconv.ParseUint(value, 0, 32)
	if err != nil {
		return strings.ToLower(value)
	}

	return fmt.Sprintf("0x%08x", parsed)
}

func readTrimmedFile(fs afero.Fs, path string) (string, error) {
	bytes, err := afero.ReadFile(fs, path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(bytes)), nil
}

func resolveCPUName(info cpuInfo, midr string) string {
	rawMIDR, ok := parseUintField(midr)
	if ok {
		implementer := (rawMIDR >> 24) & 0xff
		part := (rawMIDR >> 4) & 0xfff
		if name := armcpus.CpuidToName[(implementer<<12)|part]; name != "" {
			return name
		}
	}

	implementer, ok := parseUintField(info.cpuImplementer)
	if ok {
		part, ok := parseUintField(info.cpuPart)
		if ok {
			if name := armcpus.CpuidToName[(implementer<<12)|part]; name != "" {
				return name
			}
		}
	}
	if info.modelName != "" {
		return info.modelName
	}
	return "Unknown CPU"
}

func parseUintField(value string) (uint32, bool) {
	if strings.TrimSpace(value) == "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 0, 32)
	if err != nil {
		return 0, false
	}
	return uint32(parsed), true
}
