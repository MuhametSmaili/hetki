#!/usr/bin/env python3
"""Measure isolated, warm picker content output and input-query echo on a PTY.

Requires Python 3.10+ and tmux. Build compatible binaries first, then run:
  go build -o /tmp/hetki-bench .
  python3 scripts/bench-tui-startup.py /tmp/hetki-bench --samples 30
  python3 scripts/bench-tui-startup.py /tmp/before /tmp/after --samples 30
  python3 -m unittest discover -s scripts -p 'test_bench_tui*.py'

Each binary owns a private tmux server, HOME and state/cache/config/temp paths.
A healthy same-build status daemon is prepared outside timing. No personal
sessions, configuration, history or daemons are used. A/B samples are shuffled.

content_ms: process launch to emitted header plus a known fixture row.
input_ms: writing a filter query to observing that query in picker output.
These are output/response observations, NOT physical painting, complete-frame,
status-readiness or cold-cache measurements. Fixed 120x32 fullscreen, flat view,
All scope, preview off; quiet cat panes. Shutdown and fixture setup are untimed.
"""

import argparse
from collections.abc import Callable, Iterator
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
import errno
import fcntl
import math
import os
from pathlib import Path
import pty
import random
import select
import shutil
import statistics
import struct
import subprocess
import termios
import time

from bench_tui_fixture import FixtureError, combine_cleanup_errors, isolated_fixture, process_exit, terminate_group

ROW_MARKER = b"bench-row-000-000"
INPUT_QUERY = b"zzinputprobe"


class MeasurementError(RuntimeError):
    """A sample or its owned-resource teardown could not be verified."""


@dataclass(frozen=True)
class Sample:
    content_ms: float
    input_ms: float


class TerminalOutput:
    """Keep bounded printable output, excluding CSI and control-string payloads.

    This is not a screen emulator: readiness means bytes were emitted, not that
    their cells survived later cursor movement or that a terminal painted them.
    """

    def __init__(self, limit: int = 1 << 20) -> None:
        self.limit = limit
        self.total = 0
        self.text = bytearray()
        self.state = "text"
        self.osc = False

    def feed(self, data: bytes) -> None:
        self.total += len(data)
        if self.total > self.limit:
            raise MeasurementError(f"picker output exceeds {self.limit} bytes")
        for value in data:
            self.consume(value)
        del self.text[:-65536]

    def consume(self, value: int) -> None:
        if self.state == "text":
            if value == 27:
                self.state = "escape"
            elif value >= 32 or value in (9, 10, 13):
                self.text.append(value)
        elif self.state == "escape":
            if value == ord("["):
                self.state = "csi"
            elif value in b"]PX^_":
                self.osc = value == ord("]")
                self.state = "string"
            elif not 32 <= value <= 47:
                self.state = "text"
        elif self.state == "csi":
            if 64 <= value <= 126:
                self.state = "text"
        elif self.state == "string":
            if value == 27:
                self.state = "string_escape"
            elif value == 7 and self.osc:
                self.state = "text"
        elif self.state == "string_escape":
            self.state = "text" if value == ord("\\") else "string"

    def content_ready(self, marker: bytes) -> bool:
        return all(part in self.text for part in (b">", b"All", b"Tracked", marker))


def read_pty(master: int, timeout: float) -> bytes | None:
    if not select.select([master], [], [], max(0, timeout))[0]:
        return None
    try:
        return os.read(master, 65536)
    except OSError as error:
        if error.errno == errno.EIO:
            return b""
        raise


def wait_for_output(master: int, process: subprocess.Popen, output: TerminalOutput,
                    ready: Callable[[], bool], deadline: float, description: str) -> None:
    while not ready():
        exit_status = process_exit(process)
        if exit_status is not None:
            raise MeasurementError(f"picker exited before {description}; exit={exit_status}; output={bytes(output.text[-300:])!r}")
        remaining = deadline - time.perf_counter()
        if remaining <= 0:
            raise MeasurementError(f"timed out waiting for {description}; output={bytes(output.text[-300:])!r}")
        data = read_pty(master, min(0.05, remaining))
        if data is None:
            continue
        if not data:
            raise MeasurementError(f"picker closed before {description}; exit={process_exit(process)}; output={bytes(output.text[-300:])!r}")
        output.feed(data)


def finish_picker(process: subprocess.Popen, master: int) -> None:
    forced = False
    try:
        if process_exit(process) is None:
            try:
                os.write(master, b"\x03")
            except OSError as error:
                if error.errno != errno.EIO:
                    raise
            deadline = time.perf_counter() + 2
            while process_exit(process) is None and time.perf_counter() < deadline:
                read_pty(master, min(0.05, deadline - time.perf_counter()))
        forced = process_exit(process) is None
    finally:
        terminate_group(process)
    if forced:
        raise MeasurementError("picker required forced shutdown; sample rejected")
    if process.returncode != 0:
        raise MeasurementError(f"picker exited with status {process.returncode}")


def same_terminal_settings(before: list, after: list) -> bool:
    before, after = before.copy(), after.copy()
    # The kernel may set PENDIN when canonical input is restored; it is not a
    # changed terminal setting. Keep every other flag and control character.
    before[3] &= ~getattr(termios, "PENDIN", 0)
    after[3] &= ~getattr(termios, "PENDIN", 0)
    return before == after


