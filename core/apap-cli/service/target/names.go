// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"fmt"
	"math/rand"
	"strconv"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

// generateTargetName returns a friendly "<Colour><BeeSpecies>" name.
func generateTargetName() string {
	colours := []string{"Red", "Blue", "Green", "Yellow", "Purple", "Orange", "Cyan", "Magenta"}
	beeSpecies := []string{"Honey", "Bumble", "Carpenter", "Leafcutter", "Sweat", "Mason", "Mining", "Orchid"}

	randomColour := colours[rand.Intn(len(colours))]    // #nosec G404 -- friendly name, not security sensitive
	randomBee := beeSpecies[rand.Intn(len(beeSpecies))] // #nosec G404 -- friendly name, not security sensitive

	return fmt.Sprintf("%s%s", randomColour, randomBee)
}

// GenerateUniqueTargetName generates a friendly target name that does not collide with an
// existing target in the configuration, retrying with a numeric suffix on collisions.
func GenerateUniqueTargetName(targetService TargetManagerService) (string, error) {
	config, err := targetService.ReadTargetConfig()
	if err != nil {
		return "", err
	}

	targetName := generateTargetName()
	baseName := targetName

	// This should not be 1, otherwise the error message will be incorrectly phrased ("attempted to generate a target name 1 times")
	const maxRetries = 10
	for i := 0; i < maxRetries; i++ {
		if _, exists := config.Targets[targetName]; !exists {
			return targetName, nil
		}
		targetName = fmt.Sprintf("%s%d", baseName, rand.Intn(999)) // #nosec G404 -- friendly name, not security sensitive
	}

	return "", message.New(message.CliCmdTargetAddGenerateNameFailed).WithMetadata(map[string]string{"numAttempts": strconv.Itoa(maxRetries)})
}
