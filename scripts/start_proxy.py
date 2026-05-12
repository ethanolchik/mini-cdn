#!/usr/bin/env python3

import argparse
import os
import signal
import subprocess
import time
from pathlib import Path
from urllib.parse import urlparse


SHUTDOWN_TIMEOUT_SECONDS = 5


def parse_args():
    parser = argparse.ArgumentParser(description="Start the local proxy server.")
    parser.add_argument("port", help="proxy port to listen on")
    parser.add_argument(
        "origins",
        nargs="+",
        help="origin server addresses, e.g. localhost:8001 or http://localhost:8001",
    )
    return parser.parse_args()


def normalize_origin(origin):
    parsed = urlparse(origin)
    if parsed.scheme:
        return origin
    return f"http://{origin}"


def start_proxy(port, origins, repo_root):
    return subprocess.Popen(
        ["go", "run", "cmd/main.go", port, *origins],
        cwd=repo_root,
        start_new_session=True,
    )


def stop_proxy(process):
    if process.poll() is not None:
        return

    print("\nStopping proxy server...", flush=True)
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        return

    deadline = time.monotonic() + SHUTDOWN_TIMEOUT_SECONDS
    while time.monotonic() < deadline:
        if process.poll() is not None:
            return
        time.sleep(0.1)

    if process.poll() is None:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait()


def main():
    args = parse_args()
    repo_root = Path(__file__).resolve().parents[1]
    origins = [normalize_origin(origin) for origin in args.origins]
    process = start_proxy(args.port, origins, repo_root)
    print(f"Started proxy on :{args.port} (pid {process.pid})", flush=True)

    def handle_shutdown(signum, _frame):
        raise SystemExit(128 + signum)

    signal.signal(signal.SIGINT, handle_shutdown)
    signal.signal(signal.SIGTERM, handle_shutdown)

    try:
        return process.wait()
    finally:
        stop_proxy(process)


if __name__ == "__main__":
    raise SystemExit(main())
