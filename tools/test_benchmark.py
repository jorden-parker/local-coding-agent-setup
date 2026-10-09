import io
import json
import unittest
from unittest.mock import patch

import benchmark


def stream(*events):
    return io.BytesIO(b"".join(b"data: " + json.dumps(event).encode() + b"\n\n" for event in events)
                      + b"data: [DONE]\n\n")


class CompletionTest(unittest.TestCase):
    def test_stream_timings_and_fragmented_tool_call(self):
        events = stream(
            {"choices": [{"delta": {"tool_calls": [{"index": 0, "id": "call-1",
                "function": {"name": "read_file", "arguments": '{"path":'}}]}}]},
            {"choices": [{"delta": {"tool_calls": [{"index": 0,
                "function": {"arguments": '"sample.py"}'}}]}}]},
            {"choices": [], "usage": {"completion_tokens": 9},
             "timings": {"cache_n": 42, "predicted_per_second": 8.5}},
        )
        with patch.object(benchmark, "post", return_value=events):
            result, text, calls = benchmark.completion("unused", [], 16, benchmark.TOOLS)
        self.assertEqual(result["timings"]["cache_n"], 42)
        self.assertEqual(result["usage"]["completion_tokens"], 9)
        self.assertIsNotNone(result["ttft_ms"])
        self.assertEqual(text, "")
        self.assertEqual(json.loads(calls[0]["function"]["arguments"]), {"path": "sample.py"})
        self.assertEqual(calls[0]["id"], "call-1")

    def test_missing_timings_are_not_reported_as_decode_speed(self):
        with patch.object(benchmark, "post", return_value=stream({"choices": [{"delta": {"content": "ok"}}]})):
            with self.assertRaisesRegex(RuntimeError, "No server timings"):
                benchmark.completion("unused", [], 16)

    def test_stream_error_is_not_a_successful_measurement(self):
        with patch.object(benchmark, "post", return_value=stream({"error": "context full"})):
            with self.assertRaisesRegex(RuntimeError, "context full"):
                benchmark.completion("unused", [], 16)


if __name__ == "__main__":
    unittest.main()
