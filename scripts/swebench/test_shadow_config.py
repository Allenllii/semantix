#!/usr/bin/env python3
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

from run_bench import SemantixAdapter


class ShadowConfigTests(unittest.TestCase):
    def test_write_home_emits_explicit_shadow_mode(self):
        adapter = SemantixAdapter.__new__(SemantixAdapter)
        adapter.args = SimpleNamespace(
            openai_base="http://127.0.0.1:8139/v1",
            effort="",
            model="deepseek-v4-flash",
            semantix_retrieval_mode="shadow",
        )
        adapter.memory_on = True
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            adapter.kernel_dir = root / "kernel"
            adapter._write_home(root / "home", root / "sessions")
            config = (root / "home" / "config.toml").read_text()
        self.assertIn('mode         = "shadow"', config)
        self.assertIn("enabled      = true", config)


if __name__ == "__main__":
    unittest.main()
