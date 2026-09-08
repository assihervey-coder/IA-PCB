#!/usr/bin/env python3
"""Hot-reload du checkpoint RL du moteur IA YahriaCad (gRPC ReloadModel).

Usage :
    python3 reload_rl_model.py [checkpoint_path]

Sans argument : recharge le model_path configure (config.yaml router.model_path).
Retourne 0 si loaded=true, 1 sinon.
"""
import sys
from pathlib import Path

import grpc

REPO = Path("/home/z/my-project/yahriacad")
sys.path.insert(0, str(REPO / "shared" / "gen" / "python"))

import pcb_pb2 as pb  # noqa: E402
import pcb_pb2_grpc as pb_grpc  # noqa: E402


def main() -> int:
    target = "127.0.0.1:50051"
    with grpc.insecure_channel(target) as channel:
        grpc.channel_ready_future(channel).result(timeout=10)
        stub = pb_grpc.AIRouterServiceStub(channel)
        req = pb.ReloadModelRequest(
            checkpoint_path=sys.argv[1] if len(sys.argv) > 1 else ""
        )
        resp = stub.ReloadModel(req, timeout=120)
        info = resp.info
        print(f"loaded   : {resp.loaded}")
        print(f"message  : {resp.message}")
        if info and getattr(info, "checkpoint_path", ""):
            print(f"checkpoint: {info.checkpoint_path}")
            print(f"params   : {getattr(info, 'param_count', '?')}")
        return 0 if resp.loaded else 1


if __name__ == "__main__":
    sys.exit(main())
