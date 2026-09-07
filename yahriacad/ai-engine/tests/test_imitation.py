"""Tests du pipeline d'imitation learning (behavioral cloning de A*)."""

from __future__ import annotations

import sys
from pathlib import Path

import numpy as np
import pytest

AI_ENGINE_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = AI_ENGINE_ROOT.parents[0]
for _path in (str(AI_ENGINE_ROOT), str(REPO_ROOT / "shared" / "gen" / "python")):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from evaluation.bench import make_synthetic_board  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv, position_plane  # noqa: E402
from training.router.imitation import (  # noqa: E402
    _route_order,
    build_torch_dataset,
    collect_demos,
    encode_expert_action,
    load_demos,
    save_demos,
)


def test_encode_expert_action() -> None:
    """Via pur -> actions 1/2, deplacements -> 0/3/6/9, mixte -> None."""
    assert encode_expert_action(0, -1, 0) == 0  # up
    assert encode_expert_action(1, 0, 0) == 3  # right
    assert encode_expert_action(0, 1, 0) == 6  # down
    assert encode_expert_action(-1, 0, 0) == 9  # left
    assert encode_expert_action(0, 0, 1) == 1  # via-up
    assert encode_expert_action(0, 0, -1) == 2  # via-down
    assert encode_expert_action(1, 1, 0) is None  # diagonale : pas d'action
    assert encode_expert_action(1, 0, 1) is None  # move+via : pas d'action


def test_collect_demos_and_replay() -> None:
    """Les demonstrations rejouees reproduisent des episodes gagnants.

    Rejouer les actions expertes sur un environnement reconstruit dans le
    meme ordre de routage (meme progression d'obstacles) doit terminer
    chaque episode en succes, et l'observation reconstruite depuis la demo
    doit etre bit-exacte avec l'observation reelle de l'environnement.
    """
    board, nets = make_synthetic_board(42000)
    demos = collect_demos(board, nets)
    assert demos, "aucune demonstration collectee sur une carte synthetique"
    assert all(d["layers"] == 2 for d in demos)

    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
    demo_by_name = {d["net"]: d for d in demos}
    checked = 0
    for net_index in _route_order(env):
        # astar_route pour TOUS les nets (meme sans demo) : la progression
        # d'obstacles reproduit exactement celle de la collecte.
        env.astar_route(net_index)
        demo = demo_by_name.get(env.net_names[net_index])
        if demo is None or demo["net"].endswith("#probe"):
            continue  # net saute par la collecte, ou sondes (non rejouables)
        obs = env.reset(net_index)
        info: dict = {}
        for k, action in enumerate(demo["actions"]):
            x, y, layer = demo["steps"][k]
            assert (env._episode.x, env._episode.y, env._episode.layer) == (x, y, layer)
            # observation reelle avant l'action == reconstruction depuis la demo
            np.testing.assert_array_equal(_reconstruct_from_demo(demo, k), obs)
            obs, _r, terminated, truncated, info = env.step(int(action))
            assert not truncated, f"rejeu de {demo['net']} tronque au pas {k}"
            if terminated:
                break
        assert info.get("success"), f"rejeu de {demo['net']} non concluant"
        checked += 1
    assert checked >= max(1, len(demos) // 2)


def _reconstruct_from_demo(demo: dict, step_index: int) -> np.ndarray:
    """Reconstruction minimale (miroir du Dataset) pour la verification."""
    layers, h, w = demo["layers"], demo["h"], demo["w"]
    bits = np.unpackbits(np.asarray(demo["planes"], dtype=np.uint8))
    static = bits[: (layers + 2) * h * w].reshape(layers + 2, h, w)
    x, y, layer = demo["steps"][step_index]
    obs = np.zeros((layers + 4, h, w), dtype=np.float32)
    obs[:layers] = static[:layers]
    obs[layers] = static[layers]
    obs[layers + 1] = static[layers + 1]
    obs[layers + 2, :, :] = layer / max(1, layers - 1)
    obs[layers + 3] = position_plane(h, w, x, y)
    return obs


def test_save_load_roundtrip(tmp_path: Path) -> None:
    """Le npz preserve les plans, pas et actions (sans pickle)."""
    board, nets = make_synthetic_board(42001)
    demos = collect_demos(board, nets, max_nets=3)
    path = tmp_path / "demos.npz"
    save_demos(demos, path)
    data = load_demos(path)
    assert data["meta"].shape[1] == 7
    assert len(data["nets"]) == len(demos)
    total = sum(int(row[4]) for row in data["meta"])
    assert total == len(data["actions"]) == len(data["steps"])
    # reconstruction depuis le npz == reconstruction depuis la demo en memoire
    dataset = build_torch_dataset(data)
    assert len(dataset) == total
    obs, action = dataset[0]
    demo0 = demos[0]
    assert obs.shape == (demo0["layers"] + 4, demo0["h"], demo0["w"])
    np.testing.assert_array_equal(obs, _reconstruct_from_demo(demo0, 0))
    assert action == int(demo0["actions"][0])


def test_train_smoke(tmp_path: Path) -> None:
    """Entrainement micro : checkpoint sauve et rechargable par PPOAgent."""
    pytest.importorskip("torch")
    from src.agents.ppo_agent import PPOAgent, PPOConfig
    from training.router.imitation import train_bc

    board, nets = make_synthetic_board(42002)
    demos = collect_demos(board, nets)
    assert demos
    path = tmp_path / "demos.npz"
    save_demos(demos, path)

    out = tmp_path / "model_bc.pt"
    stats = train_bc(str(path), str(out), epochs=1, batch_size=8, lr=1e-3)
    assert out.is_file()
    assert stats["samples"] > 0
    assert stats["accuracy"] > 0.5, "l'accuracy doit depasser 50% des la 1re epoch"

    agent = PPOAgent(PPOConfig(), in_channels=6, n_actions=12, device="cpu")
    assert agent.load(str(out)), "checkpoint BC illisible par PPOAgent"
    # Propriete structurelle du masquage : un rollout glouton ne peut plus
    # percuter un mur — chaque pas ajoute exactement une cellule au chemin
    # (deplacements ET vias), donc path_len == steps + 1 a chaque instant.
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))
    for net_index in range(min(4, env.n_nets)):
        obs = env.reset(net_index)
        for step in range(40):
            action = agent.select_action(obs, greedy=True)
            obs, _r, terminated, truncated, info = env.step(action)
            assert len(env.episode_path) == step + 2, (
                "collision detectee : le masquage d'actions est casse"
            )
            if terminated or truncated:
                break
