#!/usr/bin/env python3
"""Benchmark an isolated llama-server; never edits installed configuration.

Uses only the Python standard library. Raw timings and server logs go to a new
output directory. Run outside the macOS sandbox so Metal is actually available.
"""

import argparse
import json
import os
from pathlib import Path
import platform
import re
import socket
import statistics
import subprocess
import threading
import time
import urllib.error
import urllib.request


def command(*args):
    result = subprocess.run(args, capture_output=True, text=True, check=False)
    return (result.stdout + result.stderr).strip()


def memory(pid):
    swap = command("sysctl", "vm.swapusage")
    match = re.search(r"used = ([\d.]+)M", swap)
    return {
        "swap_used_mib": float(match[1]) if match else None,
        "rss_kib": command("ps", "-o", "rss=", "-p", str(pid)),
        "pressure": command("memory_pressure", "-Q"),
    }


def post(base, route, data):
    request = urllib.request.Request(
        base + route, data=json.dumps(data).encode(),
        headers={"Content-Type": "application/json"},
    )
    return urllib.request.urlopen(request, timeout=900)


def completion(base, messages, max_tokens, tools=None, force_tool=False):
    data = {"model": "benchmark", "messages": messages, "stream": True,
            "stream_options": {"include_usage": True}, "max_tokens": max_tokens,
            "temperature": 0, "seed": 42, "cache_prompt": True}
    if tools:
        data["tools"] = tools
    if force_tool:
        data["tool_choice"] = {"type": "function", "function": {"name": "read_file"}}
    start = time.monotonic()
    first = None
    timings, usage, text, tool_calls = {}, {}, "", {}
    with post(base, "/v1/chat/completions", data) as response:
        for line in response:
            if not line.startswith(b"data: ") or line.strip() == b"data: [DONE]":
                continue
            event = json.loads(line[6:])
            if "error" in event:
                raise RuntimeError(event["error"])
            timings.update(event.get("timings") or {})
            usage.update(event.get("usage") or {})
            for choice in event.get("choices", []):
                delta = choice.get("delta", {})
                if delta.get("content") or delta.get("tool_calls") or delta.get("reasoning_content"):
                    if first is None:
                        first = (time.monotonic() - start) * 1000
                text += delta.get("content") or ""
                for fragment in delta.get("tool_calls", []):
                    call = tool_calls.setdefault(fragment["index"],
                                                 {"id": "", "type": "function",
                                                  "function": {"name": "", "arguments": ""}})
                    if fragment.get("id"):
                        call["id"] = fragment["id"]
                    for key in ("name", "arguments"):
                        call["function"][key] += fragment.get("function", {}).get(key, "")
    if not timings:
        raise RuntimeError("No server timings returned; cannot separate prefill and decode")
    return {"ttft_ms": first, "total_ms": (time.monotonic() - start) * 1000,
            "timings": timings, "usage": usage}, text, list(tool_calls.values())


TOOLS = [{"type": "function", "function": {
    "name": "read_file", "description": "Read a file from this project.",
    "parameters": {"type": "object", "properties": {"path": {"type": "string"}},
                   "required": ["path"]},
}}]


def token_prompt(base, target):
    # Repeat a short source-code fixture, then slice by the server tokenizer.
    with post(base, "/tokenize", {"content": "def add(a, b): return a + b\n" * target}) as response:
        tokens = json.load(response)["tokens"][:target]
    with post(base, "/detokenize", {"tokens": tokens}) as response:
        return json.load(response)["content"]


def stop(process):
    process.terminate()
    try:
        process.wait(timeout=15)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()


