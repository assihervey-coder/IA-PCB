"""Neural models package: GNN and board transformer (pure torch, lazy import).

Modules:
    graph_net   : GraphConv / GraphNet + build_graph (numpy, torch-free)
    transformer : BoardViT (patch embedding + TransformerEncoder)
"""

__all__ = ["graph_net", "transformer"]
