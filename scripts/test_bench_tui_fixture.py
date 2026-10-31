"""Ownership and bounded control-command tests for the startup fixture."""

import fcntl
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import bench_tui_fixture as fixtures


class CleanupErrorTests(unittest.TestCase):
    def test_single_cleanup_error_keeps_original_cause(self):
        cause = OSError("underlying failure")
        error = fixtures.CleanupError("unsafe cleanup")
        error.__cause__ = cause

        combined = fixtures.combine_cleanup_errors(None, error)

        self.assertIs(combined, error)
        self.assertIs(combined.__cause__, cause)

    def test_secondary_error_keeps_cleanup_classification_and_primary_cause(self):
        primary = fixtures.CleanupError("helpers may be alive")
        secondary = OSError("terminal restore failed")

        combined = fixtures.combine_cleanup_errors(primary, secondary)

        self.assertIsInstance(combined, fixtures.CleanupError)
        self.assertIs(combined.__cause__, primary)
        self.assertIn("helpers may be alive", str(combined))
        self.assertIn("terminal restore failed", str(combined))


class FixtureEnvironmentTests(unittest.TestCase):
    def test_ignores_parent_tmux_settings_and_uses_private_paths(self):
        with tempfile.TemporaryDirectory(prefix="ht-env-", dir="/tmp") as directory:
            root = Path(directory)

            with mock.patch.dict(os.environ, {
                "TMUX": "/personal/tmux.sock,123,7", "TMUX_PANE": "%99",
                "HOME": "/personal/home", "XDG_CONFIG_HOME": "/personal/config",
                "SHELL": "/personal/shell", "BASH_ENV": "/personal/startup.sh",
            }):
                environment = fixtures.fixture_environment(root)

            self.assertEqual(environment["HOME"], str(root / "home"))
            for key in ["XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "TMPDIR"]:
                self.assertTrue(Path(environment[key]).is_relative_to(root), key)
            self.assertEqual(environment["TMUX"], f"{root / 'tmux.sock'},0,0")
            self.assertNotIn("TMUX_PANE", environment)
            self.assertNotIn("SHELL", environment)
            self.assertNotIn("BASH_ENV", environment)
            self.assertEqual(environment["PATH"], str(root / "bin") + os.pathsep + os.defpath)


class FixtureLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.fail_setup = False
        self.fail_stop = False
        patcher = mock.patch.object(fixtures.Fixture, "command", autospec=True, side_effect=self.command)
        patcher.start()
        self.addCleanup(patcher.stop)

    def command(self, fixture, arguments):
        self.calls.append((fixture.root, arguments.copy(), fixture.environment.copy()))
        if "new-session" in arguments and self.fail_setup:
            raise fixtures.FixtureError("setup failed after attempting server creation")
        if arguments[-2:] == ["service", "stop"] and self.fail_stop:
            raise fixtures.FixtureError("cannot stop fixture daemon")
        if "list-panes" in arguments:
            return "%0\n"
        if "display-message" in arguments:
            return "123|$0\n"
        return ""

    def fixture(self):
        binary = Path(sys.executable)
        return fixtures.isolated_fixture(binary, binary, 1, 1)

    def test_success_stops_owned_service_and_server_before_removing_files(self):
        with self.fixture() as fixture:
            root = fixture.root
            self.assertEqual(root.stat().st_mode & 0o777, 0o700)

        self.assertFalse(root.exists())
        self.assertEqual(self.calls[-2][1][-2:], ["service", "stop"])
        self.assertEqual(self.calls[-1][1][-1], "kill-server")
        for owned_root, arguments, environment in self.calls:
            self.assertEqual(owned_root, root)
            self.assertTrue(environment["TMUX"].startswith(str(root / "tmux.sock") + ","))
            if "-S" in arguments:
                self.assertEqual(arguments[arguments.index("-S") + 1], str(root / "tmux.sock"))

    def test_partial_setup_still_attempts_owned_server_teardown(self):
        self.fail_setup = True

        with self.assertRaisesRegex(fixtures.FixtureError, "setup failed"):
            with self.fixture():
                self.fail("setup must fail before yielding a fixture")

        root = self.calls[0][0]
        self.assertEqual(self.calls[-1][1][-1], "kill-server")
        self.assertFalse(root.exists())

    def test_held_daemon_lock_prevents_fixture_deletion(self):
        with self.assertRaisesRegex(fixtures.FixtureError, "daemon still owns"):
            with mock.patch.object(fixtures, "LOCK_WAIT_SECONDS", 0):
                with self.fixture() as fixture:
                    root = fixture.root
                    self.addCleanup(shutil.rmtree, root)
                    lock = (root / "held.sock.lock").open("wb")
                    self.addCleanup(lock.close)
                    fcntl.flock(lock.fileno(), fcntl.LOCK_EX)

        self.assertTrue(root.is_dir())
        self.assertEqual(self.calls[-1][1][-1], "kill-server")

    def test_released_daemon_lock_allows_fixture_deletion(self):
        with self.fixture() as fixture:
            root = fixture.root
            with (root / "released.sock.lock").open("wb") as lock:
                fcntl.flock(lock.fileno(), fcntl.LOCK_EX)

        self.assertFalse(root.exists())

    def test_unverified_picker_cleanup_preserves_fixture(self):
        with self.assertRaisesRegex(fixtures.CleanupError, "fixture preserved"):
            with self.fixture() as fixture:
                root = fixture.root
                self.addCleanup(shutil.rmtree, root)
                raise fixtures.CleanupError("picker helpers could not be verified")

        self.assertTrue(root.is_dir())

    def test_failed_teardown_preserves_fixture_for_diagnosis(self):
        self.fail_stop = True

        with self.assertRaisesRegex(fixtures.FixtureError, "preserved"):
            with self.fixture() as fixture:
                root = fixture.root
                self.addCleanup(shutil.rmtree, root)

        self.assertTrue(root.is_dir())
        self.assertEqual(self.calls[-1][1][-1], "kill-server")


class ControlCommandTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix="ht-control-", dir="/tmp")
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)

    def helper(self, body):
        path = self.root / "control"
        path.write_text(
            f"#!{sys.executable}\nimport os, signal, sys\nfrom pathlib import Path\n"
            f"Path({str(self.root / 'pid')!r}).write_text(str(os.getpid()))\n" + body + "\n",
        )
        path.chmod(0o700)
        return [str(path)]

    def invoke(self, arguments):
        return fixtures.capture_command(arguments, fixtures.fixture_environment(self.root), self.root)

    def assert_reaped(self):
        pid = int((self.root / "pid").read_text())
        with self.assertRaises(ChildProcessError):
            os.waitpid(pid, os.WNOHANG)

    def test_collects_successful_control_output_and_reaps(self):
        arguments = self.helper("os.write(1, b'ready\\n')\nos.write(2, b'diagnostic\\n')")

        output = self.invoke(arguments)

        self.assertEqual(output, "ready\ndiagnostic\n")
        self.assert_reaped()

    def test_stdout_close_failure_keeps_group_cleanup_uncertainty(self):
        process = mock.Mock()
        cleanup_error = fixtures.CleanupError("group cleanup unverified")
        process.stdout.close.side_effect = OSError("stdout close failed")

        with (
            mock.patch.object(fixtures.subprocess, "Popen", return_value=process),
            mock.patch.object(fixtures.select, "select", return_value=([process.stdout], [], [])),
            mock.patch.object(fixtures.os, "read", return_value=b""),
            mock.patch.object(fixtures, "wait_for_exit", return_value=0),
            mock.patch.object(fixtures, "terminate_group", side_effect=cleanup_error),
        ):
            with self.assertRaises(fixtures.CleanupError) as caught:
                self.invoke(["fixture-control"])

        self.assertIs(caught.exception.__cause__, cleanup_error)
        self.assertIn("group cleanup unverified", str(caught.exception))
        self.assertIn("stdout close failed", str(caught.exception))
        process.stdout.close.assert_called_once()

    def test_combined_command_and_cleanup_failure_keeps_diagnostics(self):
        arguments = self.helper("os.write(2, b'fixture diagnostic')\nsys.exit(7)")
        terminate = fixtures.terminate_group

        def fail_cleanup(process):
            terminate(process)
            raise fixtures.CleanupError("cleanup verification failed")

        with mock.patch.object(fixtures, "terminate_group", side_effect=fail_cleanup):
            with self.assertRaisesRegex(fixtures.CleanupError, r"failed \(7\).*fixture diagnostic.*cleanup:.*verification"):
                self.invoke(arguments)

        self.assert_reaped()

    def test_bounds_noisy_control_output_and_reaps(self):
        arguments = self.helper("os.write(1, b'x' * 4096)\nsignal.pause()")

        with mock.patch.object(fixtures, "CONTROL_OUTPUT_BYTES", 128):
            with self.assertRaisesRegex(fixtures.FixtureError, "output exceeds 128 bytes.*tail="):
                self.invoke(arguments)

        self.assert_reaped()

    def test_bounds_stalled_control_command_and_reaps(self):
        arguments = self.helper("signal.pause()")

        with mock.patch.object(fixtures, "CONTROL_TIMEOUT_SECONDS", 2):
            with self.assertRaisesRegex(fixtures.FixtureError, "timed out"):
                self.invoke(arguments)

        self.assert_reaped()


