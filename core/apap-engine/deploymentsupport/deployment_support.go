// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package deploymentsupport

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
)

// PlatformConfigurationFilter defines a filter for platform configurations.
// Filters are used to determine if a deployment declaration applies to a given target platform configuration.
type PlatformConfigurationFilter struct {
	Architecture  *conductor.Architecture
	OS            *conductor.OS
	KernelVersion *string           // todo
	Labels        map[string]string // todo
}

// MatchesPlatform checks if the filter applies to the given platform configuration
func (filter PlatformConfigurationFilter) MatchesPlatform(platformConfig conductor.PlatformConfiguration, flags MatchFlags) bool {
	if flags == 0 {
		flags = MatchAll
	}
	if flags&MatchArch != 0 {
		if filter.Architecture != nil && *filter.Architecture != platformConfig.Architecture {
			return false
		}
	}
	if flags&MatchOS != 0 {
		if filter.OS != nil && *filter.OS != platformConfig.OS {
			return false
		}
	}
	return true
}

type MatchFlags int

const (
	MatchOS MatchFlags = 1 << iota
	MatchArch
	MatchKernelVersion
	MatchLabels
	MatchAll = (1 << iota) - 1
)

// DeploymentDeclaration defines a deployment for a recipe or tool.
type DeploymentDeclaration struct {
	AppliesTo    []PlatformConfigurationFilter
	Dependencies []Dependency
}

// Dependency defines a dependency on a tool or tool bundle required by a deployment
// Dependencies are used in both recipes and tool integrations.
type Dependency struct {
	Type         DependencyType
	Name         string
	Version      string
	Locality     DeploymentLocality
	RequiredWhen RequirementSpec
}

// ToolBundleInfo defines the tool bundle name and version for a dependency, used as part of --deploy-tools
// These are different to the tool name and version, which are used to identify the tool integration
// On the long term, we should probably retrieve the tool bundle name and version from the tool integration metadata
type ToolBundleInfo struct {
	Name     string
	Version  string
	Locality DeploymentLocality
}

// ContainsHostToolBundles reports whether any resolved tool bundle is marked for host deployment.
func ContainsHostToolBundles(bundles []ToolBundleInfo) bool {
	for _, bundle := range bundles {
		if bundle.Locality == DeploymentLocalityHost {
			return true
		}
	}
	return false
}

// maxToolDepth defines the maximum depth of tool dependencies we will resolve
// Currently only recipes depending on tools (depth 1) are supported (recipe -> tool)
// Tools depending on other tools (depth 2) is NOT supported (tool -> tool)
const maxToolDepth = 1

type DependencyType string

const (
	DependencyTypeTool       DependencyType = "tool"
	DependencyTypeToolBundle DependencyType = "tool_bundle"
)

type DeploymentLocality string

const (
	DeploymentLocalityTarget DeploymentLocality = "target"
	DeploymentLocalityHost   DeploymentLocality = "host"
)

type RequirementType string

const (
	RequirementTypeAlways          RequirementType = "always"
	RequirementTypeIfParamIsSet    RequirementType = "param_is_set"
	RequirementTypeIfParamIsNotSet RequirementType = "param_is_not_set"
)

// Negate returns the negation of the requirement type, if applicable.
// Ideally we may want to resolve a negated condition into a positive one, especially
// if we have bool parameters or types with 2 options. But this requires deeper understanding
// of the parameter types, so for now stick with simple negations.
func (r RequirementType) Negate() RequirementType {
	switch r {
	case RequirementTypeIfParamIsSet:
		return RequirementTypeIfParamIsNotSet
	case RequirementTypeIfParamIsNotSet:
		return RequirementTypeIfParamIsSet
	default:
		return r
	}
}

