// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
)

const runDetailsSummaryName = "run_details"

const runDetailsOSComponentType = "target-info-os"
const runDetailsCPUsComponentType = "target-info-cpus"

// RunDetailsSummarizer is an UnbudgetedRunSummarizer for summarizing the details of a run.
var RunDetailsSummarizer = UnbudgetedRunSummarizer{
	Name:      runDetailsSummaryName,
	Summarize: SummarizeRunDetails,
}

func SummarizeRunDetails(ctx context.Context, desc *run.RunDescription, session render.Session) (RunSummary, error) {
	payload := runDetailsPayload{
		RunID:               desc.ID,
		RunName:             desc.Name,
		RecipeName:          desc.RecipeName,
		RunResult:           desc.RunResult,
		RunError:            desc.RunError,
		WorkloadType:        desc.WorkloadType,
		WorkloadCmdline:     desc.Cmdline,
		AndroidPackageName:  desc.AndroidPackageName,
		AndroidActivityName: desc.AndroidActivityName,
		TargetName:          desc.TargetName,
		StartTime:           desc.StartTime,
		EndTime:             desc.EndTime,
		Timeout:             desc.Timeout,
	}

	if len(desc.Parameters) > 0 {
		payload.SamplingParameters = desc.Parameters
	}

	// Target environment is best-effort: older runs and non-Arm targets may not
	// have these tables, so a missing or unreadable table omits the section
	// rather than failing the whole summary.
	if session != nil && session.Database() != nil {
		payload.OS = loadRunDetailsOSInfo(ctx, session)
		payload.CPUTopology = loadRunDetailsCPUTopology(ctx, session)
	}

	return NewRunSummary(runDetailsSummaryName, runDetailsPromptFragment, payload)
}

