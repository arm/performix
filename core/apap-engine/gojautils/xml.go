// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package gojautils

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
)

// RegisterXML exposes Go's encoding/xml decoder to Goja tool helpers. The adapter
// builds a JavaScript-friendly tree of names, attributes and children; it does
// not implement XML token parsing. Text and comments are omitted because the
// collector catalogues describe counters through elements and attributes.
// Input size, depth and node limits bound the tree. Namespaces and directives
// are rejected because they are outside the supported collector format.
func RegisterXML(registry *require.Registry) {
	registry.RegisterNativeModule("performix/xml", func(vm *goja.Runtime, module *goja.Object) {
		exports := module.Get("exports").ToObject(vm)
		if err := exports.Set("parse", parseXML); err != nil {
			panic(vm.NewGoError(err))
		}
	})
}

func parseXML(source string) (map[string]any, error) {
	if len(source) > 8*1024*1024 {
		return nil, fmt.Errorf("XML exceeds 8 MiB")
	}
	decoder := xml.NewDecoder(strings.NewReader(source))
	var root map[string]any
	var stack []map[string]any
	nodes := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			nodes++
			if len(stack) >= 64 || nodes > 100000 {
				return nil, fmt.Errorf("XML structure exceeds limits")
			}
			if t.Name.Space != "" {
				return nil, fmt.Errorf("XML namespaces are not supported")
			}
			attrs := map[string]string{}
			for _, a := range t.Attr {
				if a.Name.Space != "" {
					return nil, fmt.Errorf("XML namespaces are not supported")
				}
				if _, exists := attrs[a.Name.Local]; exists {
					return nil, fmt.Errorf("duplicate XML attribute %q", a.Name.Local)
				}
				attrs[a.Name.Local] = a.Value
			}
			node := map[string]any{"name": t.Name.Local, "attributes": attrs, "children": []any{}}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("multiple XML roots")
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent["children"] = append(parent["children"].([]any), node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.Directive:
			return nil, fmt.Errorf("XML directives are not supported")
		case xml.CharData:
			if len(stack) == 0 && strings.TrimSpace(string(t)) != "" {
				return nil, fmt.Errorf("text outside XML root")
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, fmt.Errorf("incomplete XML document")
	}
	return root, nil
}
