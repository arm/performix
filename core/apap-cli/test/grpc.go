// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"github.com/spf13/viper"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/Arm-Debug/apap-cli/clients/go/errorsproto"
)

func NewTestGRPCError() error {
	detail1 := anypb.Any{}
	err := detail1.MarshalFrom(&errorsproto.ErrorMessage{Message: "detail 1"})
	if err != nil {
		panic(err)
	}

	detail2 := anypb.Any{}
	err = detail2.MarshalFrom(&errorsproto.ErrorMessage{Message: "detail 2"})
	if err != nil {
		panic(err)
	}

	errorStatus := spb.Status{
		Code:    int32(codes.Canceled),
		Message: "Bad things have happened",
		Details: []*anypb.Any{&detail1, &detail2},
	}
	return status.ErrorProto(&errorStatus)
}

// SetViperJSON is a shared test helper for temporarily toggling the `json` config flag and adds a cleanup method to
// ensure stable initial state between tests.
func SetViperJSON(t *testing.T, val bool) {
	original := viper.GetBool("json")
	viper.Set("json", val)
	t.Cleanup(func() { viper.Set("json", original) })
}
