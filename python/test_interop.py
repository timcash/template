from __future__ import annotations

import os
from pathlib import Path
import subprocess
import unittest

from python.client import add


class RustInteropTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.root = Path(__file__).resolve().parents[1]
        subprocess.run(
            ["cargo", "build", "--manifest-path", "rust/Cargo.toml", "--release"],
            cwd=cls.root,
            check=True,
        )
        suffix = ".exe" if os.name == "nt" else ""
        cls.worker = cls.root / "rust" / "target" / "release" / f"json_math{suffix}"

    def test_python_calls_real_rust_worker(self) -> None:
        self.assertEqual(add(20, 22, self.worker), 42)
        self.assertEqual(add(-8, 3, self.worker), -5)


if __name__ == "__main__":
    unittest.main()
