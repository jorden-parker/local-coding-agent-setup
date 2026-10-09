#!/usr/bin/env python3
"""Compare normal/lean Qwen sessions on disposable coding projects.

Start an isolated llama-server first. This runner uses temporary HOME/XDG
directories and never changes your Qwen settings or existing projects.
"""

import argparse
import json
import os
from pathlib import Path
import statistics
import subprocess
import tempfile
import time


TASK = """Find calc.py with glob and search for its return statement, then read it.
Use tool_search once with query "select:web_fetch" to inspect that deferred schema;
do not invoke web_fetch.
Fix add(a, b) to return their sum. Create test_calc.py
using unittest with tests for positive integers, negative integers and zero.
Run python3 -m unittest -v. Finish when all three tests pass. Do not use subagents.
Do not modify any files outside this project. Keep your final reply to one sentence."""


def tool_trace(log_text):
    """Use recorded tool results, never the assistant's execution claims."""
    events = []
    marker = log_text.find('[{"type":"system"')
    if marker >= 0:
        try:
            events = json.loads(log_text[marker:])
        except ValueError:
            pass
    tool_stats = {}
    available_tools = []
    for event in events:
        if event.get("type") == "system" and event.get("subtype") == "init":
            available_tools = event.get("tools", [])
        if event.get("type") == "result":
            tool_stats = event.get("stats", {}).get("tools", {}).get("byName", {})
    return available_tools, tool_stats


