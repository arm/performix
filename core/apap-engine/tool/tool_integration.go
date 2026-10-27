// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
)

// Migration fields for the tool integration
type Migration struct {
	Type    string `json:"type,omitempty"`
	From    string `json:"from"`
	To      string `json:"to"`
	Version string `json:"version"`

	// optional path‐layout rewrite:
	OldSuffix string `json:"oldSuffix,omitempty"`
	NewSuffix string `json:"newSuffix,omitempty"`
}

// IntegrationProperties contains the static data of a tool integration
type IntegrationProperties struct {
	Name                   string
	Version                string
	Deployments            []deploymentsupport.DeploymentDeclaration
	SupportsWorkloadLaunch bool
	ShortDescription       string
	LongDescription        string
	Migrations             []Migration
}

// IntegrationContext contains the instance specific data for a tool integration execution
type IntegrationContext struct {
	InvocationIndex       int
	counterOutcomes       *counterOutcomeSet
	CapabilityRequest     *capabilities.CapabilityRequest
	Discovery             *ProbeResult
	Name                  string
	Version               string
	Params                map[string]any
	Workload              Workload
	WorkingDir            string
	Env                   map[string]string
	Ctx                   context.Context
	OutputEntityDir       string
	Timeout               uint32
	FeatureFlags          featureflags.Flags
	DefaultEngineLocality EngineLocality
	ResolveLocality       EngineLocalityResolver
}

type ProbeAdvice struct {
	Level       string            `json:"level"`
	MessageCode string            `json:"messageCode"`
	Metadata    map[string]string `json:"metadata"`
	Cause       string            `json:"cause"`
}

type ProbeResult struct {
	Available    bool           `json:"available"`
	Capabilities map[string]any `json:"capabilities"`
	Advice       []ProbeAdvice  `json:"advice"`
}

// ToolIntegration is the interface of a tool integration
type ToolIntegration interface {
	Properties() IntegrationProperties
	Probe() (ProbeResult, error)
	StartRuntime() (cleanup func(), err error)
	Run() error
	Stop() error
	Cancel() error
	Reformat() error
	// CollectionFinished is closed when collection has finished, or nil if the integration does not report it.
	CollectionFinished() <-chan struct{}
}

type WorkloadType int

const (
	WorkloadTypeLaunch WorkloadType = iota
	WorkloadTypeAttach
	WorkloadTypeSystemWide
	WorkloadTypeAndroidLaunch
)

type Workload interface {
	Type() WorkloadType
}

type WorkloadLaunch struct {
	// The raw workload string provided in the RPC call, before parsing. This should be
	// used by tools which expect a single workload arg
	RawCommand string
	// The parsed workload, stored as a slice of strings. This should be used by tools
	// which expect the workload as multiple args
	Command     []string
	Environment map[string]string
	WorkingDir  string
	UseShell    bool
}

type WorkloadAndroidLaunch struct {
	PackageName  string
	ActivityName string
}

type WorkloadAttach struct {
	PID int32
}
type WorkloadSystemWide struct{}

func (w *WorkloadLaunch) Type() WorkloadType     { return WorkloadTypeLaunch }
func (w *WorkloadAttach) Type() WorkloadType     { return WorkloadTypeAttach }
func (w *WorkloadSystemWide) Type() WorkloadType { return WorkloadTypeSystemWide }
func (w *WorkloadAndroidLaunch) Type() WorkloadType {
	return WorkloadTypeAndroidLaunch
}
