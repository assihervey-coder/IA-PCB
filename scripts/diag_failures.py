#!/usr/bin/env python3
"""Diagnostic Task 23 : structure des échecs one-shot du champion 4L.

Hypothèse : les 97 nets échoués le sont pour une raison indépendante de
l'archi (cap 300 trop court pour les nets longs ? congestion ?). On mesure
pour chaque net la distance Manhattan source->cible (cellules) et le statut
one-shot du champion, puis on binne par distance.
"""
import json
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import torch  # noqa: E402
torch.set_num_threads(2)
from evaluation.bench import make_realistic_board, load_ppo_agent, ppo_rollout  # noqa: E402
from src.environment.pcb_env import PCBRouteEnv, EnvConfig  # noqa: E402

ROLLOUT_CAP = 300
model = sys.argv[1] if len(sys.argv) > 1 else str(ROOT / "training/router/model_bc_4l.pt")

agent = load_ppo_agent(model, in_channels=8)
rows = []
for seed in (46000, 46001):
    board, nets = make_realistic_board(seed, layer_count=4)
    env = PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=seed))
    env.max_steps = min(int(env.max_steps), ROLLOUT_CAP)
    for net_index in range(env.n_nets):
        ep = env.reset(net_index)
        start = env._episode.start
        targets = env._episode.targets
        dist = min(abs(start[0] - t[0]) + abs(start[1] - t[1]) for t in targets)
        layers_used = len({t[2] for t in targets} | {start[2]})
        route = ppo_rollout(env, net_index, agent)
        rows.append({
            "seed": seed, "net": net_index, "dist": int(dist),
            "cross_layer": layers_used > 1, "ok": bool(route.get("completed")),
            "steps": int(route.get("steps") or 0),
        })

ok_by_bin = {}
for r in rows:
    b = (r["dist"] // 20) * 20
    d = ok_by_bin.setdefault(b, [0, 0])
    d[0] += int(r["ok"])
    d[1] += 1
print("== Taux one-shot par bin de distance Manhattan (20 cellules) ==")
for b in sorted(ok_by_bin):
    ok, n = ok_by_bin[b]
    print(f"  dist {b:>3d}-{b + 19:<3d}: {ok:>2d}/{n:<2d} = {100.0 * ok / n:5.1f} %")

ok_cross = [r for r in rows if r["cross_layer"]]
ok_same = [r for r in rows if not r["cross_layer"]]
print("== Croisement de couche (via a priori nécessaire) ==")
print(f"  multi-couches : {sum(r['ok'] for r in ok_cross)}/{len(ok_cross)} = "
      f"{100.0 * sum(r['ok'] for r in ok_cross) / max(1, len(ok_cross)):.1f} %")
print(f"  mono-couche   : {sum(r['ok'] for r in ok_same)}/{len(ok_same)} = "
      f"{100.0 * sum(r['ok'] for r in ok_same) / max(1, len(ok_same)):.1f} %")

fails = [r for r in rows if not r["ok"]]
oks = [r for r in rows if r["ok"]]
import statistics as st  # noqa: E402
print("== Distance moyenne ==")
print(f"  réussis : {st.mean(r['dist'] for r in oks):.1f} cells ; "
      f"échoués : {st.mean(r['dist'] for r in fails):.1f} cells")

# Réussis à quel point ils brûlent le cap ?
if oks:
    ratios = [r["steps"] / max(1, r["dist"]) for r in oks]
    print(f"== Pas utilisés / dist minimale (réussis) : médiane {st.median(ratios):.2f}, "
          f"max {max(ratios):.2f} ==")
long_ok = [r for r in oks if r["steps"] >= 280]
print(f"  réussis avec >= 280 pas (proches du cap 300) : {len(long_ok)}")

print(json.dumps({"bins": {str(b): ok_by_bin[b] for b in sorted(ok_by_bin)},
                  "total_ok": len(oks), "total": len(rows)}))