@contextmanager
def picker_process(binary: Path, environment: dict[str, str]) -> Iterator[tuple[subprocess.Popen, int, float]]:
    if not hasattr(os, "waitid") or not hasattr(os, "WNOWAIT"):
        raise MeasurementError("safe process-group cleanup requires POSIX waitid/WNOWAIT")
    master, slave = pty.openpty()
    process = None
    before = None
    failure = None
    try:
        before = termios.tcgetattr(slave)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 32, 120, 0, 0))
        started = time.perf_counter()
        process = subprocess.Popen(
            [str(binary), "--height", "100%", "--border", "none", "--view", "flat", "--scope", "all", "--preview=false"],
            stdin=slave, stdout=slave, stderr=slave, env=environment,
            cwd=environment.get("HOME"), start_new_session=True,
        )
        yield process, master, started
    except BaseException as error:
        failure = error
    finally:
        if process is not None:
            try:
                finish_picker(process, master)
                if not same_terminal_settings(before, termios.tcgetattr(slave)):
                    raise MeasurementError("picker did not restore terminal state; sample rejected")
            except BaseException as error:
                failure = combine_cleanup_errors(failure, error, MeasurementError)
        if before is not None:
            try:
                termios.tcsetattr(slave, termios.TCSANOW, before)
            except BaseException as error:
                failure = combine_cleanup_errors(failure, error, MeasurementError)
        for descriptor in (slave, master):
            try:
                os.close(descriptor)
            except BaseException as error:
                failure = combine_cleanup_errors(failure, error, MeasurementError)
    if failure is not None:
        raise failure


def measure_startup(binary: Path, environment: dict[str, str], timeout: float = 5) -> Sample:
    with picker_process(binary, environment) as (process, master, started):
        output = TerminalOutput()
        wait_for_output(master, process, output, lambda: output.content_ready(ROW_MARKER), started + timeout, "initial content")
        content_ms = (time.perf_counter() - started) * 1000

        if termios.tcgetattr(master)[3] & termios.ECHO:
            raise MeasurementError("terminal echo is enabled; cannot verify picker input response")
        input_started = time.perf_counter()
        os.write(master, INPUT_QUERY)
        # Incremental renderers may emit only the changed query, not its prompt.
        wait_for_output(master, process, output, lambda: INPUT_QUERY in output.text, input_started + timeout, "input echo")
        input_ms = (time.perf_counter() - input_started) * 1000
    return Sample(content_ms, input_ms)


def executable(path: str | Path) -> Path:
    resolved = Path(path).resolve()
    if not resolved.is_file() or not os.access(resolved, os.X_OK):
        raise argparse.ArgumentTypeError(f"not an executable: {path}")
    return resolved


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("binaries", nargs="+", type=executable)
    parser.add_argument("--tmux", type=executable, default=shutil.which("tmux"))
    parser.add_argument("--samples", type=int, default=30)
    parser.add_argument("--warmups", type=int, default=5)
    parser.add_argument("--sessions", type=int, default=20)
    parser.add_argument("--windows", type=int, default=1)
    parser.add_argument("--timeout", type=float, default=5)
    args = parser.parse_args()
    if args.tmux is None:
        parser.error("tmux is required; use --tmux /path/to/tmux")
    args.tmux = executable(args.tmux)
    if not 1 <= len(args.binaries) <= 4:
        parser.error("measure between one and four binaries")
    if not 1 <= args.samples <= 10000 or not 0 <= args.warmups <= 1000:
        parser.error("samples must be 1..10000 and warmups 0..1000")
    if not 1 <= args.sessions <= 128 or not 1 <= args.windows <= 100 or args.sessions * args.windows * len(args.binaries) > 1000:
        parser.error("fixture requires 1..128 sessions, 1..100 windows, at most 1000 panes across all binaries")
    if not math.isfinite(args.timeout) or not 0 < args.timeout <= 60:
        parser.error("timeout must be finite and between 0 and 60 seconds")
    return args


def report(binary: Path, samples: list[Sample]) -> None:
    for metric in ("content_ms", "input_ms"):
        values = sorted(getattr(sample, metric) for sample in samples)
        p95 = values[math.ceil(len(values) * 0.95) - 1]
        print(f"{binary}: {metric} n={len(values)} min={values[0]:.2f} median={statistics.median(values):.2f} p95={p95:.2f} max={values[-1]:.2f}")


def main() -> None:
    args = parse_args()
    print(f"Fixture: {args.sessions} sessions x {args.windows} windows, quiet panes; warm same-build daemon, fullscreen 120x32, preview off")
    print("Metrics observe emitted content/query echo, not terminal painting, cold caches or status readiness.")
    with ExitStack() as stack:
        fixtures = [stack.enter_context(isolated_fixture(binary, args.tmux, args.sessions, args.windows)) for binary in args.binaries]
        for fixture in fixtures:
            sample = measure_startup(fixture.binary, fixture.environment, args.timeout)
            print(f"{fixture.binary}: first measured content_ms={sample.content_ms:.2f} input_ms={sample.input_ms:.2f} (not cold-cache)")
            for _ in range(args.warmups):
                measure_startup(fixture.binary, fixture.environment, args.timeout)
        order = list(range(len(fixtures))) * args.samples
        random.Random(42).shuffle(order)
        results = [[] for _ in fixtures]
        for index in order:
            fixture = fixtures[index]
            results[index].append(measure_startup(fixture.binary, fixture.environment, args.timeout))
    for fixture, samples in zip(fixtures, results):
        report(fixture.binary, samples)
    print("Private pickers, daemons, servers and fixture directories cleaned up.")


if __name__ == "__main__":
    try:
        main()
    except (MeasurementError, FixtureError, subprocess.TimeoutExpired, OSError) as error:
        raise SystemExit(str(error)) from error
