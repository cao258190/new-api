"""Fail on high or critical CodeQL findings in tracked runtime source."""

import json
from pathlib import Path
import subprocess
import sys
from urllib.parse import unquote, urlsplit


workspace = Path.cwd().resolve()
source_files = {
    name
    for name in subprocess.check_output(["git", "ls-files", "-z"]).decode("utf-8").split("\0")
    if Path(name).suffix in {".go", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts"}
    and not {"vendor", "node_modules", "__tests__", "__mocks__", "__fixtures__", "testdata"}.intersection(Path(name).parts)
    and not name.endswith("_test.go")
    and ".test." not in Path(name).name
    and ".spec." not in Path(name).name
}
reports = sorted(Path(sys.argv[1]).glob("*.sarif"))
if not reports:
    raise SystemExit("No CodeQL SARIF report was produced")

blocking = []
for report in reports:
    analysis = json.loads(report.read_text(encoding="utf-8"))
    for run in analysis["runs"]:
        driver = run["tool"]["driver"]
        if driver["name"] != "CodeQL":
            raise SystemExit("The source security gate accepts only CodeQL results")
        rules = {
            rule["id"]: rule
            for component in [driver, *run["tool"].get("extensions", [])]
            for rule in component.get("rules", [])
        }
        for result in run.get("results", []):
            rule = rules[result["ruleId"]]
            severity = float(rule.get("properties", {}).get("security-severity", 0))
            if severity < 7 or result.get("kind", "fail") != "fail":
                continue
            for location in result.get("locations", []):
                physical = location.get("physicalLocation", {})
                artifact = physical.get("artifactLocation", {})
                if "uri" not in artifact and "index" in artifact:
                    artifact = run["artifacts"][artifact["index"]]["location"]
                uri = urlsplit(artifact.get("uri", ""))
                if uri.scheme not in {"", "file"}:
                    continue
                path = Path(unquote(uri.path))
                if path.is_absolute():
                    try:
                        path = path.relative_to(workspace)
                    except ValueError:
                        continue
                if path.as_posix() in source_files:
                    blocking.append({"rule": rule["id"], "file": path.as_posix(),
                                     "line": physical.get("region", {}).get("startLine", 1),
                                     "security_severity": severity})
                    break

summary = {"policy": "runtime-source-code-only", "blocking_findings": len(blocking), "findings": blocking}
(Path(sys.argv[1]) / "source-security-summary.json").write_text(
    json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
)
print(json.dumps(summary, ensure_ascii=False))
raise SystemExit(1 if blocking else 0)
