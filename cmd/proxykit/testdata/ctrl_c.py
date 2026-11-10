"""Linux PTY regression driver; only disposable test fixtures are touched."""
import errno
import os
from pathlib import Path
import pty
import select
import signal
import sys
import time

store, phase = Path(sys.argv[1]), sys.argv[2]
pid, terminal = pty.fork()
if pid == 0:
    os.execvp(sys.argv[3], sys.argv[3:])

output = bytearray()
reaped = False


def pump():
    if select.select([terminal], [], [], 0.02)[0]:
        try:
            output.extend(os.read(terminal, 65536))
        except OSError as error:
            if error.errno != errno.EIO:
                raise


def until(predicate, description):
    deadline = time.monotonic() + 30
    while not predicate():
        if time.monotonic() > deadline:
            raise AssertionError(description + '\n' + output.decode(errors='replace'))
        pump()


def alive(pid):
    try:
        # Zombies are terminated, though an orphan's reaping belongs to PID 1.
        return Path(f'/proc/{pid}/stat').read_text().split(') ')[1][0] != 'Z'
    except (FileNotFoundError, ProcessLookupError):
        return False


try:
    until(lambda: (store / 'acquired').exists(), 'setup did not acquire lock')
    worker = int((store / 'worker.pid').read_text())
    descendant = int((store / 'descendant.pid').read_text())
    assert os.getpgid(worker) == worker, 'worker is not isolated'
    if phase.endswith('ready'):
        until(lambda: b'listening on' in output, 'CLI not listening')
    # The terminal driver generates SIGINT for the entire foreground group,
    # including go run and the compiled CLI, but not the private worker group.
    if phase.startswith('parent-death'):
        os.kill(pid, signal.SIGKILL)
    else:
        os.write(terminal, b'\x03')
    until(lambda: (store / 'cleaned').exists(), 'cleanup did not complete')
    assert not (store / 'fixture.lock').exists(), 'dead fixture lock'
    until(lambda: not alive(worker) and not alive(descendant), 'worker/descendant survived')

    def exited():
        global reaped
        reaped = os.waitpid(pid, os.WNOHANG)[0] == pid
        return reaped

    until(exited, 'CLI/go-run did not exit')
finally:
    for name in ['worker.pid', 'descendant.pid']:
        try:
            target = int((store / name).read_text())
            if alive(target):
                os.killpg(target, signal.SIGKILL) if name == 'worker.pid' else os.kill(target, signal.SIGKILL)
        except (FileNotFoundError, ProcessLookupError):
            pass
    if not reaped:
        try:
            os.killpg(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        os.waitpid(pid, 0)
    os.close(terminal)
