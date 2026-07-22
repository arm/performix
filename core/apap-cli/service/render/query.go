// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"io"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type QueryService interface {
	ExecuteQuery(client apapproto.ApapClient, sessionID string, format apapproto.TableFormat, queryStr string) ([]*apapproto.QueryResponse, error)
}

type QueryProcessor struct{}

func (s *QueryProcessor) ExecuteQuery(
	client apapproto.ApapClient,
	sessionID string,
	format apapproto.TableFormat,
	queryStr string,
) ([]*apapproto.QueryResponse, error) {
	request := apapproto.QueryRequest{
		SessionId:   sessionID,
		QuerySql:    queryStr,
		TableFormat: format,
	}

	query, err := client.Query(context.Background(), &request)
	if err != nil {
		return nil, err
	}

	var responses []*apapproto.QueryResponse
	for {
		message, recvErr := query.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
		responses = append(responses, message)
	}

	return responses, nil
}