def run_variant(args, name, cache, checkpoints, ubatch, unified=False):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    flags = [args.server, "-m", str(args.model), "--alias", "benchmark", "-c", str(args.ctx),
             "-fa", "on", "-ngl", "99", "-np", "1", "-b", "2048", "-ub", str(ubatch),
             "--cache-ram", str(cache), "--ctx-checkpoints", str(checkpoints),
             "--temp", "0.7", "--top-p", "0.8", "--top-k", "20", "--min-p", "0",
             "--presence-penalty", "1.5", "--chat-template-kwargs", '{"enable_thinking":false}',
             "--host", "127.0.0.1", "--port", str(port), "--jinja", "--metrics", "--verbosity", "5"]
    if unified:
        flags.append("--kv-unified")
    print(f"Starting {name}: cache={cache} checkpoints={checkpoints} ubatch={ubatch}", flush=True)
    rows, samples = [], []
    sampling_done = threading.Event()
    with (args.output / f"{name}.log").open("w") as log:
        process = subprocess.Popen(flags, stdout=log, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 180
            while True:
                if process.poll() is not None:
                    raise RuntimeError(f"{name}: server exited; inspect {log.name}")
                try:
                    with urllib.request.urlopen(base + "/health", timeout=2) as response:
                        if response.status == 200:
                            break
                except (urllib.error.URLError, TimeoutError):
                    pass
                if time.monotonic() >= deadline:
                    raise RuntimeError(f"{name}: server startup timed out; inspect {log.name}")
                time.sleep(1)
            log_text = Path(log.name).read_text()
            if "MTL0 (Apple" not in log_text or not re.search(r"offloaded (\d+)/\1 layers", log_text):
                raise RuntimeError(f"{name}: full Metal offload not confirmed; inspect {log.name}")

            def sample():
                while not sampling_done.is_set():
                    samples.append(memory(process.pid))
                    sampling_done.wait(2)

            monitor = threading.Thread(target=sample, daemon=True)
            monitor.start()
            prompt = token_prompt(base, args.prompt_tokens)
            for repetition in range(args.repetitions):
                system = {"role": "system", "content":
                          f"Run {repetition}. You are a coding assistant. Follow the user's request."}
                history = [system]
                for scenario in ("cold", "growing", "branch", "return"):
                    if scenario == "cold":
                        history.append({"role": "user", "content": prompt + "\nReply with one short sentence describing this code."})
                        messages = history
                    elif scenario == "growing":
                        history.append({"role": "user", "content": "Explain the return value in one short sentence."})
                        messages = history
                    elif scenario == "branch":
                        messages = [{"role": "system", "content": f"Explore run {repetition}. You review code."},
                                    {"role": "user", "content": prompt + "\nName one test for this code."}]
                    else:
                        history.append({"role": "user", "content": "Name another test in one short sentence."})
                        messages = history
                    result, text, _ = completion(base, messages, args.max_tokens, TOOLS)
                    row = {"variant": name, "repetition": repetition, "scenario": scenario, **result}
                    rows.append(row)
                    with (args.output / "requests.jsonl").open("a") as output:
                        output.write(json.dumps(row) + "\n")
                    if scenario != "branch":
                        history.append({"role": "assistant", "content": text})
                    print(f"  {repetition + 1}/{args.repetitions} {scenario}: "
                          f"{result['total_ms'] / 1000:.1f}s, cache={result['timings'].get('cache_n')}", flush=True)
            _, _, calls = completion(base, [
                {"role": "user", "content": "Call read_file to read sample.py. Do not explain."}
            ], 128, TOOLS, force_tool=True)
            try:
                tool_ok = bool(calls and calls[0]["function"]["name"] == "read_file"
                               and json.loads(calls[0]["function"]["arguments"]).get("path") == "sample.py")
            except (ValueError, TypeError, AttributeError):
                tool_ok = False
            samples.append(memory(process.pid))
        finally:
            sampling_done.set()
            if "monitor" in locals():
                monitor.join(timeout=5)
            stop(process)
    swaps = [sample["swap_used_mib"] for sample in samples if sample["swap_used_mib"] is not None]
    result = {"variant": name, "argv": flags, "tool_call_ok": tool_ok,
              "swap_growth_mib": max(0, max(swaps) - swaps[0]) if swaps else None,
              "memory_samples": samples,
              "median_total_ms": statistics.median(row["total_ms"] for row in rows),
              "scenarios": {}}
    for scenario in ("cold", "growing", "branch", "return"):
        subset = [row for row in rows if row["scenario"] == scenario]
        result["scenarios"][scenario] = {
            "total_ms": statistics.median(row["total_ms"] for row in subset),
            "ttft_ms": statistics.median(row["ttft_ms"] for row in subset if row["ttft_ms"] is not None),
            "prompt_tps": statistics.median(row["timings"]["prompt_per_second"] for row in subset),
            "generation_tps": statistics.median(row["timings"]["predicted_per_second"] for row in subset),
            "cache_n": statistics.median(row["timings"]["cache_n"] for row in subset),
        }
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--profile", choices=("m1", "m4"), required=True)
    parser.add_argument("--ctx", type=int)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--server", default="llama-server")
    parser.add_argument("--prompt-tokens", type=int, default=2048)
    parser.add_argument("--max-tokens", type=int, default=32)
    parser.add_argument("--repetitions", type=int, default=3)
    parser.add_argument("--variants", nargs="+", help="Optional subset of named variants")
    args = parser.parse_args()
    args.ctx = args.ctx or (65536 if args.profile == "m1" else 131072)
    if not args.model.is_file():
        parser.error("--model must be an existing GGUF file")
    if min(args.repetitions, args.prompt_tokens, args.max_tokens) < 1:
        parser.error("repetitions and token counts must be positive")
    if args.ctx < args.prompt_tokens + 1024:
        parser.error("--ctx must leave at least 1024 tokens beyond the prompt")
    variants = [("baseline", 8192, 32, 512, False),
                ("bounded-8", 0, 8, 512, False),
                ("bounded-32", 0, 32, 512, False),
                ("cache-1024", 1024, 8, 512, False),
                ("batch-256", 8192, 32, 256, False),
                ("batch-1024", 8192, 32, 1024, False)]
    if args.profile == "m4":
        variants.append(("unified-2048", 2048, 32, 512, True))
    if args.variants:
        unknown = set(args.variants) - {v[0] for v in variants}
        if unknown:
            parser.error(f"unknown variants: {sorted(unknown)}")
        variants = [v for v in variants if v[0] in args.variants]
    args.output.mkdir(parents=True, exist_ok=False)
    metadata = {"profile": args.profile, "machine": command("sysctl", "-n", "machdep.cpu.brand_string", "hw.memsize"),
                "platform": platform.platform(), "runtime": command(args.server, "--version"),
                "model": str(args.model.resolve()), "model_bytes": args.model.stat().st_size,
                "ctx": args.ctx, "prompt_tokens": args.prompt_tokens, "max_tokens": args.max_tokens,
                "repetitions": args.repetitions, "results": []}
    try:
        for variant in variants:
            metadata["results"].append(run_variant(args, *variant))
            (args.output / "summary.json").write_text(json.dumps(metadata, indent=2) + "\n")
    finally:
        (args.output / "summary.json").write_text(json.dumps(metadata, indent=2) + "\n")


if __name__ == "__main__":
    main()
