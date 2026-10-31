#!/usr/bin/env python3
"""Behavior checks for the isolated startup measurement tool (stdlib only)."""

import errno
import importlib.util
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import bench_tui_fixture as fixtures

SPEC = importlib.util.spec_from_file_location(
    "bench_tui_startup", Path(__file__).with_name("bench-tui-startup.py"),
)
bench = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = bench
SPEC.loader.exec_module(bench)


class OutputTests(unittest.TestCase):
    def test_requires_fixture_content_and_current_header(self):
        output = bench.TerminalOutput()

        output.feed(b"FILTER\r\nAll  Tracked > ")

        self.assertFalse(output.content_ready(b"bench-row-000-000"))

        output.feed(b"\r\nbench-row-000-000")

        self.assertTrue(output.content_ready(b"bench-row-000-000"))

    def test_ignores_control_string_payload_across_reads(self):
        output = bench.TerminalOutput()

        for chunk in [b"\x1b]2;All Tracked > bench-", b"row-000-000\x1b", b"\\"]:
            output.feed(chunk)

        self.assertFalse(output.content_ready(b"bench-row-000-000"))

        for chunk in [b"\x1b[1", b"m> \x1b[0m All Tracked\r\n", b"bench-row-000-000"]:
            output.feed(chunk)

        self.assertTrue(output.content_ready(b"bench-row-000-000"))

    def test_bounds_output_even_when_it_contains_only_escape_payload(self):
        output = bench.TerminalOutput(limit=32)

        with self.assertRaisesRegex(bench.MeasurementError, "output exceeds"):
            output.feed(b"\x1b]" + b"x" * 33)


class ProbeTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="ht-probe-", dir="/tmp")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)

    def helper(self, body):
        executable = self.root / "picker"
        executable.write_text(
            f"#!{sys.executable}\n"
            "import os, sys, termios, tty\n"
            f"with open({str(self.root / 'pid')!r}, 'w') as pid:\n"
            "    pid.write(str(os.getpid()))\n"
            "before = termios.tcgetattr(0)\n"
            "tty.setraw(0)\n"
            "try:\n" + "".join(f"    {line}\n" for line in body.splitlines()) +
            "finally:\n    termios.tcsetattr(0, termios.TCSANOW, before)\n",
        )
        executable.chmod(0o700)
        return executable

    def assert_reaped(self):
        pid = int((self.root / "pid").read_text())
        with self.assertRaises(ChildProcessError):
            os.waitpid(pid, os.WNOHANG)

    def test_measures_current_content_and_input_then_quits_cleanly(self):
        executable = self.helper(
            "os.write(1, b'\\x1b[1m> \\x1b[0mAll  Tracked\\r\\nbench-row-000-000')\n"
            "query = os.read(0, 128)\n"
            "os.write(1, query)\n"
            "assert os.read(0, 1) == b'\\x03'",
        )

        sample = bench.measure_startup(executable, os.environ.copy(), timeout=2)

        self.assertGreaterEqual(sample.content_ms, 0)
        self.assertGreaterEqual(sample.input_ms, 0)

    def test_times_out_on_header_without_fixture_row_and_reaps_child(self):
        executable = self.helper(
            "os.write(1, b'> All Tracked FILTER')\n"
            "assert os.read(0, 1) == b'\\x03'",
        )

        with self.assertRaisesRegex(bench.MeasurementError, "initial content"):
            bench.measure_startup(executable, os.environ.copy(), timeout=2)

        self.assert_reaped()

    def test_rejects_missing_input_echo_and_reaps_child(self):
        executable = self.helper(
            "os.write(1, b'> All Tracked bench-row-000-000')\n"
            "os.read(0, 128)\n"
            "assert os.read(0, 1) == b'\\x03'",
        )

        with self.assertRaisesRegex(bench.MeasurementError, "input echo"):
            bench.measure_startup(executable, os.environ.copy(), timeout=2)

        self.assert_reaped()

    def test_rejects_forced_shutdown_and_reaps_child(self):
        executable = self.helper(
            "os.write(1, b'> All Tracked bench-row-000-000')\n"
            "query = os.read(0, 128)\n"
            "os.write(1, query)\n"
            "while True:\n    os.read(0, 128)",
        )

        with self.assertRaisesRegex(bench.MeasurementError, "forced shutdown"):
            bench.measure_startup(executable, os.environ.copy(), timeout=2)

        self.assert_reaped()

    def test_reports_early_exit_instead_of_accepting_header(self):
        executable = self.helper("os.write(1, b'> All Tracked')\nsys.exit(7)")

        with self.assertRaisesRegex(bench.MeasurementError, "exited|closed"):
            bench.measure_startup(executable, os.environ.copy(), timeout=2)

    def test_failed_teardown_stops_helpers_after_leader_exit(self):
        unrelated = subprocess.Popen(
            [sys.executable, "-c", "import signal; signal.pause()"],
            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            start_new_session=True,
        )
        self.addCleanup(self.stop_unrelated, unrelated)
        for exit_status in [0, 7]:
            with self.subTest(exit_status=exit_status):
                endpoint = self.root / f"child-{exit_status}.sock"
                with socket.socket(socket.AF_UNIX) as listener:
                    listener.bind(str(endpoint))
                    listener.listen(1)
                    listener.settimeout(2)
                    executable = self.helper(
                        "import socket\n"
                        "read_fd, write_fd = os.pipe()\n"
                        "if os.fork() == 0:\n"
                        "    os.close(read_fd)\n"
                        "    client = socket.socket(socket.AF_UNIX)\n"
                        f"    client.connect({str(endpoint)!r})\n"
                        "    client.sendall(b'ready')\n"
                        "    os.write(write_fd, b'1')\n"
                        "    client.recv(1)\n"
                        "    os._exit(0)\n"
                        "os.close(write_fd)\n"
                        "assert os.read(read_fd, 1) == b'1'\n"
                        f"os._exit({exit_status})",
                    )

                    with self.assertRaisesRegex(bench.MeasurementError, "exited|terminal state"):
                        bench.measure_startup(executable, os.environ.copy(), timeout=2)

                    connection, _ = listener.accept()
                    with connection:
                        connection.settimeout(2)
                        self.assertEqual(connection.recv(5), b"ready")
                        self.assertEqual(connection.recv(1), b"", "same-group child must be terminated")
                    self.assertIsNone(unrelated.poll(), "unrelated process must stay alive")

    @staticmethod
    def stop_unrelated(process):
        if process.poll() is None:
            process.kill()
        process.wait(timeout=2)

    def test_rejects_kernel_echo_as_application_response(self):
        executable = self.helper(
            "settings = termios.tcgetattr(0)\n"
            "settings[3] |= termios.ECHO\n"
            "termios.tcsetattr(0, termios.TCSANOW, settings)\n"
            "os.write(1, b'> All Tracked bench-row-000-000')\n"
            "assert os.read(0, 1) == b'\\x03'",
        )

        with self.assertRaisesRegex(bench.MeasurementError, "terminal echo is enabled"):
            bench.measure_startup(executable, os.environ.copy(), timeout=2)

    def test_final_terminal_restore_error_preserves_uncertain_fixture(self):
        with self.assertRaises(fixtures.CleanupError) as caught:
            with mock.patch.object(fixtures.Fixture, "start"), mock.patch.object(fixtures.Fixture, "close"):
                with fixtures.isolated_fixture(Path(sys.executable), Path(sys.executable), 1, 1) as fixture:
                    root = fixture.root
                    with mock.patch.object(bench.subprocess, "Popen", return_value=mock.Mock()), \
                         mock.patch.object(bench, "finish_picker", side_effect=fixtures.CleanupError("helpers may be alive")), \
                         mock.patch.object(bench.termios, "tcsetattr", side_effect=OSError("terminal restore failed")):
                        with bench.picker_process(Path(sys.executable), os.environ.copy()):
                            pass

        if root.exists():
            self.addCleanup(shutil.rmtree, root)
        self.assertTrue(root.is_dir())
        self.assertIn("helpers may be alive", str(caught.exception))
        self.assertIn("terminal restore failed", str(caught.exception))

    def test_descriptor_close_errors_preserve_uncertainty_and_close_both_descriptors(self):
        real_close = os.close
        for failing_descriptor in ["slave", "master"]:
            with self.subTest(failing_descriptor=failing_descriptor):
                master, slave = bench.pty.openpty()
                failing_fd = slave if failing_descriptor == "slave" else master

                def close(fd):
                    real_close(fd)
                    if fd == failing_fd:
                        raise OSError("descriptor close failed")

                try:
                    with mock.patch.object(bench.pty, "openpty", return_value=(master, slave)), \
                         mock.patch.object(bench.subprocess, "Popen", return_value=mock.Mock()), \
                         mock.patch.object(bench, "finish_picker", side_effect=fixtures.CleanupError("helpers may be alive")), \
                         mock.patch.object(bench.os, "close", side_effect=close):
                        with self.assertRaises(fixtures.CleanupError) as caught:
                            with bench.picker_process(Path(sys.executable), os.environ.copy()):
                                pass

                    self.assertIn("helpers may be alive", str(caught.exception))
                    self.assertIn("descriptor close failed", str(caught.exception))
                    for fd in (master, slave):
                        with self.assertRaises(OSError) as closed:
                            os.fstat(fd)
                        self.assertEqual(closed.exception.errno, errno.EBADF)
                finally:
                    for fd in (master, slave):
                        try:
                            real_close(fd)
                        except OSError as error:
                            if error.errno != errno.EBADF:
                                raise

    def test_rejects_terminal_state_not_restored(self):
        executable = self.root / "picker"
        executable.write_text(
            f"#!{sys.executable}\nimport os, tty\ntty.setraw(0)\n"
            "os.write(1, b'> All Tracked bench-row-000-000')\n"
            "query = os.read(0, 128)\nos.write(1, b'> ' + query)\n"
            "os.read(0, 1)\n",
        )
        executable.chmod(0o700)

        with self.assertRaisesRegex(bench.MeasurementError, "terminal state"):
            bench.measure_startup(executable, os.environ.copy(), timeout=2)


if __name__ == "__main__":
    unittest.main()
