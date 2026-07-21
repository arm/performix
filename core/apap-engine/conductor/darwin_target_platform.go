// Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package conductor

// NewDarwinTargetPlatform reuses the Linux path utilities and actions implementation.
func NewDarwinTargetPlatform(cmdRunner CommandRunner, fs TargetFilesystem, arch Architecture) *TargetPlatform {
	return &TargetPlatform{
		PlatformConfiguration: PlatformConfiguration{Architecture: arch, OS: Darwin},
		Path:                  &LinuxPathUtils{},
		Actions:               &LinuxTargetActions{BaseTargetActions{CmdRunner: cmdRunner, FS: fs}},
	}
}
