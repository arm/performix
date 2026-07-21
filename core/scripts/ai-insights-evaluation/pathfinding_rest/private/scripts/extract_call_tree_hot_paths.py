#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Build a compact hot call-tree view from rendered drilldown rows."""

from __future__ import annotations

import argparse
import json
from collections import defaultdict
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Dict, List, Set


def _to_float(value: Any) -> float:
    try:
        if value is None:
            return 0.0
        return float(value)
    except (TypeError, ValueError):
        return 0.0


def _clean_number(value: float) -> float | int:
    if abs(value - round(value)) < 1e-9:
        return int(round(value))
    return round(value, 6)


def _clean_percent(value: float) -> float | int:
    if abs(value - round(value)) < 1e-9:
        return int(round(value))
    return round(value, 2)


@dataclass
class Node:
    call_tree_id: int
    call_tree_parent_id: int
    label: str
    image_name: str
    self_samples: float
    total_samples: float
    row: Dict[str, Any]
    children: List[int] = field(default_factory=list)


def _node_sort_key(node_id: int, nodes: Dict[int, Node]) -> tuple[float, float, str, int]:
    return (
        -nodes[node_id].total_samples,
        -nodes[node_id].self_samples,
        nodes[node_id].label,
        node_id,
    )


def _flatten_rows(root_ids: List[int], nodes: Dict[int, Node], included: Set[int]) -> List[Dict[str, Any]]:
    flattened: List[Dict[str, Any]] = []
    total_self_samples = total_call_tree_self_samples([node.row for node in nodes.values()])

    def visit(node_id: int, depth: int) -> None:
        node = nodes[node_id]
        child_ids = [child_id for child_id in node.children if child_id in included]
        child_ids.sort(key=lambda child_id: _node_sort_key(child_id, nodes))
        row = dict(node.row)
        row["id"] = node.call_tree_id
        row["parent_id"] = None if node.call_tree_parent_id in (-1, 0) else node.call_tree_parent_id
        row["child_ids"] = child_ids
        row["depth"] = depth
        row["self_samples"] = _clean_number(node.self_samples)
        row["total_samples"] = _clean_number(node.total_samples)
        row["self_percent"] = _clean_percent((node.self_samples / total_self_samples) * 100.0) if total_self_samples > 0 else 0
        row["total_percent"] = _clean_percent((node.total_samples / total_self_samples) * 100.0) if total_self_samples > 0 else 0
        row.pop("call_tree_id", None)
        row.pop("call_tree_parent_id", None)
        flattened.append(row)
        for child_id in child_ids:
            visit(child_id, depth + 1)

    for root_id in root_ids:
        visit(root_id, 0)
    return flattened


def select_dominant_subtree(
    nodes: Dict[int, Node],
    root_ids: List[int],
    threshold_percent: float,
) -> Set[int]:
    total_self_samples = total_call_tree_self_samples([node.row for node in nodes.values()])
    if total_self_samples <= 0:
        return set()

    # Keep only subtrees whose inclusive share of the root stays above the
    # floor implied by the requested threshold. For example, threshold=99
    # keeps nodes above 1% of the root total.
    cutoff_samples = total_self_samples * ((100.0 - threshold_percent) / 100.0)
    included_ids: Set[int] = set()

    for root_id in root_ids:
        def visit(node_id: int) -> None:
            node = nodes[node_id]
            if node.total_samples <= cutoff_samples:
                return
            included_ids.add(node_id)
            child_ids = sorted(
                node.children,
                key=lambda child_id: _node_sort_key(child_id, nodes),
            )
            for child_id in child_ids:
                visit(child_id)

        if root_id in nodes:
            visit(root_id)

    return included_ids


def total_call_tree_self_samples(rows: List[Dict[str, Any]]) -> float:
    return sum(
        _to_float(row.get("self_samples"))
        for row in rows
        if int(row.get("call_tree_parent_id", -1)) != -1 and _to_float(row.get("self_samples")) > 0
    )


def reduce_call_tree_rows(rows: List[Dict[str, Any]], threshold_percent: float) -> List[Dict[str, Any]]:
    """Return a thresholded call-tree row subset while preserving row fields.

    The input must be the full list of Performix call-tree drilldown rows. The
    returned list is an abridged subset of those rows ordered as a tree,
    preserving the original row fields while normalizing `call_tree_id` and
    `call_tree_parent_id` to `id` and `parent_id`. Each returned row also
    includes derived `child_ids`, `depth`, `self_percent`, and `total_percent`
    fields for easier model consumption. The selection walks the tree
    top-down and prunes any subtree once a node's inclusive samples fall to or
    below the floor implied by `threshold_percent`. For example, a threshold of
    99 keeps only nodes above 1% of the root total.
    """
    nodes: Dict[int, Node] = {}
    children_by_parent: Dict[int, List[int]] = defaultdict(list)
    synthetic_root_ids: Set[int] = set()

    for row in rows:
        call_tree_id = int(row.get('call_tree_id'))
        parent_id = int(row.get('call_tree_parent_id'))
        label = str(row.get('label') or 'No label')
        image_name = str(row.get('image_name') or 'UNKNOWN_IMAGE')
        self_samples = _to_float(row.get('self_samples'))
        total_samples = _to_float(row.get('total_samples'))
        nodes[call_tree_id] = Node(
            call_tree_id=call_tree_id,
            call_tree_parent_id=parent_id,
            label=label,
            image_name=image_name,
            self_samples=self_samples,
            total_samples=total_samples,
            row=row,
        )
        if parent_id == -1:
            synthetic_root_ids.add(call_tree_id)
        else:
            children_by_parent[parent_id].append(call_tree_id)

    for node_id, node in nodes.items():
        node.children = children_by_parent.get(node_id, [])

    root_ids = [
        node_id
        for node_id, node in nodes.items()
        if node.call_tree_parent_id in (-1, 0)
    ]
    root_ids = [node_id for node_id in root_ids if node_id not in synthetic_root_ids]
    root_ids.sort(key=lambda node_id: _node_sort_key(node_id, nodes))

    included_ids = select_dominant_subtree(
        nodes=nodes,
        root_ids=root_ids,
        threshold_percent=threshold_percent,
    )

    selected_root_ids = [
        node_id
        for node_id in included_ids
        if nodes[node_id].call_tree_parent_id in (-1, 0) or nodes[node_id].call_tree_parent_id not in included_ids
    ]
    selected_root_ids = [node_id for node_id in selected_root_ids if node_id not in synthetic_root_ids]
    selected_root_ids.sort(key=lambda node_id: _node_sort_key(node_id, nodes))

    return _flatten_rows(selected_root_ids, nodes, included_ids)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--threshold-percent', type=float, default=99.0)
    parser.add_argument('drilldown_rows_json')
    args = parser.parse_args()

    payload = json.loads(open(args.drilldown_rows_json, 'r', encoding='utf-8').read())
    rows = (((payload.get('data') or {}).get('rows')) or [])
    reduced_rows = reduce_call_tree_rows(rows, args.threshold_percent)
    result = {
        'created_at_utc': datetime.now(timezone.utc).isoformat(),
        'measurement': 'periodic_samples',
        'selection_basis': 'top_down_total_percent_cutoff',
        'threshold_percent': args.threshold_percent,
        'total_samples': _clean_number(total_call_tree_self_samples(rows)),
        'selected_node_count': len(reduced_rows),
        'node_count': len(rows),
        'rows': reduced_rows,
    }
    print(json.dumps(result, indent=2))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
