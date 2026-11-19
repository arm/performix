// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package producttelemetry

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type unexpectedTransport struct{ t *testing.T }

func (transport unexpectedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.t.Fatal("no-op sender attempted an HTTP request")
	return nil, nil
}

func TestNoopSenderDiscardsEventsWithoutNetworkAccess(t *testing.T) {
	// Given the fallback used in public builds and a transport that rejects network access.
	previous := http.DefaultTransport
	http.DefaultTransport = unexpectedTransport{t}
	t.Cleanup(func() { http.DefaultTransport = previous })
	send := newNoopSender()
	// When an event is submitted.
	err := send(context.Background(), []byte(`{"name":"test"}`))
	// Then it succeeds locally without accessing the network.
	require.NoError(t, err)
}

func TestPublicBuildUsesNoopSender(t *testing.T) {
	// Given a public-mirror build without the private Azure implementation.
	if instrumentationKey != "" {
		t.Skip("requires the filtered public source tree")
	}
	previous := http.DefaultTransport
	http.DefaultTransport = unexpectedTransport{t}
	t.Cleanup(func() { http.DefaultTransport = previous })
	// When the default sender receives an event.
	err := NewEnvelopeSender()(context.Background(), []byte(`{"name":"test"}`))
	// Then it discards the event without accessing the network.
	require.NoError(t, err)
}
