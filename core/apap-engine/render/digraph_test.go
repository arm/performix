// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDigraphNodeExists(t *testing.T) {
	g := Digraph{
		nodes: NodeSet{
			1: {},
			2: {},
		},
		successors: map[NodeID]NodeSet{
			1: {
				2: {},
			},
		},
	}

	t.Run("returns true when the node exists", func(t *testing.T) {
		assert.True(t, g.NodeExists(1))
	})
	t.Run("returns false when the node does not exist", func(t *testing.T) {
		assert.False(t, g.NodeExists(99))
	})
}

func TestDigraphRemoveAllNodes(t *testing.T) {
	t.Run("removes a single node and its incoming/outgoing edges", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
				3: {},
				4: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					2: {},
					3: {},
				},
				4: {
					1: {},
					3: {},
				},
			},
		}

		updated, err := g.RemoveAllNodes(NodeSet{
			1: {},
		})
		assert.NoError(t, err)

		expectedGraph := Digraph{
			nodes: NodeSet{
				2: {},
				3: {},
				4: {},
			},
			successors: map[NodeID]NodeSet{
				4: {
					3: {},
				},
			},
		}

		assert.Equal(t, expectedGraph, updated)
	})
	t.Run("removes multiple nodes and their incoming/outgoing edges", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
				3: {},
				4: {},
				5: {},
				6: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					2: {},
					3: {},
					4: {},
				},
				2: {
					5: {},
				},
				3: {
					5: {},
				},
				4: {
					6: {},
				},
				5: {
					6: {},
				},
			},
		}

		updated, err := g.RemoveAllNodes(NodeSet{
			2: {},
			4: {},
		})
		assert.NoError(t, err)

		expectedGraph := Digraph{
			nodes: NodeSet{
				1: {},
				3: {},
				5: {},
				6: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					3: {},
				},
				3: {
					5: {},
				},
				5: {
					6: {},
				},
			},
		}

		assert.Equal(t, expectedGraph, updated)
	})
	t.Run("returns an error when any node does not exist", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					2: {},
				},
			},
		}

		_, err := g.RemoveAllNodes(NodeSet{
			99: {},
		})
		assert.ErrorContains(t, err, "does not exist")
	})
}

func TestDigraphGetRoots(t *testing.T) {
	g := Digraph{
		nodes: NodeSet{
			1: {},
			2: {},
			3: {},
			4: {},
			5: {},
		},
		successors: map[NodeID]NodeSet{
			1: {
				2: {},
				3: {},
			},
			3: {
				4: {},
			},
		},
	}

	t.Run("returns exactly the roots of the graph", func(t *testing.T) {
		want := NodeSet{
			1: {},
			5: {},
		}

		assert.Equal(t, want, g.GetRoots())
	})
}

func TestDigraphGetDirectChildren(t *testing.T) {
	g := Digraph{
		nodes: NodeSet{
			1: {},
			2: {},
			3: {},
			4: {},
		},
		successors: map[NodeID]NodeSet{
			1: {
				2: {},
				3: {},
			},
			3: {
				4: {},
			},
		},
	}

	t.Run("returns the immediate children of the requested node", func(t *testing.T) {
		want := NodeSet{
			2: {},
			3: {},
		}

		got, err := g.GetDirectChildren(1)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})
	t.Run("returns an empty set when a node has no children", func(t *testing.T) {
		got, err := g.GetDirectChildren(4)
		assert.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("returns an error for a missing node", func(t *testing.T) {
		_, err := g.GetDirectChildren(99)
		assert.ErrorContains(t, err, "does not exist")
	})
}

func TestDigraphGetAllChildren(t *testing.T) {
	t.Run("returns all descendants in a tree", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
				3: {},
				4: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					2: {},
					3: {},
				},
				3: {
					4: {},
				},
			},
		}

		want := NodeSet{
			2: {},
			3: {},
			4: {},
		}

		got, err := g.GetAllChildren(1)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})
	t.Run("handles converging paths without cycles", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
				3: {},
				4: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					2: {},
					3: {},
				},
				2: {
					4: {},
				},
				3: {
					4: {},
				},
			},
		}

		want := NodeSet{
			2: {},
			3: {},
			4: {},
		}

		got, err := g.GetAllChildren(1)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})
	t.Run("ignores cycles but includes the start node if it is a descendant", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
				3: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					2: {},
				},
				2: {
					3: {},
				},
				3: {
					1: {},
				},
			},
		}

		want := NodeSet{
			1: {},
			2: {},
			3: {},
		}

		got, err := g.GetAllChildren(1)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})
	t.Run("includes the node itself for a self-loop", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					1: {},
				},
			},
		}

		want := NodeSet{
			1: {},
		}

		got, err := g.GetAllChildren(1)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})
	t.Run("returns an error when the node does not exist", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
			},
			successors: map[NodeID]NodeSet{},
		}

		_, err := g.GetAllChildren(99)
		assert.ErrorContains(t, err, "does not exist")
	})
}