def trial(args, profile, repetition):
    trial_dir = args.output / f"{profile}-{repetition + 1}"
    project = trial_dir / "project"
    project.mkdir(parents=True)
    (project / "calc.py").write_text("def add(a, b):\n    return a - b\n")
    with tempfile.TemporaryDirectory(prefix="lca-qwen-") as home:
        env = dict(os.environ, HOME=home, XDG_CONFIG_HOME=home + "/config",
                   XDG_STATE_HOME=home + "/state", OPENAI_API_KEY="local",
                   OPENAI_MODEL=args.alias, OPENAI_BASE_URL=args.base_url.rstrip("/") + "/v1",
                   QWEN_CODE_MAX_OUTPUT_TOKENS="512", QWEN_CODE_MODELS_DEV_REFRESH="off")
        # Pin the same provider and clear inherited credentials/config overrides.
        for key in ("QWEN_API_KEY", "GEMINI_API_KEY", "QWEN_CODE_ENABLE_WORKFLOWS",
                    "QWEN_CODE_DISABLE_WORKFLOWS"):
            env.pop(key, None)
        settings = Path(home) / ".qwen/settings.json"
        settings.parent.mkdir()
        settings.write_text(json.dumps({"$version": 4, "modelProviders": {"openai": [{
            "id": args.alias, "baseUrl": env["OPENAI_BASE_URL"], "envKey": "OPENAI_API_KEY",
            "generationConfig": {"contextWindowSize": args.ctx}
        }]}, "tools": {"core": ["read_file", "write_file", "edit", "glob",
                                  "grep_search", "run_shell_command"]},
            "permissions": {"allow": ["Bash(python3 -m unittest*)"]}}))
        if profile == "lean":
            subprocess.run([str(args.lca), "qwen-profile", "lean"], env=env, check=True,
                           stdout=subprocess.DEVNULL)
        start = time.monotonic()
        with (trial_dir / "qwen.log").open("w") as log:
            try:
                result = subprocess.run([args.qwen, "--auth-type", "openai", "--model", args.alias,
                    "--max-session-turns", "16", "--max-wall-time", str(args.timeout),
                    "--approval-mode", "auto-edit", "--output-format", "json", "-p", TASK],
                    cwd=project, env=env, stdout=log, stderr=subprocess.STDOUT,
                    timeout=args.timeout, check=False)
                returncode = result.returncode
            except subprocess.TimeoutExpired:
                returncode = None
        duration = (time.monotonic() - start) * 1000
        log_text = (trial_dir / "qwen.log").read_text()
        available_tools, tool_stats = tool_trace(log_text)
        shell_executed = tool_stats.get("run_shell_command", {}).get("success", 0) > 0
        discovery_ok = tool_stats.get("tool_search", {}).get("success", 0) > 0
        usage = []
        for file in settings.parent.glob("usage/token-usage-*.jsonl"):
            for line in file.read_text().splitlines():
                if line.strip():
                    usage.append(json.loads(line))
        (trial_dir / "usage.jsonl").write_text("".join(json.dumps(row) + "\n" for row in usage))
        (trial_dir / "settings.json").write_text(settings.read_text())
    # Verify behaviour independently of the model's final reply/test claims.
    check = subprocess.run(["python3", "-c",
        "from calc import add; assert add(2,3)==5; assert add(-2,-3)==-5; assert add(0,0)==0"],
        cwd=project, capture_output=True, text=True, timeout=10)
    tests = subprocess.run(["python3", "-m", "unittest", "-v"], cwd=project,
                           capture_output=True, text=True, timeout=30)
    tests_output = tests.stdout + tests.stderr
    (trial_dir / "tests.log").write_text(check.stdout + check.stderr + tests_output)
    sources = {}
    for row in usage:
        source = row.get("source", "unknown")
        sources[source] = sources.get(source, 0) + 1
    return {"profile": profile, "repetition": repetition, "total_ms": duration,
            "returncode": returncode, "behaviour_ok": check.returncode == 0,
            "tests_ok": tests.returncode == 0 and "Ran 3 tests" in tests_output,
            "shell_executed": shell_executed, "discovery_ok": discovery_ok,
            "available_tools": available_tools, "tool_stats": tool_stats,
            "request_sources": sources, "request_count": len(usage)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True, help="Isolated server URL without /v1")
    parser.add_argument("--alias", required=True)
    parser.add_argument("--ctx", type=int, required=True)
    parser.add_argument("--lca", type=Path, required=True)
    parser.add_argument("--qwen", default="qwen")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--repetitions", type=int, default=3)
    parser.add_argument("--timeout", type=int, default=900)
    args = parser.parse_args()
    if args.repetitions < 1 or args.timeout < 1:
        parser.error("repetitions and timeout must be positive")
    if not args.lca.is_file():
        parser.error("--lca must name a built executable")
    args.lca = args.lca.resolve()
    args.output = args.output.resolve()
    args.output.mkdir(parents=True, exist_ok=False)
    version = subprocess.run([args.qwen, "--version"], capture_output=True, text=True, check=True)
    (args.output / "metadata.json").write_text(json.dumps({"qwen_version": version.stdout.strip(),
        "base_url": args.base_url, "alias": args.alias, "ctx": args.ctx, "task": TASK,
        "max_output_tokens": 512, "repetitions": args.repetitions}, indent=2) + "\n")
    rows = []
    try:
        for repetition in range(args.repetitions):
            # Alternate order to reduce warmup/thermal bias.
            order = ("normal", "lean") if repetition % 2 == 0 else ("lean", "normal")
            for profile in order:
                row = trial(args, profile, repetition)
                rows.append(row)
                print(json.dumps(row), flush=True)
                (args.output / "summary.json").write_text(json.dumps(rows, indent=2) + "\n")
    finally:
        (args.output / "summary.json").write_text(json.dumps(rows, indent=2) + "\n")
    for profile in ("normal", "lean"):
        selected = [row for row in rows if row["profile"] == profile]
        print(f"{profile}: median {statistics.median(row['total_ms'] for row in selected) / 1000:.1f}s; "
              f"all passed: {all(row['behaviour_ok'] and row['tests_ok'] and row['shell_executed'] and row['discovery_ok'] and row['returncode'] == 0 for row in selected)}")


if __name__ == "__main__":
    main()
