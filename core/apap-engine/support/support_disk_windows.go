// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package support

import "context"

// resolveHostOpts returns the host-specific options for support package collection on Windows operating systems.
func resolveHostOpts() hostOpts {
	return hostOpts{
		diskUsage: diskUsageWindows,
	}
}

// diskUsageWindows returns the disk usage for Windows operating systems.
func diskUsageWindows(ctx context.Context) ([]byte, error) {
	psCommand := "Get-PSDrive -PSProvider FileSystem | Select-Object Name,@{Name='Used(GB)';Expression={[math]::Round($_.Used/1GB,2)}},@{Name='Free(GB)';Expression={[math]::Round($_.Free/1GB,2)}},@{Name='Total(GB)';Expression={[math]::Round((($_.Used+$_.Free)/1GB),2)}} | ConvertTo-Csv -NoTypeInformation"
	if output, err := defaultCommandRunner(ctx, "powershell", "-NoProfile", "-Command", psCommand); err == nil {
		return output, nil
	}
	return defaultCommandRunner(ctx, "wmic", "logicaldisk", "get", "Caption,FreeSpace,Size,VolumeName")
}
