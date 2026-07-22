// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type RendererRegistry struct {
	entries map[string]func() render.Renderer
}

func NewRegistry() *RendererRegistry {
	return &RendererRegistry{entries: map[string]func() render.Renderer{
		// Add new entries in this map when they are created in renderimpls
		"CmnCsvAverage": func() render.Renderer { return &CmnCsvAverageRenderer{} },
		"StreamlineAnalyzeFunctionProfileRenderer":  func() render.Renderer { return &StreamlineAnalyzeFunctionProfileRenderer{} },
		"StreamlineAnalyzeFunctionProfileRenderer2": func() render.Renderer { return &StreamlineAnalyzeFunctionProfileRenderer2{} },
		"CompareDrilldownFlat":                      func() render.Renderer { return &CompareDrilldownFlat{} },
		"CompareDrilldownCallStacks":                func() render.Renderer { return &CompareDrilldownCallStacks{} },
		"TargetInfoRenderer":                        func() render.Renderer { return &TargetInfoRenderer{} },
		"StreamlineAnalyzeFlatFunctions":            func() render.Renderer { return &StreamlineAnalyzeFlatFunctionProfileRenderer{} },
		"StreamlineAnalyzeFlatFunctions2":           func() render.Renderer { return &StreamlineAnalyzeFlatFunctionProfileRenderer2{} },
		"CSV":                                       func() render.Renderer { return &CSVRenderer{} },
		"SQL":                                       func() render.Renderer { return &SQLRenderer{} },
		"Log":                                       func() render.Renderer { return &LogRenderer{} },
		"LatencyBreakdown":                          func() render.Renderer { return &LatencyBreakdownRenderer{} },
		"TLBWalkScore":                              func() render.Renderer { return &TLBWalkScoreRenderer{} },
		"CompareFlatTable":                          func() render.Renderer { return &CompareFlatTable{} },
		"StreamlineAnalyzeSymbols":                  func() render.Renderer { return &StreamlineAnalyzeSymbolsRenderer{} },
		"SourceCodeAttribution":                     func() render.Renderer { return &SourceCodeAttributionRenderer{} },
		"CacheSharing":                              func() render.Renderer { return &CacheSharingRenderer{} },
		"ProcessesAndThreadsParser":                 func() render.Renderer { return &ProcessesAndThreadsRenderer{} },
		"SlAnalyzeRenderer":                         func() render.Renderer { return &SlAnalyzeRenderer{} },
		"SysutilTimelineCanonicalRenderer":          func() render.Renderer { return &SysutilTimelineCanonicalRenderer{} },
		"DisassemblyRenderer":                       func() render.Renderer { return &DisassemblyRenderer{} },
		"TimeRangeParser":                           func() render.Renderer { return &TimeRangeRenderer{} },
		"DummyRenderer":                             func() render.Renderer { return &DummyRenderer{} },
	}}
}

func (r *RendererRegistry) NewRenderer(rendererName string) (render.Renderer, error) {
	if factory, ok := r.entries[rendererName]; ok {
		return factory(), nil
	}

	return nil, fmt.Errorf("unrecognized renderer name '%s'", rendererName)
}

func (r *RendererRegistry) AvailableRendererNames() []string {
	return util.CopyKeysSlice(r.entries)
}
