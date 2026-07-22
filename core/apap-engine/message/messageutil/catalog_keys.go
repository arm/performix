// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package messageutil

import "strings"

// IsCatalogMetadataKey reports whether a catalog key is metadata rather than a message namespace.
func IsCatalogMetadataKey(key string) bool {
	return key == "metadata" || strings.HasPrefix(key, "_")
}