// RequirementSpec defines a requirement specification for a dependency.
// It specifies when a dependency is required based on certain conditions.
// For example, a dependency might be required only if a certain parameter equals a specific value.
type RequirementSpec struct {
	Type       RequirementType        `json:"type" yaml:"type"`
	Parameters map[string]interface{} `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// PlatformSupport defines whether a platform is supported, not supported, or conditionally supported.
type PlatformSupport struct {
	Platform      conductor.PlatformConfiguration
	Result        PlatformSupportResult
	ConditionList []RequirementSpec // used only if Result is PlatformSupportConditional
}

type PlatformSupportResult int32

const (
	PlatformIsSupported PlatformSupportResult = iota
	PlatformNotSupported
	PlatformSupportConditional
)

// appliesToPlatform returns true if ANY filter matches the platform.
// If no filters are provided, treat it as "applies everywhere".
func appliesToPlatform(filters []PlatformConfigurationFilter, platformConfig conductor.PlatformConfiguration, flags MatchFlags) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if f.MatchesPlatform(platformConfig, flags) {
			return true
		}
	}
	return false
}

// CheckParameterRequirement checks if the provided parameters meet the conditions specified in the RequirementSpec.
// It returns true if the conditions are met, false otherwise.
// An error is returned if the RequirementSpec is invalid.
func CheckParameterRequirement(ctx context.Context, spec RequirementSpec, params parameters.BoundParameters) (bool, error) {
	var requireParamToBeSet bool
	switch spec.Type {
	case RequirementTypeIfParamIsSet:
		requireParamToBeSet = true
	case RequirementTypeIfParamIsNotSet:
		requireParamToBeSet = false
	default:
		return false, message.New(message.CommonUnknownError).WithCause(fmt.Errorf("invalid requirement type for parameter check: %q", spec.Type))
	}
	if len(spec.Parameters) == 0 {
		return false, message.New(message.CommonUnknownError).WithCause(fmt.Errorf("no parameters specified as conditions"))
	}
	for param, expectedValue := range spec.Parameters {
		val, exists := params.FindValue(param)
		if !exists {
			return false, nil
		}
		if val != expectedValue && requireParamToBeSet {
			return false, nil
		}
		if val == expectedValue && requireParamToBeSet {
			continue
		}
		if val == expectedValue && !requireParamToBeSet {
			return false, nil
		}
		if val != expectedValue && !requireParamToBeSet {
			continue
		}
	}
	return true, nil
}

// ResolveParameterSupport checks if a conditionally supported platform is supported based on the provided parameters.
// If the platform support is not conditional, it is returned as-is.
func ResolveParameterSupport(ctx context.Context, ps PlatformSupport, params parameters.BoundParameters) (PlatformSupport, error) {
	if ps.Result != PlatformSupportConditional {
		return ps, nil
	}
	for _, cond := range ps.ConditionList {
		met, err := CheckParameterRequirement(ctx, cond, params)
		if err != nil {
			return PlatformSupport{Result: PlatformNotSupported}, err
		}
		if !met {
			return PlatformSupport{Result: PlatformNotSupported}, nil
		}
	}
	// All conditions are met, simply return supported
	return PlatformSupport{Result: PlatformIsSupported}, nil
}

// normalizeDeploymentLocality checks a deployment locality is valid and defaults to target for empty localities.
func normalizeDeploymentLocality(locality DeploymentLocality) (DeploymentLocality, error) {
	switch locality {
	case "":
		return DeploymentLocalityTarget, nil
	case DeploymentLocalityTarget, DeploymentLocalityHost:
		return locality, nil
	default:
		return "", message.New(message.EngineDeploymentsupportInvalidLocality).
			WithMetadata(map[string]string{"locality": string(locality)})
	}
}

// DependencyResolver is a function that returns the deployments of the dependency.
// Currently only tool dependencies are supported.
type DependencyResolver func(string, string) ([]DeploymentDeclaration, error)

// getDeploymentSupportForPlatformInternal is an internal function to support recursion with depth tracking argument
func getDeploymentSupportForPlatformInternal(depResolver DependencyResolver, flags MatchFlags, deployments []DeploymentDeclaration, platformConfig conductor.PlatformConfiguration, depth int) (PlatformSupport, error) {
	deploymentSupported := true
	var condList []RequirementSpec // At least one condition has to be met.

	// Look through each deployment. Currently deployments act as fallbacks; the first compatible one wins.
	// TODO: If we ever need to surface multiple supported deployments to callers this code here needs to be changed.
	for _, decl := range deployments {
		condList = []RequirementSpec{} // Clear the condition list for each deployment
		if !appliesToPlatform(decl.AppliesTo, platformConfig, flags) {
			// If the deployment doesn't apply to the platform, skip it, don't bother
			// looking at the dependencies
			deploymentSupported = false
			continue
		}
		deploymentSupported = true

		// Now focus on the dependencies of this deployment.
		// If any dependency has a conditional requirement, mark as conditional.
		for _, dep := range decl.Dependencies {
			conditionallyRequired := (dep.RequiredWhen.Type != RequirementTypeAlways)
			dependencySupported := true

			switch dep.Type {
			case DependencyTypeTool:
				if dep.Locality != "" {
					return PlatformSupport{Platform: platformConfig, Result: PlatformNotSupported},
						message.New(message.EngineDeploymentsupportLocalityNotAllowedForToolDependency)
				}

				if depth >= maxToolDepth {
					// Limit the levels of recursive dependency checking
					// For now we only check the platform filters for recipes (depth 0) and tools (depth 1)
					// Later we may want to support tools depending on other tools
					return PlatformSupport{Platform: platformConfig, Result: PlatformNotSupported},
						message.New(message.EngineDeploymentsupportDependencyNotAllowed)
				}

				toolDeployments, err := depResolver(dep.Name, dep.Version)
				if err != nil {
					return PlatformSupport{Platform: platformConfig, Result: PlatformNotSupported}, err
				}

				// Recursively check the tool's deployments
				depSupport, err := getDeploymentSupportForPlatformInternal(depResolver, flags, toolDeployments, platformConfig, depth+1)
				if err != nil {
					return PlatformSupport{Platform: platformConfig, Result: PlatformNotSupported}, err
				}

				if depSupport.Result == PlatformSupportConditional {
					// For now we only allow conditionally support dependencies at the top level.
					// TODO - allow conditionally supported dependencies in tools. This will allow more complex chain of dependencies, i.e tools dependent on other tools.
					// This error is a safety catch, because we should never get here. We have previously restricted the platform support resolution to 2 levels: recipe -> tool.
					return PlatformSupport{Platform: platformConfig, Result: PlatformNotSupported},
						message.New(message.CommonUnknownError).WithCause(fmt.Errorf("conditional platform support in dependencies (tools) not supported"))
				}
				if depSupport.Result == PlatformNotSupported {
					dependencySupported = false
				}
			case DependencyTypeToolBundle:
				if dep.Name == "" || dep.Version == "" {
					dependencySupported = false
					break
				}

				if _, err := normalizeDeploymentLocality(dep.Locality); err != nil {
					return PlatformSupport{Platform: platformConfig, Result: PlatformNotSupported}, err
				}
			default:
				// Unknown dependency type, treat as unsupported
				dependencySupported = false
			}

			if !dependencySupported {
				if conditionallyRequired {
					// This dependency is not supported, but it's conditionally required. What this means is that our deployment
					// is conditionally supported, but the condition is the INVERSE of this dependency's condition.
					condList = append(condList, RequirementSpec{Type: dep.RequiredWhen.Type.Negate(), Parameters: dep.RequiredWhen.Parameters})
					continue
				}
				// Dependency is not supported, and it's required unconditionally, so the deployment is not supported
				deploymentSupported = false
				break
			}
		}

		// Finished checking all dependencies
		if deploymentSupported {
			if len(condList) == 0 {
				// Deployment is supported unconditionally, no need to check any more deployments
				return PlatformSupport{Platform: platformConfig, Result: PlatformIsSupported}, nil
			}
			break
		}
	}

	if !deploymentSupported {
		return PlatformSupport{Platform: platformConfig, Result: PlatformNotSupported}, nil
	}
	if len(condList) > 0 {
		return PlatformSupport{Platform: platformConfig, Result: PlatformSupportConditional, ConditionList: dedupConditions(condList)}, nil
	}

	return PlatformSupport{Platform: platformConfig, Result: PlatformIsSupported}, nil
}

// GetDeploymentSupportForPlatform goes through all deployments and reports whether the platform
// is supported, unsupported, or conditionally supported
// Currently dependencies can be tools or tool bundles only (thus we pass tool integrations into the function)
func GetDeploymentSupportForPlatform(depResolver DependencyResolver, flags MatchFlags, deployments []DeploymentDeclaration, platformConfig conductor.PlatformConfiguration) (PlatformSupport, error) {
	return getDeploymentSupportForPlatformInternal(depResolver, flags, deployments, platformConfig, 0)
}

// dedupConditions returns a new slice with duplicate RequirementSpec entries removed.
// Duplicates are detected by (Type, Parameters) string form.
func dedupConditions(in []RequirementSpec) []RequirementSpec {
	seen := make(map[string]struct{}, len(in))
	out := make([]RequirementSpec, 0, len(in))
	key := func(rs RequirementSpec) string {
		return fmt.Sprintf("%s|%v", rs.Type, rs.Parameters)
	}
	for _, rs := range in {
		k := key(rs)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, rs)
	}
	return out
}

// ResolveToolBundles resolves recipe deployments into the set of tool bundles that must be deployed
// for a given platform and set of parameters. It walks tool dependencies recursively (recipe -> tool)
// and deduplicates tool bundles by name/version/locality.
func ResolveToolBundles(ctx context.Context,
	platformConfig conductor.PlatformConfiguration,
	paramValues *parameters.BoundParameters,
	deployments []DeploymentDeclaration,
	depResolver DependencyResolver) ([]ToolBundleInfo, error) {

	// bundleMap keeps the deduplicated set of bundles we will return.
	// TODO: if we support deeper tool dependency chains, consider tracking visited tools to avoid cycles.
	bundleMap := make(map[string]ToolBundleInfo)

	err := resolveToolBundles(
		ctx, platformConfig, paramValues, deployments, depResolver,
		bundleMap, 0)
	if err != nil {
		return nil, err
	}

	// Convert to list
	result := make([]ToolBundleInfo, 0, len(bundleMap))
	for _, bundle := range bundleMap {
		result = append(result, bundle)
	}
	return result, nil
}

// resolveToolBundles is a recursive helper function for ResolveToolBundles that walks deployments and their dependencies
// with depth tracking. Currently only recipe -> tool dependencies are supported (depth 1).
func resolveToolBundles(ctx context.Context,
	platformConfig conductor.PlatformConfiguration,
	paramValues *parameters.BoundParameters,
	deployments []DeploymentDeclaration,
	depResolver DependencyResolver,
	bundleMap map[string]ToolBundleInfo,
	depth int) error {

	// Iterate over deployments
	for idx, decl := range deployments {
		// Filter by platform
		if !appliesToPlatform(decl.AppliesTo, platformConfig, MatchAll) {
			logx.FromContext(ctx).
				WithField("deploymentIndex", idx).
				WithField("targetArchitecture", platformConfig.Architecture).
				WithField("targetOS", platformConfig.OS).
				Warn("deployment skipped for target platform")
			continue
		}

		// Each deployment can have multiple dependencies
		for depIndex, dep := range decl.Dependencies {
			switch dep.Type {
			case DependencyTypeToolBundle:
				// Filter empty tool bundles
				if dep.Name == "" || dep.Version == "" {
					return message.New(message.EngineDeploymentsupportToolBundleMissingFields)
				}

				locality, err := normalizeDeploymentLocality(dep.Locality)
				if err != nil {
					return err
				}

				shouldDeploy, err := shouldDeployDependency(ctx, dep, paramValues)
				if err != nil {
					return err
				}

				if shouldDeploy {
					bundleMap[bundleKey(dep.Name, dep.Version, locality)] = ToolBundleInfo{
						Name:     dep.Name,
						Version:  dep.Version,
						Locality: locality,
					}
				}
			case DependencyTypeTool:
				if dep.Locality != "" {
					return message.New(message.EngineDeploymentsupportLocalityNotAllowedForToolDependency)
				}

				shouldDeploy, err := shouldDeployDependency(ctx, dep, paramValues)
				if err != nil {
					return err
				}
				if !shouldDeploy {
					continue
				}

				// Tool -> tool dependency (depth 2) -- not supported
				// Something to come back in the future?
				if depth >= maxToolDepth {
					return message.New(message.EngineDeploymentsupportDependencyNotAllowed)
				}

				// Recipe -> tool dependency (depth 1) -- recurse
				toolDeployments, err := depResolver(dep.Name, dep.Version)
				if err != nil {
					return err
				}

				err = resolveToolBundles(
					ctx, platformConfig, paramValues, toolDeployments, depResolver,
					bundleMap, depth+1)
				if err != nil {
					return err
				}
			default:
				logx.FromContext(ctx).
					WithField("deploymentIndex", idx).
					WithField("dependencyIndex", depIndex).
					WithField("dependencyType", dep.Type).
					Warn("unsupported dependency type skipped")
			}
		}
	}
	return nil
}

func bundleKey(name, version string, locality DeploymentLocality) string {
	return name + "@" + version + "@" + string(locality)
}

// shouldDeployDependency checks if a dependency ultimately should be deployed based on its requirement spec.
// Currently it only checks for 'always' and parameter-based requirements.
func shouldDeployDependency(ctx context.Context, dep Dependency, paramValues *parameters.BoundParameters) (bool, error) {
	switch dep.RequiredWhen.Type {
	case RequirementTypeAlways:
		return true, nil
	case RequirementTypeIfParamIsSet, RequirementTypeIfParamIsNotSet:
		if paramValues == nil {
			return dep.RequiredWhen.Type == RequirementTypeIfParamIsNotSet, nil
		}
		required, err := CheckParameterRequirement(ctx, dep.RequiredWhen, *paramValues)
		if err != nil {
			return false, err
		}
		return required, nil
	default:
		return false, message.New(message.CommonUnknownError).
			WithCause(fmt.Errorf("invalid requirement type for parameter check: %q", dep.RequiredWhen.Type))
	}
}
