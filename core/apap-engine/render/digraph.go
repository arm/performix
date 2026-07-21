// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"maps"
	"slices"
)

func nodeNotExistError(node NodeID) error {
	return fmt.Errorf("the node `%v` does not exist in this graph", node)
}

// Digraph represents a directed graph
type Digraph struct {
	nodes      NodeSet
	successors map[NodeID]NodeSet
}
type NodeID int
type NodeSet map[NodeID]struct{}

// NodeExists returns true if the provided node exists in this graph
func (g *Digraph) NodeExists(node NodeID) bool {
	_, ok := g.nodes[node]
	return ok
}

// RemoveNode returns a copy of this Digraph with the specified node removed.
// Returns an error if <node> is not a node in this Digraph
func (g *Digraph) RemoveNode(node NodeID) (Digraph, error) {
	return g.RemoveAllNodes(NodeSet{node: {}})
}

// RemoveAllNodes returns a copy of this Digraph with the specified nodes removed.
// Returns an error if any <node> is not a node in this Digraph
func (g *Digraph) RemoveAllNodes(nodes NodeSet) (Digraph, error) {
	newNodes := maps.Clone(g.nodes)
	newSuccessors := cloneSuccessors(g.successors)
	for node := range nodes {
		if !g.NodeExists(node) {
			return Digraph{}, nodeNotExistError(node)
		}

		parents, err := g.GetDirectParents(node)
		if err != nil {
			return Digraph{}, err
		}

		delete(newNodes, node)
		delete(newSuccessors, node)

		for p := range parents {
			// Remove this node from the successors of all of its parents
			delete(newSuccessors[p], node)
		}
	}

	return NewDigraph(newNodes, newSuccessors)
}

// GetRoots returns the set of nodes in the graph which do not have any parents
func (g *Digraph) GetRoots() NodeSet {
	roots := NodeSet{}
	maps.Copy(roots, g.nodes)
	// Delete all successors of each node, leaving only nodes which aren't successors of any other nodes
	for n := range g.nodes {
		s, ok := g.successors[n]
		if !ok {
			continue
		}
		for successor := range s {
			delete(roots, successor)
		}
	}

	return roots
}

// GetDirectChildren returns the set of nodes which have <node> as a parent.
// If <node> has a self-cycle, it will be included in this set.
// Returns an error if <node> is not a node in this Digraph
func (g *Digraph) GetDirectChildren(node NodeID) (NodeSet, error) {
	if !g.NodeExists(node) {
		return nil, nodeNotExistError(node)
	}
	return g.getDirectChildren(node), nil
}

func (g *Digraph) getDirectChildren(node NodeID) NodeSet {
	if s, ok := g.successors[node]; ok && s != nil {
		return s
	}
	return NodeSet{}
}

// GetAllChildren returns the set of nodes which have <node> as an ancestor.
// If <node> has a self-cycle, it will be included in this set.
// Returns an error if <node> is not a node in this Digraph
func (g *Digraph) GetAllChildren(node NodeID) (NodeSet, error) {
	if !g.NodeExists(node) {
		return nil, nodeNotExistError(node)
	}
	return g.getAllChildren(node), nil
}

func (g *Digraph) getAllChildren(node NodeID) NodeSet {
	allChildren := NodeSet{}
	hasCycleToRoot := false

	var dfs func(n NodeID)
	dfs = func(n NodeID) {
		if _, ok := allChildren[n]; ok {
			// Already seen this node
			if n == node {
				hasCycleToRoot = true
			}
			return
		}
		allChildren[n] = struct{}{}
		for child := range g.getDirectChildren(n) {
			dfs(child)
		}
	}

	dfs(node)

	if !hasCycleToRoot {
		delete(allChildren, node)
	}

	return allChildren
}

