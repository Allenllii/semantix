import json
import tempfile
import unittest
from pathlib import Path

import validate_memory_seed as validator


class ValidateMemorySeedTest(unittest.TestCase):
    def fixture(self, root: Path, with_commit: bool = True):
        dataset = root / "dataset.jsonl"
        dataset.write_text(json.dumps({"instance_id": "i1", "repo": "Org/Repo"}) + "\n")
        ids = root / "ids.txt"
        ids.write_text("i1\n")
        db = root / "seed" / "org__repo" / ".semantix" / "project.db"
        db.parent.mkdir(parents=True)
        db.write_text("")
        rows = [{"j": 1, "bsize": 0, "bmtime": 0, "bsha": ""}]
        for index in range(5):
            meta = {"SourceSession": f"s{index % 2}"}
            if with_commit:
                meta["base_commit"] = "abc123"
            rows.append({"op": "put", "s": {
                "ID": f"slice-{index}", "Type": 1, "Scope": 1,
                "Content": "eA==", "Meta": meta,
            }})
        Path(str(db) + ".journal").write_text(
            "".join(json.dumps(row) + "\n" for row in rows))
        return root / "seed", dataset, ids

    def test_accepts_repo_with_minimum_library_sessions_and_commits(self):
        with tempfile.TemporaryDirectory() as td:
            report = validator.validate(*self.fixture(Path(td)))
        self.assertTrue(report["ok"])
        self.assertEqual(report["summary"]["ready_repos"], 1)

    def test_reports_missing_commit_and_missing_repo_store(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            seed, dataset, ids = self.fixture(root, with_commit=False)
            with dataset.open("a") as handle:
                handle.write(json.dumps({"instance_id": "i2", "repo": "Other/Repo"}) + "\n")
            ids.write_text("i1\ni2\n")
            report = validator.validate(seed, dataset, ids)
        self.assertFalse(report["ok"])
        self.assertIn("org/repo:commit_unknown", report["errors"])
        self.assertIn("other/repo:missing_store", report["errors"])


if __name__ == "__main__":
    unittest.main()
