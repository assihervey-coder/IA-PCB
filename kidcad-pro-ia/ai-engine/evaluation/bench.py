#!/usr/bin/env python3
"""Benchmark CLI: synthetic boards -> routing -> metrics table.

Runs fully without torch (``random`` and ``greedy`` agents). ``ppo`` requires
torch plus a trained checkpoint (falls back to greedy with a warning).

Usage:
    python3 evaluation/bench.py --grids 3 --agent greedy
    python3 evaluation/bench.py --grids 5 --agent random --seed 7 --json out.json
"""

from __future__ import annotations

import argparse
import json
import random
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from src.environment.action_space import ActionSpace  # noqa: E402
from src.environment.pcb_env import EnvConfig, PCBRouteEnv  # noqa: E402

from evaluation.metrics import summarize  # noqa: E402

BOARD_W_MM = 40.0
BOARD_H_MM = 30.0
NET_NAME_POOL = [
    "GND", "VCC", "CLK", "SDA", "SCL", "TX", "RX",
    "MOSI", "MISO", "SCK", "INT", "RESET", "A0", "A1", "D0",
]


def make_synthetic_board(seed: int) -> tuple:
    """Deterministically generate a synthetic board with 4-10 nets.

    Args:
        seed: RNG seed (same seed -> same board).

    Returns:
        ``(board_dict, nets_list)`` with 2 pads per net at random positions
        (rejection-sampled to keep pads apart), random layers, standard
        track width and clearance.
    """
    rng = random.Random(seed)
    board = {
        "width_mm": BOARD_W_MM,
        "height_mm": BOARD_H_MM,
        "layer_count": 2,
        "grid_resolution_mm": 0.25,
        "layer_names": ["F.Cu", "B.Cu"],
    }
    n_nets = rng.randint(4, 10)
    names = rng.sample(NET_NAME_POOL, min(n_nets, len(NET_NAME_POOL)))
    while len(names) < n_nets:
        names.append(f"N{len(names)}")

    margin = 2.0
    placed: list = []
    nets: list = []
    for name in names:
        pads = []
        for pad_idx in range(2):
            for _attempt in range(200):
                x = rng.uniform(margin, BOARD_W_MM - margin)
                y = rng.uniform(margin, BOARD_H_MM - margin)
                layer = rng.randrange(2)
                if all(
                    abs(x - px) > 3.0 or abs(y - py) > 3.0
                    for px, py in placed
                ):
                    placed.append((x, y))
                    pads.append(
                        {
                            "component_ref": f"U{len(placed)}",
                            "pad_name": str(pad_idx + 1),
                            "position": {"x": round(x, 3), "y": round(y, 3)},
                            "layer": layer,
                            "width_mm": 0.6,
                            "height_mm": 0.6,
                        }
                    )
                    break
        power = name in ("GND", "VCC")
        nets.append(
            {
                "name": name,
                "net_class": "power" if power else "default",
                "pads": pads,
                "min_track_width_mm": 0.4 if power else 0.25,
                "clearance_mm": 0.2,
            }
        )
    return board, nets


def random_rollout(env: PCBRouteEnv, net_index: int, rng: random.Random) -> dict:
    """Route one net with uniformly random actions (baseline agent)."""
    space = ActionSpace()
    obs = env.reset(net_index)
    info: dict = {}
    for _ in range(env.max_steps):
        action = space.sample(rng)
        obs, _reward, terminated, truncated, info = env.step(action)
        if terminated or truncated:
            break
    if info.get("success"):
        return env.path_to_route(net_index, env.episode_path)
    return {
        "net": env.net_names[net_index],
        "segments": [],
        "vias": [],
        "length_mm": 0.0,
        "completed": False,
    }


def ppo_rollout(env: PCBRouteEnv, net_index: int, agent) -> dict:
    """Route one net with a greedy PPO rollout (torch required)."""
    obs = env.reset(net_index)
    info: dict = {}
    for _ in range(env.max_steps):
        action = agent.select_action(obs, greedy=True)
        obs, _reward, terminated, truncated, info = env.step(action)
        if terminated or truncated:
            break
    if info.get("success"):
        return env.path_to_route(net_index, env.episode_path)
    return {
        "net": env.net_names[net_index],
        "segments": [],
        "vias": [],
        "length_mm": 0.0,
        "completed": False,
    }


def load_ppo_agent(model_path: str, in_channels: int):
    """Load a PPO checkpoint; raises when torch is missing."""
    from src.agents.ppo_agent import PPOAgent, PPOConfig

    agent = PPOAgent(PPOConfig(), in_channels=in_channels, n_actions=ActionSpace().n)
    if not agent.load(model_path):
        raise FileNotFoundError(f"unreadable PPO checkpoint: {model_path}")
    return agent


