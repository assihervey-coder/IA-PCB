"""Routing environment package: grid PCB environment, action space, rewards.

Modules:
    pcb_env      : PCBRouteEnv (gym-like routing env with A* fallback router)
    action_space : ActionSpace (12 discrete actions)
    reward       : RewardConfig / RewardShaper (pure reward functions)
"""

__all__ = ["pcb_env", "action_space", "reward"]
