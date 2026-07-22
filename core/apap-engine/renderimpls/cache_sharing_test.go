// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func TestCacheSharingConfigureDefaultsAndOverrides(t *testing.T) {
	renderer := &CacheSharingRenderer{}
	err := renderer.Configure(&render.Config{JSON: `{}`})
	require.NoError(t, err)
	assert.Equal(t, defaultPerfEntity, renderer.getEntity())
	assert.Equal(t, defaultComponent, renderer.getComponent())

	renderer = &CacheSharingRenderer{}
	err = renderer.Configure(&render.Config{JSON: `{"entity":"custom/entity/","component":"custom-component"}`})
	require.NoError(t, err)
	assert.Equal(t, "custom/entity/", renderer.getEntity())
	assert.Equal(t, "custom-component", renderer.getComponent())
}

func TestCacheSharingInitializeMissingSymbols(t *testing.T) {
	renderer := &CacheSharingRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))

	session := render.MockSession{}

	err := renderer.Initialize(&session, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required input 'symbols'")
}

func TestCacheSharingInitializeMissingImages(t *testing.T) {
	renderer := &CacheSharingRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))

	session := render.MockSession{}
	session.On("Content").Return(&render.ContentMap{Entries: []render.ContentMapEntry{{}}})

	resolved := render.TableRefMap{
		"symbols": {{Name: "symbols"}}, // matches 1 entry
		// images missing
		"source_files": {{Name: "source_files"}}, // matches 1 entry
	}

	err := renderer.Initialize(&session, resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required input 'images'")
}

func TestCacheSharingInitializeMissingSourceFiles(t *testing.T) {
	renderer := &CacheSharingRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))

	session := render.MockSession{}
	session.On("Content").Return(&render.ContentMap{Entries: []render.ContentMapEntry{{}}})

	resolved := render.TableRefMap{
		"symbols": {{Name: "symbols"}}, // matches 1 entry
		"images":  {{Name: "images"}},  // matches 1 entry
		// source_files missing
	}

	err := renderer.Initialize(&session, resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required input 'source_files'")
}
