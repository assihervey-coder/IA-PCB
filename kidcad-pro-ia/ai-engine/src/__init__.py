"""KidCAD-Pro-IA AI engine (Python).

gRPC microservice providing PCB placement planning, routing and route
optimization. The package is deliberately torch-free at import time: all
PyTorch-dependent code is imported lazily inside functions/classes so the
service runs end-to-end with the deterministic A* fallback.

Sub-packages:
    environment : grid-based routing environment (gym-like) + A* fallback
    agents      : RL agents (PPO, A3C) and the abstract agent interface
    models      : pure-torch neural network models (GNN, ViT)
"""

__all__ = ["service", "environment", "agents", "models"]
