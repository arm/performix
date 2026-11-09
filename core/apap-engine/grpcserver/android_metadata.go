// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

// ResolvedADBAddress is an optional response trailer on TargetTest and TargetPrepare.
// It contains the selector verified against the requested Android physical serial,
// even if a later step fails. Binary metadata preserves the selector verbatim.
const ResolvedADBAddress = "apx-resolved-adb-address-bin"

func withAndroidAddressTrailer(ctx context.Context, tgt target.Target) context.Context {
	android, ok := tgt.(*target.AndroidTarget)
	if !ok {
		return ctx
	}
	return conductor.WithADBDeviceObserver(ctx, func(device conductor.ADBDevice) {
		if device.SerialNumber != android.SerialNumber {
			return
		}
		if err := grpc.SetTrailer(ctx, metadata.Pairs(ResolvedADBAddress, device.Address)); err != nil {
			log.WithError(err).Warn("Could not return resolved Android address")
		}
	})
}