def run_bench(grids: int, seed: int, agent_name: str, model_path: str | None) -> dict:
    """Run the benchmark over ``grids`` synthetic boards."""
    ppo_agent = None
    if agent_name == "ppo":
        try:
            ppo_agent = load_ppo_agent(model_path or "training/router/model_v1.pt", 5)
        except Exception as exc:
            print(f"[warn] PPO indisponible ({exc}) -> repli greedy", flush=True)
            agent_name = "greedy"

    report: dict = {
        "agent": agent_name,
        "seed": seed,
        "boards": [],
        "global": {},
    }
    all_routes: list = []
    started = time.monotonic()

    for board_idx in range(grids):
        board_seed = seed * 1000 + board_idx
        board, nets = make_synthetic_board(board_seed)
        env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=board_seed))
        rng = random.Random(board_seed + 999)
        routes = []
        for net_index in range(env.n_nets):
            if agent_name == "greedy":
                route = env.astar_route(net_index)
            elif agent_name == "ppo" and ppo_agent is not None:
                route = ppo_rollout(env, net_index, ppo_agent)
                if not route.get("completed"):
                    route = env.astar_route(net_index)
            else:
                route = random_rollout(env, net_index, rng)
            routes.append(route)
        all_routes.extend(routes)
        summary = summarize(routes)
        report["boards"].append(
            {
                "seed": board_seed,
                "nets_total": env.n_nets,
                "routes": [
                    {
                        "net": route.get("net", ""),
                        "length_mm": route.get("length_mm", 0.0),
                        "vias": len(route.get("vias") or []),
                        "completed": bool(route.get("completed")),
                    }
                    for route in routes
                ],
                "summary": summary,
            }
        )

    report["global"] = summarize(all_routes)
    # DRC estimate must not be computed ACROSS boards (different synthetic
    # boards share coordinates): use the sum of per-board estimates instead.
    report["global"]["drc_violations_est"] = sum(
        board["summary"]["drc_violations_est"] for board in report["boards"]
    )
    report["elapsed_s"] = round(time.monotonic() - started, 3)
    return report


def print_report(report: dict) -> None:
    """ASCII table + global summary (stdout)."""
    print("=" * 64)
    print(
        f" KidCAD-Pro-IA benchmark - agent={report['agent']}"
        f" - {len(report['boards'])} board(s) - seed={report['seed']}"
    )
    print("=" * 64)
    for board in report["boards"]:
        print(f"\nboard (seed {board['seed']}) - {board['nets_total']} nets")
        print(f"  {'net':<10} {'length_mm':>10} {'vias':>6} {'completed':>10}")
        print("  " + "-" * 42)
        for row in board["routes"]:
            print(
                f"  {row['net']:<10} {row['length_mm']:>10.2f}"
                f" {row['vias']:>6} {str(row['completed']).lower():>10}"
            )
        s = board["summary"]
        print(
            f"  -> {s['completed']}/{s['nets']} nets"
            f" ({100 * s['completion_rate']:.1f}%),"
            f" {s['total_length_mm']:.1f} mm, {s['total_vias']} vias,"
            f" DRC est. {s['drc_violations_est']}"
        )
    g = report["global"]
    print("\n" + "=" * 64)
    print(
        f" GLOBAL : {g['completed']}/{g['nets']} nets"
        f" ({100 * g['completion_rate']:.1f}%) |"
        f" longueur {g['total_length_mm']:.1f} mm |"
        f" vias {g['total_vias']} | DRC est. {g['drc_violations_est']}"
        f" | {report['elapsed_s']} s"
    )
    print("=" * 64)


def main(argv=None) -> int:
    """CLI entrypoint."""
    parser = argparse.ArgumentParser(description="KidCAD-Pro-IA routing benchmark")
    parser.add_argument("--grids", type=int, default=3, help="number of synthetic boards")
    parser.add_argument("--seed", type=int, default=42, help="base RNG seed")
    parser.add_argument("--agent", type=str, default="greedy", choices=["random", "greedy", "ppo"])
    parser.add_argument("--model", type=str, default=None, help="PPO checkpoint (.pt) for --agent ppo")
    parser.add_argument("--json", type=str, default=None, help="write the JSON report to this file")
    args = parser.parse_args(argv)

    report = run_bench(args.grids, args.seed, args.agent, args.model)
    print_report(report)
    if args.json:
        out_path = Path(args.json)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        with open(out_path, "w", encoding="utf-8") as handle:
            json.dump(report, handle, indent=2)
        print(f"rapport JSON ecrit : {out_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
