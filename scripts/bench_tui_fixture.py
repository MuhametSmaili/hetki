"""Private process, tmux/server and daemon ownership for the startup benchmark."""

from collections.abc import Iterator
from contextlib import contextmanager
import fcntl
import os
from pathlib import Path
import select
import shutil
import signal
import subprocess
import tempfile
import time

CONTROL_OUTPUT_BYTES = 65536
CONTROL_TIMEOUT_SECONDS = 15.0
GROUP_WAIT_SECONDS = 2.0
LOCK_WAIT_SECONDS = 3.0


class FixtureError(RuntimeError):
    """Fixture setup or verified teardown failed."""


class CleanupError(FixtureError):
    """Resources may still be in use; preserve their fixture directory."""


def combine_cleanup_errors(primary: BaseException | None, cleanup: BaseException,
                           error_type: type[Exception] = FixtureError) -> BaseException:
    """Retain diagnostics and cleanup uncertainty through subsequent failures."""
    if primary is None:
        return cleanup
    if isinstance(primary, CleanupError) or isinstance(cleanup, CleanupError):
        error_type = CleanupError
    combined = error_type(f"{primary}; cleanup: {cleanup}")
    combined.__cause__ = primary
    return combined


def process_exit(process: subprocess.Popen) -> int | None:
    # Reserving the unreaped leader PID prevents group ID reuse during cleanup.
    result = os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
    if result is None:
        return None
    return result.si_status if result.si_code == os.CLD_EXITED else -result.si_status


def group_has_live_members(group: int) -> bool:
    # Numeric -g selects the new leader's group on BSD or its session on GNU ps;
    # filter PGID so only its owned group is considered. Never capture commands.
    with tempfile.TemporaryFile() as output:
        result = subprocess.run(
            ["/bin/ps", "-g", str(group), "-o", "pgid=,stat="],
            stdout=output, stderr=subprocess.STDOUT, timeout=2,
            env={"PATH": os.defpath, "LC_ALL": "C"},
        )
        output.seek(0)
        data = output.read(CONTROL_OUTPUT_BYTES + 1)
    if result.returncode not in (0, 1) or len(data) > CONTROL_OUTPUT_BYTES:
        raise CleanupError("cannot verify picker group: ps failed or exceeded output limit")
    for line in data.decode("ascii", errors="replace").splitlines():
        fields = line.split()
        if len(fields) != 2 or not fields[0].isdigit():
            raise CleanupError(f"cannot verify picker group: unexpected ps row {line!r}")
        if int(fields[0]) == group and not fields[1].startswith("Z"):
            return True
    return False


def terminate_group(process: subprocess.Popen) -> None:
    try:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        except PermissionError:
            # Darwin reports EPERM for zombie-only groups. Other permission
            # failures remain errors, even after the leader has exited.
            if process_exit(process) is None or group_has_live_members(process.pid):
                raise
        deadline = time.perf_counter() + GROUP_WAIT_SECONDS
        while group_has_live_members(process.pid):
            if time.perf_counter() >= deadline:
                raise CleanupError("owned process group still has live helpers")
            time.sleep(0.01)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise CleanupError(f"cannot stop owned process group: {error}") from error
    finally:
        try:
            process.wait(timeout=2)
        except subprocess.TimeoutExpired as error:
            raise CleanupError("owned process leader did not terminate") from error


def wait_for_exit(process: subprocess.Popen, deadline: float) -> int:
    while (status := process_exit(process)) is None:
        remaining = deadline - time.perf_counter()
        if remaining <= 0:
            raise FixtureError("fixture command timed out")
        time.sleep(min(0.01, remaining))
    return status


def capture_command(arguments: list[str], environment: dict[str, str], directory: Path) -> str:
    if not hasattr(os, "waitid") or not hasattr(os, "WNOWAIT"):
        raise FixtureError("safe process cleanup requires POSIX waitid/WNOWAIT")
    process = subprocess.Popen(
        arguments, env=environment, cwd=directory, stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True,
    )
    output = bytearray()
    result = ""
    failure = None
    try:
        deadline = time.perf_counter() + CONTROL_TIMEOUT_SECONDS
        while True:
            remaining = deadline - time.perf_counter()
            if remaining <= 0:
                raise FixtureError(f"fixture command timed out: {arguments!r}; tail={bytes(output[-2048:])!r}")
            if not select.select([process.stdout], [], [], min(0.05, remaining))[0]:
                if process_exit(process) is not None:
                    raise FixtureError(f"fixture command left its output pipe open: {arguments!r}")
                continue
            data = os.read(process.stdout.fileno(), 4096)
            if not data:
                status = wait_for_exit(process, deadline)
                if status:
                    raise FixtureError(f"{arguments!r} failed ({status}); tail={bytes(output[-2048:])!r}")
                result = output.decode("utf-8", errors="replace")
                break
            output.extend(data)
            if len(output) > CONTROL_OUTPUT_BYTES:
                raise FixtureError(f"fixture command output exceeds {CONTROL_OUTPUT_BYTES} bytes: {arguments!r}; tail={bytes(output[-2048:])!r}")
    except BaseException as error:
        failure = error
    finally:
        try:
            terminate_group(process)
        except BaseException as error:
            failure = combine_cleanup_errors(failure, error)
        try:
            process.stdout.close()
        except BaseException as error:
            failure = combine_cleanup_errors(failure, error)
    if failure is not None:
        raise failure
    return result