func TestDigraphGetDirectParents(t *testing.T) {
	g := Digraph{
		nodes: NodeSet{
			1: {},
			2: {},
			3: {},
			4: {},
			5: {},
		},
		successors: map[NodeID]NodeSet{
			1: {
				2: {},
				3: {},
			},
			3: {
				4: {},
			},
			5: {
				2: {},
			},
		},
	}

	t.Run("returns the immediate parents of the requested node", func(t *testing.T) {
		want := NodeSet{
			1: {},
			5: {},
		}

		got, err := g.GetDirectParents(2)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})
	t.Run("returns an empty set when a node has no parents", func(t *testing.T) {
		got, err := g.GetDirectParents(1)
		assert.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("returns an error for a missing node", func(t *testing.T) {
		_, err := g.GetDirectParents(99)
		assert.ErrorContains(t, err, "does not exist")
	})
}

func TestDigraphContainsCycle(t *testing.T) {
	t.Run("returns false for an empty graph", func(t *testing.T) {
		g := Digraph{
			nodes:      NodeSet{},
			successors: map[NodeID]NodeSet{},
		}

		found, cycle := g.ContainsCycle()
		assert.False(t, found)
		assert.Empty(t, cycle)
	})
	t.Run("returns false for a graph with no edges", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
			},
			successors: map[NodeID]NodeSet{},
		}

		found, cycle := g.ContainsCycle()
		assert.False(t, found)
		assert.Empty(t, cycle)
	})
	t.Run("detects a self-cycle", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					1: {},
				},
			},
		}

		found, cycle := g.ContainsCycle()
		assert.True(t, found)
		assert.Equal(t, []NodeID{1, 1}, cycle)
	})
	t.Run("detects a normal cycle", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
				3: {},
				4: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					2: {},
				},
				2: {
					3: {},
				},
				3: {
					4: {},
				},
				4: {
					2: {},
				},
			},
		}

		found, cycle := g.ContainsCycle()
		assert.True(t, found)
		assert.Equal(t, []NodeID{2, 3, 4, 2}, cycle)
	})
	t.Run("detects a cycle when there is no root", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					2: {},
				},
				2: {
					1: {},
				},
			},
		}

		found, cycle := g.ContainsCycle()
		assert.True(t, found)
		permA := slices.Equal([]NodeID{1, 2, 1}, cycle)
		permB := slices.Equal([]NodeID{2, 1, 2}, cycle)
		assert.True(t, permA || permB)
	})
	t.Run("detects a cycle next to a root", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
				2: {},
				3: {},
				4: {},
			},
			successors: map[NodeID]NodeSet{
				1: {
					2: {},
				},
				3: {
					4: {},
				},
				4: {
					3: {},
				},
			},
		}

		found, cycle := g.ContainsCycle()
		assert.True(t, found)
		permA := slices.Equal([]NodeID{3, 4, 3}, cycle)
		permB := slices.Equal([]NodeID{4, 3, 4}, cycle)
		assert.True(t, permA || permB)
	})
}

func TestDigraphIsEmpty(t *testing.T) {
	t.Run("returns true for an empty graph", func(t *testing.T) {
		g := Digraph{
			nodes:      NodeSet{},
			successors: map[NodeID]NodeSet{},
		}

		assert.True(t, g.IsEmpty())
	})
	t.Run("returns false when the graph has nodes", func(t *testing.T) {
		g := Digraph{
			nodes: NodeSet{
				1: {},
			},
			successors: map[NodeID]NodeSet{},
		}

		assert.False(t, g.IsEmpty())
	})
}

func TestNewDigraph(t *testing.T) {
	t.Run("creates a graph with the provided nodes and successors", func(t *testing.T) {
		nodes := NodeSet{
			1: {},
			2: {},
			3: {},
		}
		successors := map[NodeID]NodeSet{
			1: {
				2: {},
			},
			2: {
				3: {},
			},
		}

		g, err := NewDigraph(nodes, successors)
		assert.NoError(t, err)
		assert.Equal(t, nodes, g.nodes)
		assert.Equal(t, successors, g.successors)
	})
	t.Run("returns an error when a successor references a missing node", func(t *testing.T) {
		nodes := NodeSet{
			1: {},
			2: {},
		}
		successors := map[NodeID]NodeSet{
			1: {
				3: {},
			},
		}

		_, err := NewDigraph(nodes, successors)
		assert.ErrorContains(t, err, "unknown successor")
	})
}
