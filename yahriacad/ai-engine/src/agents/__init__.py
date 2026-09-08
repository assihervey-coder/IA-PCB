"""RL agents package: abstract interface, PPO, A3C.

Modules:
    base_agent : BaseAgent (abstract) + RolloutBuffer (GAE)
    ppo_agent  : PPOConfig / PPOAgent / PPOTrainer (torch lazy)
    a3c_agent  : A3CConfig / A3CAgent / A3CWorker / A3CTrainer (torch lazy)
"""

__all__ = ["base_agent", "ppo_agent", "a3c_agent"]
