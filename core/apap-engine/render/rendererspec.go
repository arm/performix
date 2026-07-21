// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type RendererSpec struct {
	Graph       Digraph
	Configs     RendererConfigList
	DataSources []map[string][]DataSource
}

func NewRendererSpec(configs RendererConfigList, dataSources []map[string][]DataSource) (RendererSpec, []error, error) {
	nodes := make(NodeSet, len(configs))
	errs := make([]error, len(configs))
	successors := map[NodeID]NodeSet{}
	for i := range configs {
		nodes[NodeID(i)] = struct{}{}
		errs[i] = addSuccessorsFromDataSources(successors, dataSources[i], configs, i)
	}

	digraph, err := NewDigraph(nodes, successors)
	if err != nil {
		return RendererSpec{}, nil, err
	}
	propagationErrs, err := propagateCycleErrs(digraph, configs, errs)
	if err != nil {
		return RendererSpec{}, nil, err
	}
	errs = merge(errs, propagationErrs)

	spec := RendererSpec{
		Graph:       digraph,
		Configs:     configs,
		DataSources: dataSources,
	}
	return spec, errs, nil
}

func addSuccessorsFromDataSources(successors map[NodeID]NodeSet, dataSources map[string][]DataSource, configs RendererConfigList, dependeeIndex int) error {
	if dataSources == nil {
		return nil
	}
	var err error
	for _, sources := range dataSources {
		for _, ds := range sources {
			otr, ok := ds.(*OutputTableRef)
			if !ok {
				continue
			}

			dependencyIndex := rendererIndexFromID(otr.RendererID, configs)
			if dependencyIndex == -1 {
				if err == nil {
					err = fmt.Errorf("unknown renderer ID %v", otr.RendererID)
				}
				continue
			}
			// Add dependeeIndex as a successor of dependencyIndex, creating the map of successors if necessary
			dependency := successors[NodeID(dependencyIndex)]
			if dependency == nil {
				successors[NodeID(dependencyIndex)] = NodeSet{NodeID(dependeeIndex): {}}
			} else {
				dependency[NodeID(dependeeIndex)] = struct{}{}
			}
		}
	}

	return err
}

func rendererIndexFromID(id string, configs RendererConfigList) int {
	for i, conf := range configs {
		if conf.ID != nil && *conf.ID == id {
			return i
		}
	}

	return -1
}

// propagateCycleErrs identifies any cycles in the provided digraph, and propagates an appropriate
// error to all renderers corresponding to nodes which are elements of a cycle
func propagateCycleErrs(digraph Digraph, configs RendererConfigList, configErrs []error) ([]error, error) {
	propagationErrs := make([]error, len(configErrs))
	currentGraph := digraph
	cycleFound, cycle := currentGraph.ContainsCycle()
	// While this graph contains a cycle, remove all nodes in that cycle from the graph copy
	// and recheck for cycles
	for cycleFound {
		stringCycle := util.Map(cycle, func(i NodeID) string {
			return fmt.Sprintf("%v (id: `%v`)", configs[i].Name, configs[i].GetDisplayID())
		})
		metadata := map[string]string{"cycle": strings.Join(stringCycle, " -> ")}
		toRemove := NodeSet{}
		for _, rendererIndex := range cycle {
			if configErrs[rendererIndex] == nil {
				metadata["type"] = configs[rendererIndex].Name
				metadata["id"] = configs[rendererIndex].GetDisplayID()
				propagationErrs[rendererIndex] = message.New(message.EngineRenderRendererspecRendererDependencyCycle).WithMetadata(metadata)
			}

			toRemove[rendererIndex] = struct{}{}
		}
		// Remove all nodes in this cycle and recompute
		updatedGraph, err := currentGraph.RemoveAllNodes(toRemove)
		if err != nil {
			return nil, err
		}
		currentGraph = updatedGraph
		cycleFound, cycle = currentGraph.ContainsCycle()
	}
	return propagationErrs, nil
}

func merge(existing []error, new []error) []error {
	lens := []int{len(existing), len(new)}
	slices.Sort(lens)

	// Make a new slice of the longer of the two lengths
	mergedErrs := make([]error, lens[1])
	for i := range mergedErrs {
		if i < len(existing) && existing[i] != nil {
			mergedErrs[i] = existing[i]
			continue
		}
		if i < len(new) {
			mergedErrs[i] = new[i]
		}
	}

	return mergedErrs
}
