"""Keep a tool loaded in memory between runs (maggot "warm" mode).

Copy this file next to your tool's entry script. Standard library only.

    import maggot_warm

    def load():
        return load_the_model()          # slow, done once per warm process

    def handle(argv, model):
        ...                               # one run: parse argv, use model, print, return exit code

    if __name__ == "__main__":
        sys.exit(maggot_warm.main(load, handle))

How it works:
  * `run.py --serve` (started by `maggot warm <tool>`) calls load() once, then
    serves jobs on the Unix socket in $MAGGOT_WARM_SOCKET until it has been
    idle for $MAGGOT_WARM_IDLE seconds.
  * A normal run checks that socket. When a warm process answers, the job runs
    there (stdout/stderr and the exit code come back as if it ran here).
    Otherwise it runs cold: load() then handle() in this process. So the tool
    always works, with or without maggot.
  * Jobs run one at a time, in the job's working directory, with the job's
    MAGGOT_* environment variables.
"""

import contextlib
import io
import json
import os
import socket
import sys
import threading
import time
import traceback

SOCKET_ENV = "MAGGOT_WARM_SOCKET"
IDLE_ENV = "MAGGOT_WARM_IDLE"


def main(load, handle, argv=None):
    argv = sys.argv[1:] if argv is None else list(argv)
    if argv[:1] == ["--serve"]:
        return serve(load, handle)
    code = forward(argv)
    if code is not None:
        return code
    return _call(handle, argv, load())


def _call(handle, argv, model):
    try:
        code = handle(argv, model)
    except SystemExit as e:  # argparse errors, sys.exit() in handle
        code = e.code
    if code is None:
        return 0
    if isinstance(code, int):
        return code
    print(code, file=sys.stderr)
    return 1


# ---------- client side: send this run to the warm process ----------

def forward(argv):
    """Run argv in the warm process. Returns the exit code, or None when no
    warm process answers (then the caller runs cold)."""
    path = os.environ.get(SOCKET_ENV)
    if not path or not os.path.exists(path):
        return None
    try:
        conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        conn.connect(path)
    except OSError:
        return None
    env = {k: v for k, v in os.environ.items() if k.startswith("MAGGOT_") and k not in (SOCKET_ENV, IDLE_ENV)}
    with conn:
        conn.sendall((json.dumps({"type": "run", "argv": argv, "cwd": os.getcwd(), "env": env}) + "\n").encode())
        for raw in conn.makefile("rb"):
            msg = json.loads(raw)
            if "out" in msg:
                sys.stdout.write(msg["out"])
                sys.stdout.flush()
            elif "err" in msg:
                sys.stderr.write(msg["err"])
                sys.stderr.flush()
            elif "exit" in msg:
                return int(msg["exit"])
    print("warm process closed the connection before the job finished", file=sys.stderr)
    return 1


# ---------- server side ----------

class _Stream(io.TextIOBase):
    """A text stream that sends what is written to the client."""

    def __init__(self, conn, key):
        self.conn, self.key = conn, key

    def writable(self):
        return True

    def isatty(self):
        return False

    def write(self, s):
        if s:
            _send(self.conn, {self.key: s})  # client gone: finish the job anyway
        return len(s)


def _send(conn, msg):
    """Send one JSON line. A client that went away is not an error."""
    try:
        conn.sendall((json.dumps(msg) + "\n").encode())
        return True
    except OSError:
        return False


def serve(load, handle):
    path = os.environ.get(SOCKET_ENV)
    if not path:
        print(f"{SOCKET_ENV} is not set; start serve mode with `maggot warm <tool>`", file=sys.stderr)
        return 2
    idle = float(os.environ.get(IDLE_ENV, "600"))
    started = time.monotonic()
    model = load()
    print(f"warm: loaded in {time.monotonic() - started:.1f}s, waiting for jobs (stops after {idle:.0f}s idle)", flush=True)

    if os.path.exists(path):
        os.unlink(path)
    srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    srv.bind(path)
    os.chmod(path, 0o600)
    srv.listen(128)
    srv.settimeout(1.0)

    state = {"last": time.monotonic(), "jobs": 0, "busy": False}
    job_lock = threading.Lock()  # jobs run one at a time

    def idle_left():
        if state["busy"] or job_lock.locked():
            return idle
        return max(0.0, idle - (time.monotonic() - state["last"]))

    def client(conn):
        # Every connection gets its own thread, so a status check is answered
        # at once, even while a long job runs. Nothing a client does (hanging
        # up, sending junk) may stop the server.
        try:
            with conn:
                conn.settimeout(None)
                req = json.loads(conn.makefile("rb").readline() or b"{}")
                if req.get("type") == "status":
                    _send(conn, {"ok": True, "idle_left": round(idle_left()), "jobs": state["jobs"], "busy": state["busy"]})
                elif req.get("type") == "run":
                    with job_lock:
                        state["busy"] = True
                        state["jobs"] += 1
                        try:
                            code = _run_job(conn, req, handle, model)
                        finally:
                            state["busy"] = False
                            state["last"] = time.monotonic()
                    _send(conn, {"exit": code})
        except (OSError, ValueError):
            pass

    try:
        while idle_left() > 0:
            try:
                conn, _ = srv.accept()
            except socket.timeout:
                continue
            except OSError:
                time.sleep(0.1)
                continue
            threading.Thread(target=client, args=(conn,), daemon=True).start()
    finally:
        srv.close()
        with contextlib.suppress(OSError):
            os.unlink(path)
    print(f"warm: idle for {idle:.0f}s, stopping after {state['jobs']} job(s)", flush=True)
    return 0


def _run_job(conn, req, handle, model):
    old_cwd, old_env = os.getcwd(), dict(os.environ)
    out, err = _Stream(conn, "out"), _Stream(conn, "err")
    try:
        os.chdir(req.get("cwd") or old_cwd)
        os.environ.update(req.get("env") or {})
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            try:
                return _call(handle, req.get("argv") or [], model)
            except Exception:
                traceback.print_exc()
                return 1
    finally:
        os.chdir(old_cwd)
        os.environ.clear()
        os.environ.update(old_env)
