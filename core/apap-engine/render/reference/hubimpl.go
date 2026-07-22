// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package reference

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

type hub struct {
	db   *render.Database
	meas render.MeasurementsService
}

func NewHub(db *render.Database, manifest *render.Manifest) (render.Hub, error) {
	meas, err := newMeasurementsService(db)
	if err != nil {
		return nil, err
	}
	meas.EnsureManifest(manifest)

	return &hub{
		db:   db,
		meas: meas,
	}, nil
}

func (h *hub) Measurements() render.MeasurementsService { return h.meas }

func (h *hub) Close() error {
	return h.meas.Close()
}