def fixture_environment(root: Path) -> dict[str, str]:
    return {
        "HOME": str(root / "home"),
        "XDG_CONFIG_HOME": str(root / "config"),
        "XDG_CACHE_HOME": str(root / "cache"),
        "XDG_STATE_HOME": str(root / "state"),
        "XDG_RUNTIME_DIR": str(root / "runtime"),
        "TMPDIR": str(root / "tmp"),
        "PATH": str(root / "bin") + os.pathsep + os.defpath,
        "TERM": "xterm-256color",
        "LANG": "C.UTF-8",
        "TMUX": f"{root / 'tmux.sock'},0,0",
    }


class Fixture:
    def __init__(self, root: Path, binary: Path, tmux: Path) -> None:
        self.root, self.binary = root, binary
        self.environment = fixture_environment(root)
        self.tmux = [str(tmux), "-S", str(root / "tmux.sock"), "-f", str(root / "tmux.conf")]
        self.started = False

    def command(self, arguments: list[str]) -> str:
        return capture_command(arguments, self.environment, self.root)

    def start(self, sessions: int, windows: int) -> None:
        for name in ("home", "config", "cache", "state", "runtime", "tmp", "bin"):
            (self.root / name).mkdir(mode=0o700)
        (self.root / "bin" / "tmux").symlink_to(self.tmux[0])
        (self.root / "tmux.conf").write_text(
            'set -g default-shell /bin/sh\nset -g default-command "exec /bin/cat"\n'
            'set -g status off\nset -g history-limit 0\nset -g base-index 0\nset -g pane-base-index 0\n',
        )
        for session in range(sessions):
            name = f"bench-session-{session:03}"
            # A failed first client may already have created a server.
            self.started = True
            self.command(self.tmux + ["new-session", "-d", "-s", name, "-n", f"bench-row-{session:03}-000", "-c", self.environment["HOME"], "-x", "120", "-y", "32"])
            for window in range(1, windows):
                self.command(self.tmux + ["new-window", "-d", "-t", name + ":", "-n", f"bench-row-{session:03}-{window:03}", "-c", self.environment["HOME"]])
        rows = self.command(self.tmux + ["list-panes", "-a", "-F", "#{pane_id}"])
        if len(rows.splitlines()) != sessions * windows:
            raise FixtureError("private fixture pane count does not match requested inventory")
        pid, session_id = self.command(self.tmux + ["display-message", "-p", "-t", "bench-session-000:", "#{pid}|#{session_id}"]).strip().split("|")
        self.environment["TMUX"] = f"{self.root / 'tmux.sock'},{pid},{session_id.removeprefix('$')}"
        self.command([str(self.binary), "service", "start"])

    def close(self) -> None:
        if not self.started:
            return
        errors = []
        for arguments in ([str(self.binary), "service", "stop"], self.tmux + ["kill-server"]):
            try:
                self.command(arguments)
            except (FixtureError, subprocess.TimeoutExpired, OSError) as error:
                errors.append(str(error))
        try:
            self.wait_unlocked()
        except FixtureError as error:
            errors.append(str(error))
        if errors:
            raise CleanupError(f"fixture teardown failed; preserved {self.root}: {'; '.join(errors)}")

    def wait_unlocked(self) -> None:
        deadline = time.perf_counter() + LOCK_WAIT_SECONDS
        for path in self.root.rglob("*.sock.lock"):
            with path.open("rb") as lock:
                while True:
                    try:
                        fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                        fcntl.flock(lock.fileno(), fcntl.LOCK_UN)
                        break
                    except BlockingIOError:
                        if time.perf_counter() >= deadline:
                            raise CleanupError(f"daemon still owns {path}") from None
                        time.sleep(0.01)


@contextmanager
def isolated_fixture(binary: Path, tmux: Path, sessions: int, windows: int) -> Iterator[Fixture]:
    root = Path(tempfile.mkdtemp(prefix="htb-", dir="/tmp"))
    fixture = Fixture(root, binary, tmux)
    failure = None
    try:
        fixture.start(sessions, windows)
        yield fixture
    except BaseException as error:
        failure = error
        raise
    finally:
        try:
            fixture.close()
        except (FixtureError, OSError, subprocess.TimeoutExpired) as error:
            raise combine_cleanup_errors(failure, error, CleanupError)
        if isinstance(failure, CleanupError):
            raise CleanupError(f"{failure}; fixture preserved at {root}") from failure
        shutil.rmtree(root)
