#!/usr/bin/env python3
"""gRPC end-to-end smoke test (real server subprocess, no torch required).

Scenario:
1. spawn ``python3 cmd/ai-server/main.py --port 50077`` as a subprocess;
2. wait for the port to accept connections;
3. ``GetHealth`` must answer (``ok`` or ``degraded`` without a model);
4. ``PlanPlacement`` on a small 4-component board must return 4 placements;
5. ``RouteBoard`` on a 2-net board must stream per-net events and finish with
   ``done=True`` and both nets completed;
6. ``OptimizeRoutes`` over the streamed results must end with ``done=True``.

Exit code 0 = PASS, 1 = FAIL.
"""

from __future__ import annotations

import socket
import subprocess
import sys
import time
from pathlib import Path

AI_ENGINE_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = AI_ENGINE_ROOT.parent
sys.path.insert(0, str(REPO_ROOT / "shared" / "gen" / "python"))
sys.path.insert(0, str(AI_ENGINE_ROOT))

import grpc  # noqa: E402
import pcb_pb2 as pb  # noqa: E402
import pcb_pb2_grpc as pb_grpc  # noqa: E402

PORT = 50077


def _build_request() -> tuple:
    """Small 20x15mm board, 2 nets x 2 pads, plus 4 components for placement."""
    board = pb.BoardSpec(
        width_mm=20.0,
        height_mm=15.0,
        layer_count=2,
        grid_resolution_mm=0.25,
        layer_names=["F.Cu", "B.Cu"],
    )

    def pad(ref: str, name: str, x: float, y: float, layer: int) -> pb.PadRef:
        return pb.PadRef(
            component_ref=ref,
            pad_name=name,
            position=pb.Point(x=x, y=y),
            layer=layer,
            width_mm=0.6,
            height_mm=0.6,
        )

    nets = [
        pb.NetSpec(
            name="NET1",
            net_class="default",
            pads=[pad("U1", "1", 2.0, 2.0, 0), pad("U2", "1", 17.0, 12.0, 0)],
            min_track_width_mm=0.25,
            clearance_mm=0.2,
        ),
        pb.NetSpec(
            name="NET2",
            net_class="default",
            pads=[pad("U3", "1", 2.0, 12.0, 1), pad("U4", "1", 17.0, 2.0, 1)],
            min_track_width_mm=0.25,
            clearance_mm=0.2,
        ),
    ]

    components = []
    for i, (ref, x, y) in enumerate(
        [("U1", 2.0, 2.0), ("U2", 17.0, 12.0), ("U3", 2.0, 12.0), ("U4", 17.0, 2.0)]
    ):
        comp = pb.ComponentSpec(ref=ref, footprint="0603")
        comp.position.x = x
        comp.position.y = y
        comp.bbox_mm.min_x = x - 0.5
        comp.bbox_mm.min_y = y - 0.3
        comp.bbox_mm.max_x = x + 0.5
        comp.bbox_mm.max_y = y + 0.3
        comp.height_mm = 0.5
        comp.fixed = i == 0
        components.append(comp)
    return board, nets, components


def _wait_for_port(port: int, timeout_s: float = 20.0) -> bool:
    """Poll the port until the server accepts TCP connections."""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                return True
        except OSError:
            time.sleep(0.2)
    return False


def main() -> int:
    """Run the end-to-end gRPC smoke test."""
    process = subprocess.Popen(
        [sys.executable, str(AI_ENGINE_ROOT / "cmd" / "ai-server" / "main.py"), "--port", str(PORT)],
        cwd=str(AI_ENGINE_ROOT),
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    channel = None
    try:
        if not _wait_for_port(PORT):
            print(f"[smoke-grpc] FAIL - server did not open port {PORT}")
            return 1
        channel = grpc.insecure_channel(f"127.0.0.1:{PORT}")
        grpc.channel_ready_future(channel).result(timeout=10)
        stub = pb_grpc.AIRouterServiceStub(channel)
        board, nets, components = _build_request()

        # 1) GetHealth
        health = stub.GetHealth(pb.HealthRequest())
        print(
            f"[smoke-grpc] GetHealth: status={health.status} version={health.version}"
            f" device={health.device} model_loaded={health.model_loaded}"
        )
        assert health.status == "ok", f"bad status: {health.status}"
        assert health.version, "empty version"
        assert isinstance(health.model_loaded, bool)

        # 2) PlanPlacement
        placement = stub.PlanPlacement(
            pb.PlacementRequest(board=board, components=components, strategy="heuristic")
        )
        print(
            f"[smoke-grpc] PlanPlacement: {len(placement.placed)} components,"
            f" wire={placement.total_wirelength_mm:.2f} mm,"
            f" score={placement.score:.2f}, strategy={placement.strategy}"
        )
        assert len(placement.placed) == len(components)
        for comp in placement.placed:
            assert 0.0 <= comp.position.x <= board.width_mm
            assert 0.0 <= comp.position.y <= board.height_mm
        assert placement.strategy == "heuristic"

        # 3) RouteBoard (streaming)
        request = pb.RouteRequest(board=board, nets=nets, strategy="astar")
        events = list(stub.RouteBoard(request))
        for event in events:
            partial = (
                f" partial={event.partial.net}:{event.partial.length_mm:.1f}mm"
                f"(completed={event.partial.completed})"
                if event.HasField("partial")
                else ""
            )
            print(
                f"[smoke-grpc] event: stage={event.stage} net={event.current_net or '-'}"
                f" percent={event.percent:.0f} done={event.done}{partial}"
                f" msg='{event.message}'"
            )
        assert events, "no progress event received"
        assert events[-1].done is True, "final event must have done=True"
        assert events[-1].percent == 100.0
        assert not events[-1].error, f"final event error: {events[-1].error}"
        partials = [e.partial for e in events if e.HasField("partial")]
        assert len(partials) == 2, f"expected 2 per-net events, got {len(partials)}"
        assert all(p.completed for p in partials), "both nets must be completed"
        assert all(p.length_mm > 0.0 for p in partials), "lengths must be positive"

        # 4) OptimizeRoutes (streaming) over the streamed results
        opt_request = pb.OptimizeRequest(
            board=board, nets=nets, routes=partials, objectives=["length", "vias", "drc"]
        )
        opt_events = list(stub.OptimizeRoutes(opt_request))
        for event in opt_events:
            print(
                f"[smoke-grpc] optimize: net={event.current_net or '-'}"
                f" percent={event.percent:.0f} done={event.done} msg='{event.message}'"
            )
        assert opt_events and opt_events[-1].done is True
        assert not opt_events[-1].error, f"optimize error: {opt_events[-1].error}"

        print("[smoke-grpc] PASS - health, placement, routing and optimization OK")
        return 0
    except Exception as exc:
        print(f"[smoke-grpc] FAIL - {exc.__class__.__name__}: {exc}")
        return 1
    finally:
        if channel is not None:
            channel.close()
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)


if __name__ == "__main__":
    sys.exit(main())
