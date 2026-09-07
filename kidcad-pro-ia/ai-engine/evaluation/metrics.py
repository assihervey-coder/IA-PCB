"""Routing quality metrics (pure numpy/stdlib, torch-free).

All functions accept "routes": a list of route dicts as produced by
``PCBRouteEnv.astar_route`` / ``path_to_route``::

    {"net": str, "segments": [{"a": {"x","y","layer"}, "b": {...}, "width_mm": w}],
     "vias": [{"x","y","from_layer","to_layer", ...}], "length_mm": float,
     "completed": bool}
"""

from __future__ import annotations

import math
from typing import List, Sequence, Tuple

Point = Tuple[float, float]

_EPS = 1e-9


def route_length_mm(routes: Sequence[dict]) -> float:
    """Total routed length in mm (uses ``length_mm`` when present)."""
    total = 0.0
    for route in routes or []:
        declared = route.get("length_mm")
        if declared is not None:
            total += float(declared)
            continue
        for seg in route.get("segments") or []:
            a = seg.get("a") or {}
            b = seg.get("b") or {}
            total += math.hypot(
                float(b.get("x", 0.0)) - float(a.get("x", 0.0)),
                float(b.get("y", 0.0)) - float(a.get("y", 0.0)),
            )
    return round(total, 4)


def via_count(routes: Sequence[dict]) -> int:
    """Total number of vias across all routes."""
    return sum(len(route.get("vias") or []) for route in routes or [])


def completion_rate(routes: Sequence[dict]) -> float:
    """Fraction of fully routed nets (0.0 when there is no net)."""
    if not routes:
        return 0.0
    completed = sum(1 for route in routes if bool(route.get("completed")))
    return completed / len(routes)


def point_segment_distance(point: Point, seg_a: Point, seg_b: Point) -> float:
    """Euclidean distance between a point and a segment."""
    px, py = point
    ax, ay = seg_a
    bx, by = seg_b
    dx, dy = bx - ax, by - ay
    length_sq = dx * dx + dy * dy
    if length_sq <= _EPS:
        return math.hypot(px - ax, py - ay)
    t = ((px - ax) * dx + (py - ay) * dy) / length_sq
    t = min(1.0, max(0.0, t))
    return math.hypot(px - (ax + t * dx), py - (ay + t * dy))


def _segments_intersect(p1: Point, p2: Point, p3: Point, p4: Point) -> bool:
    """True when segments p1-p2 and p3-p4 intersect (incl. collinear touch)."""

    def orientation(a: Point, b: Point, c: Point) -> float:
        return (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0])

    def on_segment(a: Point, b: Point, c: Point) -> bool:
        return (
            min(a[0], b[0]) - _EPS <= c[0] <= max(a[0], b[0]) + _EPS
            and min(a[1], b[1]) - _EPS <= c[1] <= max(a[1], b[1]) + _EPS
        )

    d1 = orientation(p3, p4, p1)
    d2 = orientation(p3, p4, p2)
    d3 = orientation(p1, p2, p3)
    d4 = orientation(p1, p2, p4)
    if ((d1 > _EPS and d2 < -_EPS) or (d1 < -_EPS and d2 > _EPS)) and (
        (d3 > _EPS and d4 < -_EPS) or (d3 < -_EPS and d4 > _EPS)
    ):
        return True
    if abs(d1) <= _EPS and on_segment(p3, p4, p1):
        return True
    if abs(d2) <= _EPS and on_segment(p3, p4, p2):
        return True
    if abs(d3) <= _EPS and on_segment(p1, p2, p3):
        return True
    if abs(d4) <= _EPS and on_segment(p1, p2, p4):
        return True
    return False


def segment_distance(seg_a1: Point, seg_a2: Point, seg_b1: Point, seg_b2: Point) -> float:
    """Euclidean distance between two 2D segments (0 when intersecting)."""
    if _segments_intersect(seg_a1, seg_a2, seg_b1, seg_b2):
        return 0.0
    return min(
        point_segment_distance(seg_a1, seg_b1, seg_b2),
        point_segment_distance(seg_a2, seg_b1, seg_b2),
        point_segment_distance(seg_b1, seg_a1, seg_a2),
        point_segment_distance(seg_b2, seg_a1, seg_a2),
    )


