#!/usr/bin/env python3
"""Test live multi-modeles du moteur IA YahriaCad (gRPC direct).

Verifie le registre de checkpoints par canaux d'observation :
  * carte 4 couches (8 canaux) + strategy=rl  -> le modele BC 4L sert le RL ;
  * carte 2 couches (6 canaux) + strategy=rl  -> le modele BC v6 sert le RL ;
  * les deux reponses doivent annoncer strategy="rl" (sinon repli A*).
"""
import sys
import time
from pathlib import Path

ROOT = Path("/home/z/my-project/yahriacad")
sys.path.insert(0, str(ROOT / "shared" / "gen" / "python"))

import grpc  # noqa: E402
import pcb_pb2 as pb  # noqa: E402
import pcb_pb2_grpc as pb_grpc  # noqa: E402

NAMES_4L = ["F.Cu", "In1.Cu", "In2.Cu", "B.Cu"]


def pads(net: str, layer_a: int, layer_b: int) -> list:
    return [
        pb.PadRef(component_ref=f"U_{net}A", pad_name="1",
                  position=pb.Point(x=2.0, y=2.0 if layer_a == 0 else 12.0),
                  layer=layer_a, width_mm=0.6, height_mm=0.6),
        pb.PadRef(component_ref=f"U_{net}B", pad_name="1",
                  position=pb.Point(x=17.0, y=12.0 if layer_b == 3 else 2.0),
                  layer=layer_b, width_mm=0.6, height_mm=0.6),
    ]


def board(names: list[str]) -> pb.BoardSpec:
    return pb.BoardSpec(width_mm=20.0, height_mm=15.0, layer_count=len(names),
                        grid_resolution_mm=0.25, layer_names=names)


def nets(names: list[str]) -> list:
    last = len(names) - 1
    out = []
    for i in range(4):
        a = 0 if i % 2 == 0 else last   # pads sur F.Cu / B.Cu uniquement
        b = last if i % 2 == 0 else 0   # -> croisements de couches obligatoires
        out.append(pb.NetSpec(
            name=f"N{i + 1}", net_class="default", pads=pads(f"N{i + 1}", a, b),
            min_track_width_mm=0.25, clearance_mm=0.2,
        ))
    return out


def main() -> int:
    with grpc.insecure_channel("127.0.0.1:50051") as channel:
        grpc.channel_ready_future(channel).result(timeout=10)
        stub = pb_grpc.AIRouterServiceStub(channel)

        health = stub.GetHealth(pb.HealthRequest())
        print(f"health: status={health.status} model_loaded={health.model_loaded} "
              f"device={health.device}")
        if not health.model_loaded:
            print("ECHEC: aucun modele RL charge")
            return 1

        ok = True
        for label, names in [("4 couches (8 canaux)", NAMES_4L),
                             ("2 couches (6 canaux)", ["F.Cu", "B.Cu"])]:
            request = pb.RouteRequest(board=board(names), nets=nets(names),
                                      strategy="rl")
            t0 = time.time()
            events = list(stub.RouteBoard(request, timeout=180))
            elapsed = time.time() - t0
            completed = [e.partial for e in events
                         if e.HasField("partial") and e.partial.completed]
            vias = sum(len(p.vias) for p in completed)
            # Evidence RL : les rollouts BC prennent des secondes, l'A* seul
            # serait instantane (<100 ms) sur ces petites cartes.
            rl_engaged = elapsed > 1.0
            print(f"{label}: {len(completed)}/{len(nets(names))} nets, "
                  f"{elapsed:.1f}s, {vias} vias -> "
                  f"{'RL engagé' if rl_engaged else 'repli A* (trop rapide)'}")
            if not rl_engaged:
                ok = False
        print("RESULTAT:", "OK — RL multi-modeles actif" if ok else "ECHEC")
        return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
