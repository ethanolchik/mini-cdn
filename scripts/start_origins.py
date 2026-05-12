#!/usr/bin/env python3

import argparse
import os
import signal
import subprocess
import time
from pathlib import Path


DEFAULT_ORIGIN_PORTS = ["8081", "8082", "8083", "8084", "8085"]
SHUTDOWN_TIMEOUT_SECONDS = 5


def parse_args():
    parser = argparse.ArgumentParser(description="Start local origin servers.")
    parser.add_argument(
        "ports",
        nargs="*",
        default=DEFAULT_ORIGIN_PORTS,
        help="origin ports to start (default: %(default)s)",
    )
    return parser.parse_args()


def start_origin(port, repo_root):
    return subprocess.Popen(
        ["go", "run", "cmd/origin/main.go", port],
        cwd=repo_root,
        start_new_session=True,
    )


def stop_origins(processes):
    running = [process for process in processes if process.poll() is None]
    if not running:
        return

    print("\nStopping origin servers...")
    for process in running:
        os.killpg(process.pid, signal.SIGTERM)

    deadline = time.monotonic() + SHUTDOWN_TIMEOUT_SECONDS
    while time.monotonic() < deadline:
        if all(process.poll() is not None for process in running):
            return
        time.sleep(0.1)

    still_running = [process for process in running if process.poll() is None]
    for process in still_running:
        os.killpg(process.pid, signal.SIGKILL)


def wait_for_origins(processes):
    while True:
        for process in processes:
            return_code = process.poll()
            if return_code is not None:
                return return_code
        time.sleep(0.25)


def main():
    args = parse_args()
    repo_root = Path(__file__).resolve().parents[1]
    processes = []

    def handle_shutdown(signum, _frame):
        stop_origins(processes)
        raise SystemExit(128 + signum)

    signal.signal(signal.SIGINT, handle_shutdown)
    signal.signal(signal.SIGTERM, handle_shutdown)

    try:
        for port in args.ports:
            process = start_origin(port, repo_root)
            processes.append(process)
            print(f"Started origin on :{port} (pid {process.pid})")

        return wait_for_origins(processes)
    finally:
        stop_origins(processes)


if __name__ == "__main__":
    raise SystemExit(main())
