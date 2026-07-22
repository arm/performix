// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// Lister defines interfaces to list all runs
type Lister interface {
	ListRuns(client apapproto.ApapClient) (clijson.CLIRunListing, error)
}

type ListService struct{}

func (s ListService) ListRuns(client apapproto.ApapClient) (clijson.CLIRunListing, error) {
	listing, err := client.ListRuns(context.Background(), &emptypb.Empty{})
	if err != nil {
		return clijson.CLIRunListing{}, err
	}

	return clijson.CLIRunListingFromProto(listing)
}
