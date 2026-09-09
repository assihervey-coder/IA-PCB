#!/usr/bin/env python3
"""Extrait des micro-démos « zones via » d'un npz de démos téléportées.

Chaque étiquette via (action % 3 != 0) dans les démos #tp définit une
micro-démo de 2k+1 états consécutifs (voisins dans la liste du pas —
les états de corridor y sont ordonnés le long du leg) centrée sur le via.
Ces micro-démos, fusionnées avec un fort upsampling, redonnent du poids
de gradient à la décision de via que le BC sous-apprend (0.00 constaté).

Usage : python3 extract_via_demos.py <in.npz> <out.npz> [rayon=3]
"""
import sys
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad/ai-engine")
sys.path.insert(0, str(ROOT))

import numpy as np  # noqa: E402

from training.router.imitation import load_demos, save_demos  # noqa: E402


def main() -> int:
    src = Path(sys.argv[1] if len(sys.argv) > 1 else
               ROOT / "training/router/demos_tp.npz")
    dst = Path(sys.argv[2] if len(sys.argv) > 2 else
               ROOT / "training/router/demos_tp_via.npz")
    radius = int(sys.argv[3]) if len(sys.argv) > 3 else 3

    data = load_demos(src)
    meta, planes = data["meta"], data["planes"]
    steps, actions, nets = data["steps"], data["actions"], data["nets"]

    micro: list[dict] = []
    for row in range(len(meta)):
        h, w, layers, step_off, n, plane_off, plane_bytes = (
            int(v) for v in meta[row]
        )
        if n == 0:
            continue
        bits = np.unpackbits(planes[plane_off: plane_off + plane_bytes])
        static = bits[: (layers + 2) * h * w].reshape(layers + 2, h, w)
        planes_arr = np.packbits(np.ascontiguousarray(static))
        demo_steps = [tuple(int(v) for v in r)
                      for r in steps[step_off: step_off + n]]
        demo_actions = [int(a) for a in actions[step_off: step_off + n]]
        name = str(nets[row])
        for i, a in enumerate(demo_actions):
            if a % 3 == 0:
                continue  # pas un via
            lo, hi = max(0, i - radius), min(n, i + radius + 1)
            micro.append({
                "net": f"{name}#vz{i}",
                "layers": layers, "h": h, "w": w, "planes": planes_arr,
                "steps": demo_steps[lo:hi], "actions": demo_actions[lo:hi],
            })
    save_demos(micro, dst)
    states = sum(len(d["actions"]) for d in micro)
    print(f"extract-via: {len(micro)} micro-démos / {states} états "
          f"-> {dst}", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
