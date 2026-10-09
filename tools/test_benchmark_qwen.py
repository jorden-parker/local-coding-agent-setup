import json
import unittest

import benchmark_qwen


class ToolTraceTest(unittest.TestCase):
    def test_execution_claim_does_not_count_as_a_tool_result(self):
        events = [{"type": "system", "subtype": "init", "tools": ["run_shell_command"]},
                  {"type": "assistant", "content": "I ran all three tests successfully."},
                  {"type": "result", "stats": {"tools": {"byName": {}}}}]
        available, stats = benchmark_qwen.tool_trace(json.dumps(events, separators=(",", ":")))
        self.assertIn("run_shell_command", available)
        self.assertEqual(stats.get("run_shell_command", {}).get("success", 0), 0)

    def test_warning_prefix_and_recorded_results(self):
        events = [{"type": "system", "subtype": "init", "tools": ["tool_search"]},
                  {"type": "result", "stats": {"tools": {"byName": {
                      "run_shell_command": {"success": 1}, "tool_search": {"success": 1}}}}}]
        available, stats = benchmark_qwen.tool_trace(
            "Warning: fixture\n" + json.dumps(events, separators=(",", ":")))
        self.assertEqual(available, ["tool_search"])
        self.assertEqual(stats["run_shell_command"]["success"], 1)
        self.assertEqual(stats["tool_search"]["success"], 1)

    def test_incomplete_log_cannot_pass(self):
        self.assertEqual(benchmark_qwen.tool_trace('[{"type":"system"'), ([], {}))


if __name__ == "__main__":
    unittest.main()
