#!/usr/bin/env python3
"""Sanité Task 23 : architectures base/compass/wide + transfert + round-trip."""
import os
import sys

sys.path.insert(0, "/home/z/my-project/yahriacad/ai-engine")
import torch  # noqa: E402

torch.set_num_threads(2)
from src.agents.ppo_agent import PPOAgent, PPOConfig  # noqa: E402

CKPT = "/home/z/my-project/yahriacad/ai-engine/training/router/model_bc_4l.pt"
TMP = "/home/z/my-project/scripts/tmp_sanity_compass.pt"
H, W = 300, 400

obs = torch.zeros(1, 8, H, W)
obs[0, 0, 150, 190:210] = 1.0          # obstacle ligne sur couche 0
obs[0, 4, 100, 200] = 1.0              # source
obs[0, 5, 250, 350] = 1.0              # cible
obs[0, 6, :, :] = 0.0                  # indicateur couche 0 (L=4 -> 0.0)
obs[0, 7, 100, 200] = 1.0              # agent

print("== 1. Construction + forward par archi (obs 8 canaux, 300x400) ==")
n_params = {}
for arch in ("base", "compass", "wide"):
    agent = PPOAgent(PPOConfig(), in_channels=8, n_actions=12, device="cpu", arch=arch)
    n_params[arch] = sum(p.numel() for p in agent.net.parameters())
    with torch.no_grad():
        logits, value = agent.net(obs)
    assert tuple(logits.shape) == (1, 12), logits.shape
    finite = bool(torch.isfinite(logits).all())
    greedy = int(logits.argmax(dim=-1).item())
    print(f"  {arch:8s} params={n_params[arch]:>9,} finite={finite} "
          f"greedy={greedy} value={float(value):.3f}")
    assert finite

print("== 2. Transfert partiel c3 (base) -> compass / wide ==")
for arch in ("compass", "wide"):
    agent = PPOAgent(PPOConfig(), in_channels=8, n_actions=12, device="cpu", arch=arch)
    ok = agent.load(CKPT, follow_arch=False)
    with torch.no_grad():
        logits, _ = agent.net(obs)
    print(f"  {arch:8s} load={ok} finite={bool(torch.isfinite(logits).all())}")
    assert ok

print("== 3. Rétro-compat service : base construit, checkpoint base chargé (follow) ==")
agent = PPOAgent(PPOConfig(), in_channels=8, n_actions=12, device="cpu")
ok = agent.load(CKPT)
print(f"  load={ok} arch finale={agent.arch} (attendu base, strict)")
assert ok and agent.arch == "base"

print("== 4. Round-trip compass : save -> PPOAgent base -> load follow (simule service/eval) ==")
agent_c = PPOAgent(PPOConfig(), in_channels=8, n_actions=12, device="cpu", arch="compass")
assert agent_c.load(CKPT, follow_arch=False)
assert agent_c.save(TMP), "save compass échoué"
agent_s = PPOAgent(PPOConfig(), in_channels=8, n_actions=12, device="cpu")  # base par défaut
ok = agent_s.load(TMP)
with torch.no_grad():
    lg1, v1 = agent_c.net(obs)
    lg2, v2 = agent_s.net(obs)
same = bool(torch.allclose(lg1, lg2, atol=1e-5)) and abs(float(v1) - float(v2)) < 1e-5
print(f"  load={ok} arch finale={agent_s.arch} (attendu compass) logits_identiques={same}")
assert ok and agent_s.arch == "compass" and same
os.remove(TMP)

print("== 5. Boussole : orientation du plan vs cible ==")
agent = PPOAgent(PPOConfig(), in_channels=8, n_actions=12, device="cpu", arch="compass")
plane = agent.net._target_compass(obs, 4, 38, 50)
print(f"  plan {tuple(plane.shape)} norme_max={float(plane.norm(dim=1).max()):.3f}")
# agent (100,200) -> cible (250,350) : direction attendue (+1, +1)/sqrt(2) (y vers le bas, x vers la droite)
gy, gx = 100 // 8, 200 // 8
dx, dy = float(plane[0, 0, gy, gx]), float(plane[0, 1, gy, gx])
print(f"  a la cellule agent (gy={gy},gx={gx}) : dx={dx:+.3f} dy={dy:+.3f} (attendu ~+0.707/+0.707)")
assert dx > 0.6 and dy > 0.6

print("== 6. Chrono forward (inferérence rollout, B=1) ==")
import time  # noqa: E402
for arch in ("base", "compass", "wide"):
    agent = PPOAgent(PPOConfig(), in_channels=8, n_actions=12, device="cpu", arch=arch)
    with torch.no_grad():
        agent.net(obs)  # warmup
        t0 = time.monotonic()
        for _ in range(30):
            agent.net(obs)
        dt = (time.monotonic() - t0) / 30 * 1000
    print(f"  {arch:8s} {dt:.1f} ms/forward")

print("SANITY_OK")