class GroupCleanupTests(unittest.TestCase):
    def test_classifies_group_snapshot_and_filters_other_groups(self):
        for output, expected in [(b"42 Z\n", False), (b"42 S\n", True), (b"99 S\n42 Z\n", False)]:
            with self.subTest(output=output):
                def run(arguments, **options):
                    options["stdout"].write(output)
                    return subprocess.CompletedProcess(arguments, 0)

                with mock.patch.object(fixtures.subprocess, "run", side_effect=run):
                    self.assertEqual(fixtures.group_has_live_members(42), expected)

    def test_reports_unexpected_group_inspection_failure(self):
        with mock.patch.object(fixtures.subprocess, "run", return_value=subprocess.CompletedProcess([], 2)):
            with self.assertRaisesRegex(fixtures.CleanupError, "ps failed"):
                fixtures.group_has_live_members(42)

    def test_permission_error_is_only_accepted_for_exited_zombie_group(self):
        for exited, live, accepted in [(0, False, True), (0, True, False), (None, False, False)]:
            with self.subTest(exited=exited, live=live):
                process = mock.Mock(pid=42)
                with mock.patch.object(fixtures.os, "killpg", side_effect=PermissionError("denied")), \
                     mock.patch.object(fixtures, "process_exit", return_value=exited), \
                     mock.patch.object(fixtures, "group_has_live_members", return_value=live):
                    if accepted:
                        fixtures.terminate_group(process)
                    else:
                        with self.assertRaisesRegex(fixtures.CleanupError, "cannot stop owned"):
                            fixtures.terminate_group(process)
                process.wait.assert_called_once()

    def test_live_helpers_after_signal_are_reported(self):
        process = mock.Mock(pid=42)
        with mock.patch.object(fixtures.os, "killpg"), \
             mock.patch.object(fixtures, "group_has_live_members", return_value=True), \
             mock.patch.object(fixtures, "GROUP_WAIT_SECONDS", 0):
            with self.assertRaisesRegex(fixtures.CleanupError, "still has live helpers"):
                fixtures.terminate_group(process)
        process.wait.assert_called_once()


if __name__ == "__main__":
    unittest.main()