def _flat_segments(routes: Sequence[dict]) -> List[Tuple[str, int, Point, Point]]:
    """Flatten routes into ``(net, layer, a, b)`` tuples."""
    out: List[Tuple[str, int, Point, Point]] = []
    for route in routes or []:
        net = str(route.get("net", "") or "")
        for seg in route.get("segments") or []:
            a = seg.get("a") or {}
            b = seg.get("b") or {}
            out.append(
                (
                    net,
                    int(a.get("layer", 0) or 0),
                    (float(a.get("x", 0.0) or 0.0), float(a.get("y", 0.0) or 0.0)),
                    (float(b.get("x", 0.0) or 0.0), float(b.get("y", 0.0) or 0.0)),
                )
            )
    return out


def _flat_vias(routes: Sequence[dict]) -> List[Tuple[str, Point, set]]:
    """Flatten routes into ``(net, (x, y), {layers})`` via tuples."""
    out: List[Tuple[str, Point, set]] = []
    for route in routes or []:
        net = str(route.get("net", "") or "")
        for via in route.get("vias") or []:
            out.append(
                (
                    net,
                    (float(via.get("x", 0.0) or 0.0), float(via.get("y", 0.0) or 0.0)),
                    {int(via.get("from_layer", 0) or 0), int(via.get("to_layer", 0) or 0)},
                )
            )
    return out


def drc_penalty_estimate(routes: Sequence[dict], clearance_mm: float = 0.2) -> int:
    """Approximate DRC violation count between routed nets.

    Counts, for pairs of DIFFERENT nets:
      * same-layer segments closer than ``clearance_mm``;
      * vias whose layer ranges overlap and whose 2D distance is too small;
      * a via and a foreign segment (segment layer within the via range)
        closer than ``clearance_mm``.
    """
    segments = _flat_segments(routes)
    vias = _flat_vias(routes)
    threshold = float(clearance_mm) - _EPS
    count = 0
    for i in range(len(segments)):
        net_a, layer_a, a1, a2 = segments[i]
        for j in range(i + 1, len(segments)):
            net_b, layer_b, b1, b2 = segments[j]
            if net_a == net_b or layer_a != layer_b:
                continue
            if segment_distance(a1, a2, b1, b2) < threshold:
                count += 1
    for i in range(len(vias)):
        net_a, pos_a, layers_a = vias[i]
        for j in range(i + 1, len(vias)):
            net_b, pos_b, layers_b = vias[j]
            if net_a == net_b or not (layers_a & layers_b):
                continue
            if math.hypot(pos_a[0] - pos_b[0], pos_a[1] - pos_b[1]) < threshold:
                count += 1
    for net_a, pos_a, layers_a in vias:
        for net_b, layer_b, b1, b2 in segments:
            if net_a == net_b or layer_b not in layers_a:
                continue
            if point_segment_distance(pos_a, b1, b2) < threshold:
                count += 1
    return count


# Contract alias: docs/architecture/contracts.md section 6 names the metric
# ``drc_penalty``; it is the same estimator as ``drc_penalty_estimate``.
drc_penalty = drc_penalty_estimate


def summarize(routes: Sequence[dict]) -> dict:
    """Global summary of a routing pass (used by the bench and the smoke tests)."""
    routes = list(routes or [])
    completed = sum(1 for route in routes if bool(route.get("completed")))
    lengths = [float(route.get("length_mm", 0.0) or 0.0) for route in routes]
    total_length = sum(lengths)
    return {
        "nets": len(routes),
        "completed": completed,
        "completion_rate": round(completion_rate(routes), 4),
        "total_length_mm": round(total_length, 4),
        "avg_length_mm": round(total_length / len(routes), 4) if routes else 0.0,
        "total_vias": via_count(routes),
        "drc_violations_est": drc_penalty_estimate(routes),
    }
