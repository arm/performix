// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package android

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// `$ dumpsys package r` lists all system resolvers and
// uses the following format ('-' replaced with spaces):
//
// * <Resolver Type>
// * ----<Intent filter>:
// * --------pkg1_hash--pkg1_name/activity_name
// * --------pkg2_hash--pkg2_name/activity_name
// * ...
// * ----<Intent filter>:
// * --------another_pkg_hash--another_pkg_name/activity_name
// * ...
//
// To get MAIN activities we need to find all the packages listed
// under `android.intent.action.MAIN:` intent filter.
//
// Note: there might be more than one `android.intent.action.MAIN:` filter.
const mainActivitiesCommand = "dumpsys package r" + // dump all resolver tables
	" | sed -z 's/\\n        / /g'" + // remove \n from every lines containing \n followed by eight spaces (all packages are in one line)
	" | grep 'android.intent.action.MAIN:'" + // grep only the lines which contain packages with MAIN intent action
	" | tr ' ' '\\n'" + // convert the result back into separate lines
	" | grep '/'" + // grep only the lines containing "pkg_name/activity_name"
	" | sort | uniq" // avoid duplicate activities

// The result of running `cmd` should be packages with MAIN activities:
// * com.example.package1/main.activity.name1
// * com.example.package1/main.activity.name2
// * com.example.package2/another.activity.name
// * ...

// ListPackages lists third-party packages installed on an Android target and their MAIN activities.
func ListPackages(processManager process.ProcessManager) (*targetagentproto.AndroidPackageList, error) {
	// return packages only from third party apps (not system apps) using `pm list packages -3`
	result, err := processManager.ExecCommand(&process.LaunchCommand{Command: []string{"pm", "list", "packages", "-3"}})
	if err != nil {
		return nil, message.New(message.AgentGrpcserverApiTargetAgentListAndroidPackages).WithCause(err)
	}
	if result.Rc != 0 {
		return nil, message.New(message.AgentGrpcserverApiTargetAgentListAndroidPackages).
			WithCause(fmt.Errorf("pm list packages -3 exited with code %d: %s", result.Rc, strings.TrimSpace(result.Stderr)))
	}

	packageNames := parsePackageList(result.Stdout)
	resolverResult, err := processManager.ExecCommand(&process.LaunchCommand{Command: []string{"sh", "-c", mainActivitiesCommand}})
	if err != nil {
		return nil, message.New(message.AgentGrpcserverApiTargetAgentListAndroidPackageActivities).WithCause(err)
	}
	if resolverResult.Rc != 0 {
		return nil, message.New(message.AgentGrpcserverApiTargetAgentListAndroidPackageActivities).
			WithCause(fmt.Errorf("package activity query exited with code %d: %s", resolverResult.Rc, strings.TrimSpace(resolverResult.Stderr)))
	}

	mainActivitiesByPackage := parseMainActivityComponentsByPackage(resolverResult.Stdout)
	packages := make([]*targetagentproto.AndroidPackage, 0, len(packageNames))
	for _, packageName := range packageNames {
		activities := mainActivitiesByPackage[packageName]
		if len(activities) == 0 {
			continue
		}
		packages = append(packages, &targetagentproto.AndroidPackage{
			Name:       packageName,
			Activities: activitiesToProto(activities),
		})
	}

	return &targetagentproto.AndroidPackageList{Packages: packages}, nil
}

func parsePackageList(output string) []string {
	seen := map[string]struct{}{}
	var packages []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		packageName := strings.TrimSpace(strings.TrimPrefix(line, "package:"))
		if idx := strings.LastIndex(packageName, "="); idx >= 0 {
			packageName = packageName[idx+1:]
		}
		if packageName == "" {
			continue
		}
		if _, ok := seen[packageName]; ok {
			continue
		}
		seen[packageName] = struct{}{}
		packages = append(packages, packageName)
	}
	sort.Strings(packages)
	return packages
}

func parseMainActivityComponentsByPackage(output string) map[string][]string {
	seen := map[string]map[string]struct{}{}
	activitiesByPackage := map[string][]string{}

	for _, line := range strings.Split(output, "\n") {
		packageName, activity, ok := parseComponentName(line)
		if !ok {
			continue
		}
		if seen[packageName] == nil {
			seen[packageName] = map[string]struct{}{}
		}
		if _, ok := seen[packageName][activity]; ok {
			continue
		}
		seen[packageName][activity] = struct{}{}
		activitiesByPackage[packageName] = append(activitiesByPackage[packageName], activity)
	}

	for packageName := range activitiesByPackage {
		sort.Strings(activitiesByPackage[packageName])
	}
	return activitiesByPackage
}

func parseComponentName(component string) (string, string, bool) {
	packageName, activity, ok := strings.Cut(strings.TrimSpace(component), "/")
	if !ok || packageName == "" || activity == "" {
		return "", "", false
	}
	if strings.HasPrefix(activity, ".") {
		activity = packageName + activity
	}
	return packageName, activity, true
}

func activitiesToProto(activities []string) []*targetagentproto.AndroidActivity {
	protoActivities := make([]*targetagentproto.AndroidActivity, 0, len(activities))
	for _, activity := range activities {
		protoActivities = append(protoActivities, &targetagentproto.AndroidActivity{Name: activity})
	}
	return protoActivities
}