// loadRunDetailsOSInfo reads the target operating system description and kernel
// version from the rendered target-info-os table, if present.
func loadRunDetailsOSInfo(ctx context.Context, session render.Session) *runDetailsOSInfo {
	tableName := singleTableByComponentType(session.Manifest().Entries(), runDetailsOSComponentType)
	if tableName == "" {
		return nil
	}

	row := session.Database().Conn.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COALESCE(os_description, ''), COALESCE(kernel_version, '') FROM %s LIMIT 1`,
		tableName,
	))

	var description, kernelVersion string
	if err := row.Scan(&description, &kernelVersion); err != nil {
		return nil
	}
	if description == "" && kernelVersion == "" {
		return nil
	}

	return &runDetailsOSInfo{Description: description, KernelVersion: kernelVersion}
}

// loadRunDetailsCPUTopology reads per-core CPU rows from the rendered
// target-info-cpus table and groups cores sharing the same model name and MIDR
// into CPU types, mirroring the heterogeneous-aware topology view.
func loadRunDetailsCPUTopology(ctx context.Context, session render.Session) *runDetailsCPUTopology {
	tableName := singleTableByComponentType(session.Manifest().Entries(), runDetailsCPUsComponentType)
	if tableName == "" {
		return nil
	}

	rows, err := session.Database().Conn.QueryContext(ctx, fmt.Sprintf(
		`SELECT COALESCE(name, ''), COALESCE(midr, ''), cluster_id FROM %s`,
		tableName,
	))
	if err != nil {
		return nil
	}
	defer rows.Close()

	type cpuGroupKey struct {
		name string
		midr string
	}
	type cpuGroup struct {
		name       string
		midr       string
		coreCount  int
		clusterIDs map[uint32]struct{}
	}
	groupOrder := []cpuGroupKey{}
	groups := map[cpuGroupKey]*cpuGroup{}
	totalCPUCount := 0

	for rows.Next() {
		var name, midr string
		var clusterID sql.NullInt64
		if err := rows.Scan(&name, &midr, &clusterID); err != nil {
			return nil
		}
		totalCPUCount++

		key := cpuGroupKey{name: name, midr: midr}
		group, ok := groups[key]
		if !ok {
			group = &cpuGroup{name: name, midr: midr, clusterIDs: map[uint32]struct{}{}}
			groups[key] = group
			groupOrder = append(groupOrder, key)
		}
		group.coreCount++
		if clusterID.Valid && clusterID.Int64 >= 0 && clusterID.Int64 <= math.MaxUint32 {
			group.clusterIDs[uint32(clusterID.Int64)] = struct{}{} // #nosec G115 - bounds checked above
		}
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	if totalCPUCount == 0 {
		return nil
	}

	cpuTypes := make([]runDetailsCPUType, 0, len(groupOrder))
	for _, key := range groupOrder {
		group := groups[key]
		clusterIDs := make([]uint32, 0, len(group.clusterIDs))
		for clusterID := range group.clusterIDs {
			clusterIDs = append(clusterIDs, clusterID)
		}
		sort.Slice(clusterIDs, func(i, j int) bool { return clusterIDs[i] < clusterIDs[j] })
		cpuTypes = append(cpuTypes, runDetailsCPUType{
			Name:       group.name,
			Midr:       group.midr,
			CoreCount:  group.coreCount,
			ClusterIDs: clusterIDs,
		})
	}
	sort.SliceStable(cpuTypes, func(i, j int) bool {
		if cpuTypes[i].CoreCount != cpuTypes[j].CoreCount {
			return cpuTypes[i].CoreCount > cpuTypes[j].CoreCount
		}
		if cpuTypes[i].Name != cpuTypes[j].Name {
			return cpuTypes[i].Name < cpuTypes[j].Name
		}
		return cpuTypes[i].Midr < cpuTypes[j].Midr
	})

	return &runDetailsCPUTopology{TotalCPUCount: totalCPUCount, CPUTypes: cpuTypes}
}

const runDetailsPromptFragment = "Run metadata. SamplingParameters (optional) holds the profiling recipe parameters. Os (optional) holds the target operating system Description and KernelVersion. CpuTopology (optional) summarizes the target CPUs: TotalCpuCount is the total core count, and CpuTypes groups cores sharing the same model Name and Midr (Arm MIDR_EL1 register value), each with its CoreCount and the ClusterIds those cores belong to."

type runDetailsPayload struct {
	RunID               string                 `json:"RunId"`
	RunName             string                 `json:"RunName"`
	RecipeName          string                 `json:"RecipeName"`
	RunResult           string                 `json:"RunResult"`
	RunError            string                 `json:"RunError"`
	WorkloadType        string                 `json:"WorkloadType"`
	WorkloadCmdline     string                 `json:"WorkloadCmdline"`
	AndroidPackageName  string                 `json:"AndroidPackageName,omitempty"`
	AndroidActivityName string                 `json:"AndroidActivityName,omitempty"`
	TargetName          string                 `json:"TargetName"`
	StartTime           string                 `json:"StartTime"`
	EndTime             string                 `json:"EndTime"`
	Timeout             uint32                 `json:"Timeout"`
	SamplingParameters  map[string]any         `json:"SamplingParameters,omitempty"`
	OS                  *runDetailsOSInfo      `json:"Os,omitempty"`
	CPUTopology         *runDetailsCPUTopology `json:"CpuTopology,omitempty"`
}

type runDetailsOSInfo struct {
	Description   string `json:"Description,omitempty"`
	KernelVersion string `json:"KernelVersion,omitempty"`
}

type runDetailsCPUTopology struct {
	TotalCPUCount int                 `json:"TotalCpuCount"`
	CPUTypes      []runDetailsCPUType `json:"CpuTypes"`
}

type runDetailsCPUType struct {
	Name       string   `json:"Name,omitempty"`
	Midr       string   `json:"Midr,omitempty"`
	CoreCount  int      `json:"CoreCount"`
	ClusterIDs []uint32 `json:"ClusterIds"`
}
