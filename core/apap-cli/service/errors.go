// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"fmt"
	"strings"

	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/clients/go/errorsproto"
)

type GRPCErrors struct{}

// ExtractGRPCError returns the components of a gRPC error
func (ge GRPCErrors) ExtractGRPCError(err error) (string, string, string, bool) {
	st, gRPCError := status.FromError(err)

	var gRPCDetails string
	var gRPCCode string
	var gRPCMessage string

	if gRPCError {
		var detailsList []string
		for _, details := range st.Details() {
			detailsList = append(detailsList, details.(*errorsproto.ErrorMessage).Message)
		}

		gRPCDetails = fmt.Sprintf("- %s", strings.Join(detailsList, "\n- "))
		gRPCCode = fmt.Sprint(st.Code())
		gRPCMessage = st.Message()
	}

	return gRPCCode, gRPCMessage, gRPCDetails, gRPCError
}
