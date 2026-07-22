// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import "github.com/Arm-Debug/apap-cli/apap-engine/targetsession"

type RendererList = []Renderer

type Row = map[string]interface{}
type Table = []Row // fake table data to use until we have a real DB interface exposed on top of duckdb

// Session is the interface that represents a single render session. A render session is started on a particular set of
// runs (the Content). The renderers in the session produce a list of queryable items, which are listed in
// the Manifest. These items are queryable via the Database.
type Session interface {
	ID() string
	DatabaseKey() string
	Close()
	Content() *ContentMap
	Manifest() *Manifest
	Database() *Database
	WidgetDataSources() *WidgetDataSources
	Reference() Hub
	Rerender() SessionRenderFS
	TargetSessions() targetsession.TargetSessionProvider
}

// SessionFactory defines an interface for things that can construct instances of Session
type SessionFactory interface {
	NewSession(content *ContentMap, renderers RendererList, dbFactory DatabaseFactory, rerender SessionRenderFS, targetSessions targetsession.TargetSessionProvider) (Session, error)
}