// GetDirectParents returns the set of nodes which are parents of <node>
// Returns an error if <node> is not a node in this Digraph
func (g *Digraph) GetDirectParents(node NodeID) (NodeSet, error) {
	if !g.NodeExists(node) {
		return nil, nodeNotExistError(node)
	}
	parents := NodeSet{}

	for n := range g.nodes {
		s, ok := g.successors[n]
		if !ok {
			continue
		}
		for successor := range s {
			if successor == node {
				parents[n] = struct{}{}
			}
		}
	}

	return parents, nil
}

// ContainsCycle determines whether this graph contains a cycle or not. If the graph contains
// a cycle, ContainsCycle will return true, and an ordered slice of nodes which form a cycle
// in this graph. If the graph has multiple cycles, there is no guarantee about which cycle will
// be found. If the graph has no cycles, ContainsCycle returns false.
func (g *Digraph) ContainsCycle() (bool, []NodeID) {
	if g.IsEmpty() {
		return false, []NodeID{}
	}

	// Search for cycle descending from each root
	roots := g.GetRoots()
	unvisited := maps.Clone(g.nodes)
	for root := range roots {
		found, cycle := g.findCycle(root)
		if found {
			return true, cycle
		}
		delete(unvisited, root)
		for node := range g.getAllChildren(root) {
			delete(unvisited, node)
		}
	}

	// The existence of remaining unvisited nodes means there must be a cycle in these nodes
	for node := range unvisited {
		found, cycle := g.findCycle(node)
		if found {
			return true, cycle
		}
	}
	return false, nil
}

// findCycle returns a slice of nodes which form a cycle descended from the provided node, or an empty
// slice if no such cycle exists.
func (g *Digraph) findCycle(node NodeID) (bool, []NodeID) {
	// Each recursion needs a new map in order to keep track of seen nodes on a per-iteration basis
	var dfs func(n NodeID, seen NodeSet) (bool, []NodeID)
	dfs = func(n NodeID, seen NodeSet) (bool, []NodeID) {
		// Seen tracks previous nodes on this path
		if _, ok := seen[n]; ok {
			// Already seen this node - cycle detected
			return true, []NodeID{n}
		}
		seen[n] = struct{}{}

		// Recurse for each child
		for child := range g.getDirectChildren(n) {
			found, path := dfs(child, maps.Clone(seen))
			if found {
				// If we haven't already seen the leaf of the cycle, that means this node is before the cycle started
				// so shouldn't be included in the cycle path
				if _, ok := seen[path[0]]; ok {
					path = append(path, n)
				}
				return true, path
			}
		}
		return false, nil
	}

	found, path := dfs(node, NodeSet{})
	if found {
		slices.Reverse(path)
		return true, path
	}
	return false, nil
}

// IsEmpty returns true if this graph has no nodes
func (g *Digraph) IsEmpty() bool {
	return len(g.nodes) == 0
}

// NewDigraph returns a new Digraph struct with the provided nodes and edges. Returns an error
// if these nodes and edges form an invalid graph.
func NewDigraph(nodes NodeSet, successors map[NodeID]NodeSet) (Digraph, error) {
	if err := validateNodesAndSuccessors(nodes, successors); err != nil {
		return Digraph{}, err
	}
	return Digraph{
		nodes:      nodes,
		successors: successors,
	}, nil
}

// validateNodesAndSuccessors validates that the provided sets of nodes and children are valid. The only invalid
// state is if a child relation references a non-existent node.
func validateNodesAndSuccessors(nodes NodeSet, successors map[NodeID]NodeSet) error {
	for node := range nodes {
		s, ok := successors[node]
		if !ok {
			continue
		}
		for successor := range s {
			if _, ok = nodes[successor]; !ok {
				return fmt.Errorf("node %v has an unknown successor %v", node, successor)
			}
		}
	}

	return nil
}

func cloneSuccessors(successors map[NodeID]NodeSet) map[NodeID]NodeSet {
	returnMap := make(map[NodeID]NodeSet, len(successors))
	for node, sucSet := range successors {
		returnMap[node] = maps.Clone(sucSet)
	}
	return returnMap
}
