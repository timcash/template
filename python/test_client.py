from __future__ import annotations

import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

from python.client import add


class PythonClientTests(unittest.TestCase):
    @patch("python.client.subprocess.run")
    def test_add_invokes_worker_and_decodes_json(self, run: unittest.mock.Mock) -> None:
        run.return_value = subprocess.CompletedProcess([], 0, json.dumps({"result": 42}), "")

        worker = Path("/tmp/json_math")
        self.assertEqual(add(19, 23, worker), 42)
        run.assert_called_once_with(
            [str(worker), "add", "19", "23"],
            check=True,
            capture_output=True,
            text=True,
        )

    def test_add_rejects_non_integer_input(self) -> None:
        with self.assertRaises(TypeError):
            add(True, 2)

    @patch("python.client.subprocess.run")
    def test_add_rejects_malformed_worker_response(self, run: unittest.mock.Mock) -> None:
        run.return_value = subprocess.CompletedProcess([], 0, json.dumps({"result": "forty-two"}), "")
        with self.assertRaises(ValueError):
            add(20, 22, Path("/tmp/json_math"))


if __name__ == "__main__":
    unittest.main()
