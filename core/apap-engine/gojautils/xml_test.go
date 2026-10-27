// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package gojautils

import (
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
	"github.com/stretchr/testify/assert"
)

func TestXMLModule(t *testing.T) {
	vm := goja.New()
	registry := require.NewRegistry()
	RegisterXML(registry)
	registry.Enable(vm)
	value, err := vm.RunString(`require('performix/xml').parse('<?xml version="1.0"?><events><!-- comment --><event name="a &amp; b"/></events>').children[0].attributes.name`)
	assert.NoError(t, err)
	assert.Equal(t, "a & b", value.String())
}

func TestXMLRejectsInvalidDocuments(t *testing.T) {
	for name, source := range map[string]string{
		"malformed": "<a>", "multiple_roots": "<a/><b/>", "duplicate_attributes": `<a id="1" id="2"/>`,
		"entity_definition": `<!DOCTYPE a [<!ENTITY x SYSTEM "file:///etc/passwd">]><a>&x;</a>`,
		"unknown_entity":    "<a>&missing;</a>", "outside_text": "ERROR: failed<a/>",
		"depth": strings.Repeat("<a>", 65) + strings.Repeat("</a>", 65), "size": strings.Repeat(" ", 8*1024*1024+1),
	} {
		t.Run(name, func(t *testing.T) { _, err := parseXML(source); assert.Error(t, err) })
	}
}
