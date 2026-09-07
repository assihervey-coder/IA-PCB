#!/usr/bin/env python3
"""YahriaCad AI engine - gRPC server entrypoint.

Bootstraps ``sys.path`` (ai-engine root + ``shared/gen/python`` stubs), starts
``grpc.server`` with a thread pool, enables gRPC reflection when available and
handles SIGTERM/SIGINT with a graceful stop.

Usage:
    python3 cmd/ai-server/main.py --port 50051 --log-level info
    YAHRIACAD_AI_PORT=50051 python3 cmd/ai-server/main.py
"""

from __future__ import annotations

import argparse
import json
import logging
import os
import signal
import sys
from concurrent import futures
from pathlib import Path

# --------------------------------------------------------------------------
# Path bootstrap (ai-engine root + generated gRPC stubs)
# --------------------------------------------------------------------------
AI_ENGINE_ROOT = Path(__file__).resolve().parents[2]
REPO_ROOT = AI_ENGINE_ROOT.parent
SHARED_GEN_DIR = REPO_ROOT / "shared" / "gen" / "python"
for _path in (str(SHARED_GEN_DIR), str(AI_ENGINE_ROOT)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

import grpc  # noqa: E402
import pcb_pb2_grpc as pb_grpc  # noqa: E402
from src.service import AIRouterServicer  # noqa: E402

DEFAULT_PORT = int(os.environ.get("YAHRIACAD_AI_PORT", "50051"))


class JsonFormatter(logging.Formatter):
    """Minimal JSON log formatter (structured logs)."""

    def format(self, record: logging.LogRecord) -> str:
        payload = {
            "time": self.formatTime(record, "%Y-%m-%dT%H:%M:%S%z"),
            "level": record.levelname,
            "logger": record.name,
            "message": record.getMessage(),
        }
        if record.exc_info:
            payload["exception"] = self.formatException(record.exc_info)
        return json.dumps(payload, ensure_ascii=True)


def build_logger(level_name: str) -> logging.Logger:
    """Configure and return the service logger."""
    level = getattr(logging, str(level_name).upper(), logging.INFO)
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JsonFormatter())
    logger = logging.getLogger("yahriacad.ai")
    logger.setLevel(level)
    logger.handlers.clear()
    logger.addHandler(handler)
    logger.propagate = False
    return logger


def parse_args(argv=None) -> argparse.Namespace:
    """CLI arguments."""
    parser = argparse.ArgumentParser(description="YahriaCad AI engine (gRPC)")
    parser.add_argument("--port", type=int, default=DEFAULT_PORT, help="gRPC port (default: YAHRIACAD_AI_PORT or 50051)")
    parser.add_argument("--host", type=str, default="0.0.0.0", help="bind address (default: 0.0.0.0)")
    parser.add_argument("--config", type=str, default=None, help="path to training/router/config.yaml")
    parser.add_argument("--log-level", type=str, default="info", help="debug | info | warning | error")
    return parser.parse_args(argv)


def print_banner(host: str, port: int, servicer: AIRouterServicer) -> None:
    """Startup banner (human readable)."""
    line = "=" * 62
    print(line)
    print(" YahriaCad - AI engine (gRPC)")
    print(f"   listening : {host}:{port}")
    print(f"   version   : {servicer.version}")
    print(f"   device    : {servicer.device}")
    print(f"   RL model  : {'loaded' if servicer.model_loaded else 'not loaded (A* fallback)'}")
    print(line, flush=True)


def main(argv=None) -> int:
    """Start the gRPC server and block until SIGTERM/SIGINT."""
    args = parse_args(argv)
    logger = build_logger(args.log_level)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    servicer = AIRouterServicer(config_path=args.config, logger=logger)
    pb_grpc.add_AIRouterServiceServicer_to_server(servicer, server)

    bound = server.add_insecure_port(f"[::]:{args.port}")
    if bound == 0:
        bound = server.add_insecure_port(f"{args.host}:{args.port}")
    if bound == 0:
        logger.error("could not bind port %s", args.port)
        return 1

    # Optional gRPC reflection (grpcio-reflection), non-blocking when absent.
    try:
        from grpc_reflection.v1alpha import reflection

        service_names = (
            pb_grpc.__dict__.get("pcb__pb2").DESCRIPTOR.services_by_name["AIRouterService"].full_name,
            reflection.SERVICE_NAME,
        )
        reflection.enable_server_reflection(service_names, server)
        logger.info("gRPC reflection enabled")
    except Exception:
        logger.info("gRPC reflection unavailable (grpcio-reflection not installed)")

    stop_requested = False

    def handle_signal(signum, _frame):
        nonlocal stop_requested
        if not stop_requested:
            stop_requested = True
            logger.info("signal %s received, shutting down gracefully", signum)
            server.stop(grace=0.5)

    signal.signal(signal.SIGTERM, handle_signal)
    signal.signal(signal.SIGINT, handle_signal)

    server.start()
    print_banner(args.host, args.port, servicer)
    logger.info("ai-engine serving on %s:%s", args.host, args.port)

    try:
        server.wait_for_termination()
    except KeyboardInterrupt:  # pragma: no cover - fallback for raw Ctrl-C
        server.stop(grace=0.5).wait(timeout=2)
    return 0


if __name__ == "__main__":
    sys.exit(main())
