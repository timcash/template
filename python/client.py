"""Call the Rust integer worker and decode its JSON response."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
from typing import Any


def default_worker_path() -> Path:
    suffix = ".exe" if os.name == "nt" else ""
    return Path(__file__).resolve().parents[1] / "rust" / "target" / "release" / f"json_math{suffix}"


def add(left: int, right: int, worker: Path | None = None) -> int:
    """Return ``left + right`` as calculated by the Rust subprocess."""
    if isinstance(left, bool) or isinstance(right, bool) or not isinstance(left, int) or not isinstance(right, int):
        raise TypeError("left and right must be integers")

    command = [str(worker or default_worker_path()), "add", str(left), str(right)]
    completed = subprocess.run(command, check=True, capture_output=True, text=True)
    payload: Any = json.loads(completed.stdout)
    result = payload.get("result") if isinstance(payload, dict) else None
    if isinstance(result, bool) or not isinstance(result, int):
        raise ValueError("Rust worker response must contain an integer result")
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("left", type=int)
    parser.add_argument("right", type=int)
    parser.add_argument("--worker", type=Path, default=None)
    args = parser.parse_args()
    print(json.dumps({"result": add(args.left, args.right, args.worker)}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
