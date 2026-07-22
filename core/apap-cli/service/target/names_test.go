// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"testing"

	"github.com/stretchr/testify/assert"

	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

func TestGenerateUniqueTargetName(t *testing.T) {
	t.Run("random name collisions do not cause errors", func(t *testing.T) {
		generatedNames := make(map[string]engine_target.Target)
		mtm := MockTargetManager{}
		mtm.On("ReadTargetConfig").Return(&engine_target.TargetConfig{Default: "default", Targets: generatedNames}, nil)

		for i := 0; i < 100; i++ {
			name, err := GenerateUniqueTargetName(&mtm)
			assert.NoError(t, err)
			generatedNames[name] = &engine_target.SSHTarget{}
		}
	})
}
