"""Pure-torch graph neural network over the PCB netlist (NO torch_geometric).

``build_graph`` is a numpy-only helper (importable without torch): it turns a
netlist into ``(node_feat, edge_index)`` where nodes are pads and edges connect
pads of the same net (both directions). The torch modules ``GraphConv`` and
``GraphNet`` are built lazily on first use through the module-level
``__getattr__`` (PEP 562), so importing this module never requires torch.

Node features (6, all normalized to [0, 1] when possible):
    0: normalized x position
    1: normalized y position
    2: normalized layer index (layer / max_layer)
    3: onehot-style top-layer flag (1.0 when the pad sits on the last layer)
    4: normalized pad width
    5: normalized pad height
"""

from __future__ import annotations

import numpy as np


def build_graph(nets: list[dict]) -> tuple[np.ndarray, np.ndarray]:
    """Build the pad-level graph of a netlist (numpy only).

    Args:
        nets: list of net dicts, each with a ``pads`` list of dicts carrying
            ``position`` ``{x, y}``, ``layer``, ``width_mm``, ``height_mm``.

    Returns:
        ``(node_feat, edge_index)`` with ``node_feat`` a float32 array of
        shape ``(N, 6)`` and ``edge_index`` an int64 array of shape ``(2, E)``
        (each undirected edge stored in both directions).
    """
    xs: list[float] = []
    ys: list[float] = []
    layers: list[int] = []
    widths: list[float] = []
    heights: list[float] = []
    net_of_node: list[list[int]] = []  # node indices per net

    for net in nets or []:
        node_ids: list[int] = []
        for pad in net.get("pads") or []:
            pos = pad.get("position") or {}
            xs.append(float(pos.get("x", 0.0) or 0.0))
            ys.append(float(pos.get("y", 0.0) or 0.0))
            layers.append(int(pad.get("layer", 0) or 0))
            widths.append(float(pad.get("width_mm", 0.3) or 0.3))
            heights.append(float(pad.get("height_mm", 0.3) or 0.3))
            node_ids.append(len(xs) - 1)
        net_of_node.append(node_ids)

    n = len(xs)
    if n == 0:
        return np.zeros((0, 6), dtype=np.float32), np.zeros((2, 0), dtype=np.int64)

    x_arr = np.asarray(xs, dtype=np.float64)
    y_arr = np.asarray(ys, dtype=np.float64)
    layer_arr = np.asarray(layers, dtype=np.int64)
    w_arr = np.asarray(widths, dtype=np.float64)
    h_arr = np.asarray(heights, dtype=np.float64)

    scale = max(float(np.max(np.abs(x_arr))), float(np.max(np.abs(y_arr))), 1e-6)
    max_layer = max(int(layer_arr.max()), 1)
    max_w = max(float(w_arr.max()), 1e-6)
    max_h = max(float(h_arr.max()), 1e-6)

    feats = np.stack(
        [
            x_arr / scale,
            y_arr / scale,
            layer_arr.astype(np.float64) / float(max_layer),
            (layer_arr == layer_arr.max()).astype(np.float64),  # top-layer flag
            w_arr / max_w,
            h_arr / max_h,
        ],
        axis=1,
    ).astype(np.float32)

    src: list[int] = []
    dst: list[int] = []
    for node_ids in net_of_node:
        for i in range(len(node_ids)):
            for j in range(i + 1, len(node_ids)):
                a, b = node_ids[i], node_ids[j]
                src.extend((a, b))
                dst.extend((b, a))
    edge_index = (
        np.asarray([src, dst], dtype=np.int64)
        if src
        else np.zeros((2, 0), dtype=np.int64)
    )
    return feats, edge_index


# Lazily-built torch classes (see module __getattr__).
_TORCH_CLASSES: dict | None = None


def _torch_classes() -> dict:
    """Build (once) the GraphConv / GraphNet torch classes."""
    global _TORCH_CLASSES
    if _TORCH_CLASSES is None:
        import torch
        import torch.nn.functional as F
        nn = torch.nn

        class GraphConv(nn.Module):
            """Message-passing layer with MEAN aggregation (index_add_).

            out = ReLU( W_self x  +  mean_{j in N(i)} W_msg x_j )
            """

            def __init__(self, in_dim: int, out_dim: int) -> None:
                super().__init__()
                self.linear_msg = nn.Linear(in_dim, out_dim)
                self.linear_self = nn.Linear(in_dim, out_dim)

            def forward(self, x, edge_index):
                src, dst = edge_index[0], edge_index[1]
                messages = self.linear_msg(x)
                n = x.shape[0]
                aggregated = torch.zeros_like(messages)
                aggregated.index_add_(0, dst, messages.index_select(0, src))
                degree = torch.zeros((n, 1), dtype=x.dtype, device=x.device)
                degree.index_add_(
                    0,
                    dst,
                    torch.ones((dst.shape[0], 1), dtype=x.dtype, device=x.device),
                )
                aggregated = aggregated / degree.clamp(min=1.0)
                return F.relu(aggregated + self.linear_self(x))

        class GraphNet(nn.Module):
            """Pad-level GNN: encoder MLP + GraphConv stack + node/graph heads.

            Args:
                in_features: number of node input features (6 with
                    :func:`build_graph`).
                hidden: width of the hidden layers.
                layers: number of message-passing layers (2-3 recommended).
                node_out: output size of the per-node head.
                graph_out: output size of the pooled per-graph head.
            """

            def __init__(
                self,
                in_features: int = 6,
                hidden: int = 128,
                layers: int = 3,
                node_out: int = 1,
                graph_out: int = 2,
            ) -> None:
                super().__init__()
                self.encoder = nn.Sequential(
                    nn.Linear(in_features, hidden), nn.ReLU(), nn.Linear(hidden, hidden), nn.ReLU()
                )
                self.convs = nn.ModuleList(
                    [GraphConv(hidden, hidden) for _ in range(max(2, layers))]
                )
                self.node_head = nn.Linear(hidden, node_out)
                self.graph_head = nn.Sequential(
                    nn.Linear(hidden, hidden), nn.ReLU(), nn.Linear(hidden, graph_out)
                )

            def forward(self, x, edge_index, batch=None):
                h = self.encoder(x)
                for conv in self.convs:
                    h = conv(h, edge_index)
                node_out = self.node_head(h)
                if batch is None:
                    pooled = h.mean(dim=0, keepdim=True)
                else:
                    n_graphs = int(batch.max().item()) + 1
                    pooled = torch.zeros(
                        (n_graphs, h.shape[1]), dtype=h.dtype, device=h.device
                    )
                    count = torch.zeros(
                        (n_graphs, 1), dtype=h.dtype, device=h.device
                    )
                    pooled.index_add_(0, batch, h)
                    count.index_add_(
                        0,
                        batch,
                        torch.ones((h.shape[0], 1), dtype=h.dtype, device=h.device),
                    )
                    pooled = pooled / count.clamp(min=1.0)
                return node_out, self.graph_head(pooled)

        _TORCH_CLASSES = {"GraphConv": GraphConv, "GraphNet": GraphNet}
    return _TORCH_CLASSES


def __getattr__(name: str):
    """PEP 562: expose GraphConv / GraphNet lazily (torch required)."""
    if name in ("GraphConv", "GraphNet"):
        return _torch_classes()[name]
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


def __dir__() -> list[str]:  # pragma: no cover - introspection helper
    return sorted(set(globals()) | {"GraphConv", "GraphNet"})
