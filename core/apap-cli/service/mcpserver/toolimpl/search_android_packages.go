// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// search_android_packages.go implements the search_android_packages MCP tool. It lists launchable
// Android packages and activities, optionally filters them, and caps the response size.

package toolimpl

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

type SearchAndroidPackagesTool struct{}

const maxAndroidPackageResults = 1000

type searchAndroidPackagesInput struct {
	Target   string `json:"target"`
	Contains string `json:"contains,omitempty"`
}

var searchAndroidPackagesInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"target"},
	Properties: map[string]*jsonschema.Schema{
		"target": {
			Type:        "string",
			Description: "Configured Android target name. If unknown, use list_targets to list configured targets. Use add_target to configure a new Android target.",
		},
		"contains": {
			Type:        "string",
			Description: "Return only packages whose package name or an activity name contains this value, ignoring case.",
		},
	},
}

type listedAndroidPackage struct {
	PackageName string   `json:"package_name"`
	Activities  []string `json:"activities"`
	Debuggable  bool     `json:"debuggable"`
}

type searchAndroidPackagesResult struct {
	Packages         []listedAndroidPackage `json:"packages"`
	TotalMatches     int                    `json:"total_matches"`
	ResultsTruncated bool                   `json:"results_truncated"`
	Error            *toolError             `json:"error,omitempty"`
}

var searchAndroidPackagesOutputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"packages", "total_matches", "results_truncated"},
	Properties: map[string]*jsonschema.Schema{
		"packages": {
			Type:        "array",
			Description: "Matching launchable packages, sorted by package name in ascending order.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"package_name", "activities", "debuggable"},
				Properties: map[string]*jsonschema.Schema{
					"package_name": {
						Type:        "string",
						Description: "Android package name.",
					},
					"activities": {
						Type:        "array",
						Description: "Launchable activity names, sorted in ascending order.",
						Items:       &jsonschema.Schema{Type: "string"},
					},
					"debuggable": {
						Type:        "boolean",
						Description: "True if the Android package is confirmed debuggable; false otherwise, including when its status could not be determined. Only packages with debuggable set to true can be passed to run_recipe.",
					},
				},
			},
		},
		"total_matches": {
			Type:        "integer",
			Minimum:     jsonschema.Ptr(0.0),
			Description: "Total number of packages matching the supplied substring.",
		},
		"results_truncated": {
			Type:        "boolean",
			Description: "Whether matching packages were omitted. Refine contains when true.",
		},
		"error": toolErrorSchema(),
	},
}

func (SearchAndroidPackagesTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_android_packages",
		Description: "Searches launchable packages and activities on a configured Android target, deploying mandatory tools if needed. " +
			fmt.Sprintf("Optionally filter by a case-insensitive substring matched against package and activity names. The filter is applied before results are limited to the first %d packages. ", maxAndroidPackageResults) +
			"Only packages with debuggable set to true can be used as Android launch workloads. Pass a debuggable package's package_name and one of its activities to run_recipe using android_launch.package_name and android_launch.activity_name. " +
			"If the requested package has debuggable set to false, explain that the user must install a debuggable build before profiling it. " +
			"Use list_targets to find an Android target name. " +
			"If results_truncated is true, refine contains. Call again only to refine the search, after an error or to refresh the results.",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: jsonschema.Ptr(false),
		},
		InputSchema:  searchAndroidPackagesInputSchema,
		OutputSchema: searchAndroidPackagesOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input searchAndroidPackagesInput) (*mcp.CallToolResult, searchAndroidPackagesResult, error) {
		tgt, err := toolDeps.Targets.GetTarget(input.Target)
		if err != nil {
			return searchAndroidPackagesError(err)
		}
		if _, ok := tgt.(*engine_target.AndroidTarget); !ok {
			err := message.New(message.EngineGrpcserverApiApapListAndroidPackagesTargetNotAndroid).
				WithMetadata(map[string]string{"targetType": fmt.Sprintf("%T", tgt)})
			return searchAndroidPackagesError(err)
		}
		protoTarget := grpcserver.TargetToProto(tgt)

		prepareResponse, err := toolDeps.Engine.TargetPrepare(ctx, &apapproto.TargetPrepareRequest{
			Target:         protoTarget,
			DeploymentType: apapproto.ToolDeploy_AUTO,
		})
		if err != nil {
			return searchAndroidPackagesError(err)
		}
		switch prepareResponse.GetResult() {
		case apapproto.TargetPrepareResult_DEPLOYED, apapproto.TargetPrepareResult_NO_ACTION:
		case apapproto.TargetPrepareResult_DEPLOY:
			return searchAndroidPackagesError(errors.New("the package search cannot continue because automatic target preparation failed to deploy all mandatory tools"))
		default:
			return searchAndroidPackagesError(fmt.Errorf("target preparation returned an unexpected result: %s", prepareResponse.GetResult()))
		}

		response, err := toolDeps.Engine.ListAndroidPackages(ctx, &apapproto.ListAndroidPackagesRequest{
			Target: protoTarget,
		})
		if err != nil {
			return searchAndroidPackagesError(err)
		}

		return nil, searchAndroidPackagesResultFromProto(response, input), nil
	})
}

func searchAndroidPackagesResultFromProto(response *apapproto.ListAndroidPackagesResponse, input searchAndroidPackagesInput) searchAndroidPackagesResult {
	packages := make([]listedAndroidPackage, 0)
	contains := strings.ToLower(input.Contains)
	for _, pkg := range response.GetPackages() {
		matches := contains == "" || strings.Contains(strings.ToLower(pkg.GetName()), contains)
		activities := make([]string, 0, len(pkg.GetActivities()))
		for _, activity := range pkg.GetActivities() {
			activityName := activity.GetName()
			activities = append(activities, activityName)
			matches = matches || strings.Contains(strings.ToLower(activityName), contains)
		}
		if !matches {
			continue
		}

		slices.Sort(activities)
		packages = append(packages, listedAndroidPackage{
			PackageName: pkg.GetName(),
			Activities:  activities,
			Debuggable:  pkg.GetDebuggable(),
		})
	}

	slices.SortFunc(packages, func(packageA, packageB listedAndroidPackage) int {
		return strings.Compare(packageA.PackageName, packageB.PackageName)
	})
	end := min(len(packages), maxAndroidPackageResults)

	return searchAndroidPackagesResult{
		Packages:         packages[:end],
		TotalMatches:     len(packages),
		ResultsTruncated: len(packages) > end,
	}
}

func searchAndroidPackagesError(err error) (*mcp.CallToolResult, searchAndroidPackagesResult, error) {
	return &mcp.CallToolResult{IsError: true}, searchAndroidPackagesResult{
		Packages: []listedAndroidPackage{},
		Error:    newToolError(err),
	}, nil
}
