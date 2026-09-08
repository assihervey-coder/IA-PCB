"""GNN léger (PyTorch) — message passing sur le graphe des nets PCB.

Graphe : nœuds = pads/nets, arêtes = connexions borne-à-borne (netlist).
Objectif R&D : produire un embedding de chaque net (densité, longueur estimée,
criticalité) pouvant guider l'ordre de routage ou le choix de couche du moteur
TS (voir ADR-002). Non requis en production.

Référence : Gilmer et al., "Neural Message Passing for Quantum Chemistry", 2017.
"""

from __future__ import annotations

from typing import List, Tuple

import torch
import torch.nn as nn
import torch.nn.functional as F

NodeFeatures = torch.Tensor        # (num_nodes, in_dim)
EdgeIndex = torch.Tensor           # (2, num_edges) — COO, dtype long
EdgeAttr = torch.Tensor            # (num_edges, edge_dim)


class MessagePassingLayer(nn.Module):
    """Une couche de message passing : agrégation moyenne des voisins + MLP."""

    def __init__(self, dim: int, edge_dim: int = 4) -> None:
        super().__init__()
        self.message_mlp = nn.Sequential(
            nn.Linear(dim + edge_dim, dim),
            nn.ReLU(),
            nn.Linear(dim, dim),
        )
        self.update_gru = nn.GRUCell(dim, dim)
        self.norm = nn.LayerNorm(dim)

    def forward(self, x: torch.Tensor, edge_index: EdgeIndex, edge_attr: EdgeAttr) -> torch.Tensor:
        src, dst = edge_index[0], edge_index[1]
        # Messages = MLP([h_src, attr_edge])
        msgs = self.message_mlp(torch.cat([x[src], edge_attr], dim=-1))
        # Agrégation moyenne par nœud destination (scatter_mean)
        agg = torch.zeros_like(x)
        count = torch.zeros(x.size(0), 1, device=x.device, dtype=x.dtype)
        agg.index_add_(0, dst, msgs)
        count.index_add_(0, dst, torch.ones(msgs.size(0), 1, device=x.device))
        agg = agg / count.clamp(min=1.0)
        # Mise à jour type GRU + résidu normalisé
        updated = self.update_gru(agg, x)
        return self.norm(x + updated)


class NetGraphNet(nn.Module):
    """GNN de nets : k couches de message passing + tête d'embedding.

    Entrées :
        x          : (num_nodes, in_dim)  features de nœuds (pads/nets)
        edge_index : (2, num_edges)       arêtes (connexions de nets)
        edge_attr  : (num_edges, edge_dim) features d'arêtes (longueur, couche…)

    Sorties :
        node_embeddings (num_nodes, out_dim) et score de criticalité par net.
    """

    def __init__(
        self,
        in_dim: int = 8,
        hidden_dim: int = 64,
        out_dim: int = 32,
        num_layers: int = 3,
        edge_dim: int = 4,
    ) -> None:
        super().__init__()
        self.input_proj = nn.Linear(in_dim, hidden_dim)
        self.layers = nn.ModuleList(
            [MessagePassingLayer(hidden_dim, edge_dim=edge_dim) for _ in range(num_layers)]
        )
        self.output_proj = nn.Linear(hidden_dim, out_dim)
        # Tête auxiliaire : score de criticalité de routage par nœud/net
        self.criticality_head = nn.Sequential(
            nn.Linear(out_dim, out_dim // 2),
            nn.ReLU(),
            nn.Linear(out_dim // 2, 1),
        )

    def forward(
        self,
        x: NodeFeatures,
        edge_index: EdgeIndex,
        edge_attr: EdgeAttr,
    ) -> Tuple[torch.Tensor, torch.Tensor]:
        """Retourne (embeddings de nœuds, scores de criticalité)."""
        h = F.relu(self.input_proj(x))
        for layer in self.layers:
            h = layer(h, edge_index, edge_attr)
        embeddings = self.output_proj(h)
        criticality = self.criticality_head(embeddings).squeeze(-1)
        return embeddings, criticality

    @staticmethod
    def build_graph(
        pads: List[dict],
        nets: List[dict],
    ) -> Tuple[NodeFeatures, EdgeIndex, EdgeAttr]:
        """Construit le graphe à partir d'une netlist YahriaCad parsée.

        Args:
            pads: liste de dicts {ref, pin, net, x, y, layer}.
            nets: liste de dicts {name, nodes: [{ref, pin}, ...]}.

        Returns:
            (x, edge_index, edge_attr) prêts pour ``forward``.
        """
        pad_index = {f"{p['ref']}.{p['pin']}": i for i, p in enumerate(pads)}
        features = torch.zeros(len(pads), 8)
        edges_src: List[int] = []
        edges_dst: List[int] = []
        edge_attrs: List[List[float]] = []

        for i, p in enumerate(pads):
            features[i, 0] = p.get("x", 0.0) / 100.0          # position normalisée
            features[i, 1] = p.get("y", 0.0) / 100.0
            features[i, 2] = float(p.get("layer", 0))          # couche
            features[i, 3] = 1.0 if p.get("net") in ("VCC", "GND") else 0.0  # alim

        for net in nets:
            nodes = net.get("nodes", [])
            for a, b in zip(nodes, nodes[1:]):
                ia = pad_index.get(f"{a['ref']}.{a['pin']}")
                ib = pad_index.get(f"{b['ref']}.{b['pin']}")
                if ia is None or ib is None:
                    continue
                pa, pb = pads[ia], pads[ib]
                # Arête bidirectionnelle avec features simples
                length = abs(pa.get("x", 0) - pb.get("x", 0)) + abs(pa.get("y", 0) - pb.get("y", 0))
                for (u, v) in ((ia, ib), (ib, ia)):
                    edges_src.append(u)
                    edges_dst.append(v)
                    edge_attrs.append([
                        length / 100.0,
                        float(pa.get("layer", 0) != pb.get("layer", 0)),
                        1.0 if net["name"] in ("VCC", "GND") else 0.0,
                        0.0,
                    ])

        edge_index = torch.tensor([edges_src, edges_dst], dtype=torch.long).reshape(2, -1)
        edge_attr = torch.tensor(edge_attrs, dtype=torch.float32).reshape(-1, 4)
        return features, edge_index, edge_attr


if __name__ == "__main__":  # petit test de fumée
    pads = [
        {"ref": "U1", "pin": "3", "net": "CLK", "x": 10, "y": 5, "layer": 0},
        {"ref": "U2", "pin": "14", "net": "CLK", "x": 25, "y": 5, "layer": 0},
        {"ref": "J1", "pin": "1", "net": "VCC", "x": 2, "y": 2, "layer": 0},
    ]
    nets = [
        {"name": "CLK", "nodes": [{"ref": "U1", "pin": "3"}, {"ref": "U2", "pin": "14"}]},
        {"name": "VCC", "nodes": [{"ref": "J1", "pin": "1"}]},
    ]
    x, ei, ea = NetGraphNet.build_graph(pads, nets)
    model = NetGraphNet()
    emb, crit = model(x, ei, ea)
    print("embeddings:", tuple(emb.shape), "criticality:", tuple(crit.shape))
