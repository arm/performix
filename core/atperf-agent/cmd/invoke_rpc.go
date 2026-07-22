// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/atperf-agent/rpcclient"
)

// NewInvokeRpcCmd invokes a gRPC method and prints the JSON response
func NewInvokeRpcCmd(clientSupplier rpcclient.ClientSupplier) *cobra.Command {
	var port int
	var request string
	cmd := &cobra.Command{
		Use:   "invoke-rpc",
		Short: "Invoke a gRPC call and output the JSON response",
		RunE: func(cmd *cobra.Command, args []string) error {
			return InvokeRPC(request, port, clientSupplier, cmd.OutOrStdout())
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 50051, "gRPC server port")
	cmd.Flags().StringVar(&request, "request", "", "RPC request in the form MethodName={json}")
	_ = cmd.MarkFlagRequired("request")
	return cmd
}

// parseRequest splits "MethodName={json}" into method name and raw JSON args
func parseRequest(request string) (method string, args json.RawMessage, err error) {
	parts := strings.SplitN(request, "=", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid request format, expected MethodName={json}")
	}
	return parts[0], json.RawMessage(parts[1]), nil
}

func InvokeRPC(request string, port int, clientSupplier rpcclient.ClientSupplier, out io.Writer) error {
	// parse the request string
	method, argsRaw, err := parseRequest(request)
	if err != nil {
		return err
	}
	registry := rpcclient.NewRegistry()
	rpcHandler, err := registry.NewRPCHandler(method)
	if err != nil {
		return err
	}

	output, err := rpcclient.InvokeRPC(rpcHandler, port, clientSupplier, argsRaw)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, output)
	return nil
}
