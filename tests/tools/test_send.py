#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""maxpack send against a fake MaxPack for the console (its receive routes, as docs/guide.md gives them).

Usage: test_send.py <maxpack> <fixtures folder> <temporary folder>
"""
import http.server
import json
import os
import shutil
import socket
import subprocess
import sys
import threading
import time
import urllib.parse

EXE, FX, TMP = sys.argv[1], sys.argv[2], sys.argv[3]
CODE = "482913"
failures = []


def check(name, cond, detail=""):
    print(("ok    " if cond else "FAIL  ") + name + ("" if cond else "  " + detail))
    if not cond:
        failures.append(name)


class Console:
    """What the fake console keeps: its drives, the files it received and how it should answer."""

    def __init__(self):
        self.lock = threading.Lock()
        self.reset()

    def reset(self):
        self.receive_on = True
        self.code = CODE
        self.drives = [{"id": "internal", "free": 600 * 10**9, "total": 825 * 10**9, "fs": "ufs"},
                       {"id": "usb0", "free": 1500 * 10**9, "total": 2000 * 10**9, "fs": "exfat"}]
        self.partial = {}      # (drive, name) -> bytearray: <name>.maxpack-recv
        self.done = {}         # (drive, name) -> bytes: <name>, verified
        self.token = None
        self.key = None
        self.size = 0
        self.state = None
        self.verify_bad = None  # a list of bad items: the verify fails with them
        self.verify_polls = 0
        self.cancel_during_verify = False
        self.refuse_start = None   # (status, code)
        self.cut_puts = 0          # PUTs cut short after half their body, with no answer
        self.forget_on_cut = False  # and the console forgets the token, as when its payload starts again
        self.offset_once = None    # the first PUT is answered 409 offset with this have
        self.slow_put = 0.0
        self.requests = []         # (method, path, the code it carried)
        self.put_offsets = []
        self.cancelled = False
        # contract v2: a receive paused (its token kept), and what the PC sees of it
        self.pause_after = None    # after this many PUTs taken the receive pauses, for pause_reason
        self.pause_reason = "idle"
        self.pause_again = 0       # it pauses again this many times after each resume (a write error that stays)
        self.pause_in_verify = None  # the verify pauses for this reason
        self.puts_taken = 0
        self.starts = 0
        self.forget_once = False   # the next PUT finds its token unknown (404), as after a restart
        self.cut_status = 0        # status requests cut with no answer, every other one
        self.status_seen = 0
        self.stuck = False         # PUT answers 200 with the same have and keeps nothing
        self.verify_stuck = False  # the verify never moves
        self.hostile = False       # strings with control characters, and a long list of what failed

    def have(self):
        return len(self.partial.get(self.key, b""))


C = Console()


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def answer(self, status, obj):
        body = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def refuse(self, status, code, **more):
        self.answer(status, dict({"error": code, "detail": "the fake console says " + code}, **more))

    def handle_any(self, method):
        url = urllib.parse.urlparse(self.path)
        q = dict(urllib.parse.parse_qsl(url.query))
        n = int(self.headers.get("Content-Length") or 0)
        with C.lock:
            C.requests.append((method, url.path, self.headers.get("X-MaxPack-Code")))
            if not C.receive_on:
                self.rfile.read(n)
                return self.refuse(403, "receive_off")
            if self.headers.get("X-MaxPack-Code") != C.code:
                self.rfile.read(n)
                return self.refuse(401, "bad_code")
            if (method, url.path) == ("GET", "/api/receive/drives"):
                if C.hostile:
                    return self.answer(200, {"drives": [{"id": "internal", "free": 1, "total": 2, "fs": "ufs\x1b[31m\x07"},
                                                        {"id": "usb\x1b]0;x\x07", "free": 1, "total": 2, "fs": "e" * 5000}]})
                return self.answer(200, {"drives": C.drives})
            if (method, url.path) == ("POST", "/api/receive/start"):
                req = json.loads(self.rfile.read(n) or b"{}")
                if C.refuse_start:
                    return self.refuse(*C.refuse_start)
                name, size, drive = req.get("name", ""), req.get("size", -1), req.get("drive", "")
                if drive not in [d["id"] for d in C.drives]:
                    return self.refuse(404, "no_drive")
                if not name.endswith(".p5z") or "/" in name or "\\" in name:
                    return self.refuse(400, "bad_name")
                if (drive, name) in C.done:
                    return self.refuse(409, "exists")
                key = (drive, name)
                if key in C.partial and C.size != size:   # a .maxpack-recv of another size: started again
                    C.partial[key] = bytearray()
                C.partial.setdefault(key, bytearray())
                C.starts += 1
                if C.state == "paused" and C.pause_again > 0:
                    C.pause_again -= 1
                    C.pause_after = C.puts_taken + 1
                C.key, C.size, C.state = key, size, "receiving"
                C.token = "t%d" % len(C.requests)
                return self.answer(200, {"id": C.token, "have": C.have()})
            if q.get("id") != C.token or C.token is None:
                self.rfile.read(n)
                return self.refuse(404, "unknown_id")
            if (method, url.path) == ("PUT", "/api/receive/data"):
                off = int(q.get("offset", "-1"))
                C.put_offsets.append(off)
                if C.forget_once:
                    C.forget_once = False
                    self.rfile.read(n)
                    C.token = None
                    return self.refuse(404, "unknown_id")
                if C.state == "paused":
                    self.rfile.read(n)
                    return self.refuse(409, "paused", reason=C.pause_reason, have=C.have())
                if C.stuck:
                    self.rfile.read(n)
                    return self.answer(200, {"have": C.have()})
                if C.cut_puts > 0:
                    C.cut_puts -= 1
                    if C.forget_on_cut:
                        C.token = None
                    self.rfile.read(n // 2)
                    self.close_connection = True
                    self.connection.shutdown(socket.SHUT_RDWR)
                    return None
                body = self.rfile.read(n)
                if C.offset_once is not None:
                    have, C.offset_once = C.offset_once, None
                    del C.partial[C.key][have:]
                    return self.refuse(409, "offset", have=C.have())
                if off != C.have():
                    return self.refuse(409, "offset", have=C.have())
                if off + len(body) > C.size:
                    return self.refuse(400, "past_size")
                if C.slow_put:
                    time.sleep(C.slow_put)
                C.partial[C.key] += body
                C.puts_taken += 1
                if C.pause_after is not None and C.puts_taken >= C.pause_after:
                    C.pause_after, C.state = None, "paused"
                return self.answer(200, {"have": C.have()})
            if (method, url.path) == ("POST", "/api/receive/finish"):
                self.rfile.read(n)
                if C.state == "paused":
                    return self.refuse(409, "paused", reason=C.pause_reason, have=C.have())
                if C.have() != C.size:
                    return self.refuse(409, "not_complete")
                C.state, C.verify_polls = "verifying", 0
                self.send_response(202)
                self.send_header("Content-Length", "0")
                self.end_headers()
                return None
            if (method, url.path) == ("GET", "/api/receive/status"):
                C.status_seen += 1
                if C.cut_status > 0 and C.status_seen % 2 == 0:
                    C.cut_status -= 1
                    self.close_connection = True
                    self.connection.shutdown(socket.SHUT_RDWR)
                    return None
                verify = {"bytes": 0, "total": C.size, "bad": []}
                if C.state == "paused":
                    return self.answer(200, {"have": C.have(), "size": C.size, "state": "paused", "reason": C.pause_reason,
                                             "verify": verify})
                if C.state == "verifying" and C.pause_in_verify:
                    C.state, C.pause_reason, C.pause_in_verify = "paused", C.pause_in_verify, None
                    return self.answer(200, {"have": C.have(), "size": C.size, "state": "paused", "reason": C.pause_reason,
                                             "verify": verify})
                if C.state == "verifying" and C.verify_stuck:
                    return self.answer(200, {"have": C.have(), "size": C.size, "state": "verifying", "verify": verify})
                if C.state == "verifying":
                    C.verify_polls += 1
                    verify["bytes"] = min(C.size, C.verify_polls * C.size // 3)
                    if C.cancel_during_verify:
                        C.state = "cancelled"
                        del C.partial[C.key]
                    elif C.verify_polls >= 3 and C.verify_bad:
                        C.state = "failed"
                    elif C.verify_polls >= 3:
                        C.state = "done"
                        C.done[C.key] = bytes(C.partial.pop(C.key))
                if C.state == "failed":
                    verify = {"bytes": C.size, "total": C.size, "bad": C.verify_bad}
                if C.state == "done":
                    verify["bytes"] = C.size
                return self.answer(200, {"have": C.size if C.state == "done" else C.have(), "size": C.size,
                                         "state": C.state, "verify": verify})
            if (method, url.path) == ("POST", "/api/receive/cancel"):
                self.rfile.read(n)
                C.partial.pop(C.key, None)
                C.state, C.cancelled = "cancelled", True
                return self.answer(200, {"ok": True})
            self.rfile.read(n)
            return self.refuse(404, "no_route")

    def do_GET(self):
        self.handle_any("GET")

    def do_POST(self):
        self.handle_any("POST")

    def do_PUT(self):
        self.handle_any("PUT")


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
ADDR = "127.0.0.1:%d" % server.server_address[1]


def run(*args, env=None, stdin=None):
    e = dict(os.environ)
    e.pop("MAXPACK_SEND_CODE", None)
    e.update(env or {})
    p = subprocess.run([EXE, *args], stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=e,
                       input=stdin, timeout=120)
    return p.returncode, p.stdout.decode("utf-8", "replace"), p.stderr.decode("utf-8", "replace")


def events(text):
    out = []
    for line in text.splitlines():
        if line.strip():
            try:
                out.append(json.loads(line))
            except ValueError:
                out.append({"event": "not_json", "line": line})
    return out


def result(evs):
    return evs[-1] if evs and evs[-1].get("event") == "result" else {}


def no_code_shown(*texts):
    return all(CODE not in t for t in texts)


# a .p5z to send
os.makedirs(TMP, exist_ok=True)
P5Z = os.path.join(TMP, "Test Game [PPSA10001].p5z")
NAME = os.path.basename(P5Z)
if os.path.exists(P5Z):
    os.remove(P5Z)
rc, _, err = run("pack", os.path.join(FX, "basic.exfat"), "--p5z", "-o", P5Z, "--threads", "2")
check("pack", rc == 0, err[-300:])
DATA = open(P5Z, "rb").read()
SIZE = len(DATA)
check("p5z_is_several_chunks", SIZE > 4 * 65536, str(SIZE))
SMALL = ["--chunk", "65536", "--retry-wait", "20"]

# ---- the drives ----------------------------------------------------------------------------------
C.reset()
rc, so, err = run("send", "--drives", ADDR, "--code", CODE, "--json")
d = json.loads(so or "{}")
check("drives_json", rc == 0 and [x["id"] for x in d.get("drives", [])] == ["internal", "usb0"]
      and d["drives"][1]["free"] == 1500 * 10**9 and d["drives"][1]["total"] == 2000 * 10**9
      and d["drives"][1]["fs"] == "exfat", so + err)
rc, so, err = run("send", "--drives", ADDR, "--code", CODE)
check("drives_text", rc == 0 and "internal" in so and "usb0" in so and "1500.00 GB free of 2000.00 GB" in so
      and no_code_shown(so, err), so + err)
check("drives_carry_the_code", all(r[2] == CODE for r in C.requests), str(C.requests))

# ---- a whole send --------------------------------------------------------------------------------
# maxpack send tells its progress at most every 100 ms (and at the end): the console takes each PUT in no
# less than 40 ms here, so the 32 PUTs of 64 KiB last more than 1.2 s and more than four lines must come,
# however fast the machine is
C.reset()
C.slow_put = 0.04
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--drive", "usb0", "--json", *SMALL)
evs = events(so)
res = result(evs)
sending = [e for e in evs if e.get("event") == "progress" and e.get("phase") == "sending"]
verifying = [e for e in evs if e.get("event") == "progress" and e.get("phase") == "verifying"]
check("send_done", rc == 0 and res.get("ok") is True and C.done.get(("usb0", NAME)) == DATA and not C.partial,
      "rc=%d %s %s" % (rc, so[-400:], err[-400:]))
check("send_result_fields", res.get("name") == NAME and res.get("drive") == "usb0" and res.get("size") == SIZE
      and res.get("resumed_from") == 0 and res.get("sent") == SIZE and res.get("retries") == 0, str(res))
check("send_progress", len(sending) >= 4 and sending[0]["done"] < SIZE and sending[-1]["done"] == SIZE and sending[-1]["total"] == SIZE
      and all(a["done"] <= b["done"] for a, b in zip(sending, sending[1:]))
      and all("rate" in e and "eta" in e for e in sending), str(sending[-3:]))
check("send_console_verify_progress", len(verifying) >= 1 and all(e["total"] == SIZE for e in verifying),
      str(verifying))
check("send_started_event", any(e.get("event") == "started" and e.get("drive") == "usb0" and e.get("have") == 0
                                for e in evs), so[:400])
check("send_lines_are_json", all(e.get("event") != "not_json" for e in evs), so[-400:])
check("send_carries_the_code", C.requests and all(r[2] == CODE for r in C.requests), str(C.requests[:3]))
check("send_never_shows_the_code", no_code_shown(so, err), so[-200:] + err[-200:])
check("send_offsets_in_order", C.put_offsets == list(range(0, SIZE, 65536)), str(C.put_offsets[:6]))
# text: the time left and the speed, and what it ended with
C.reset()
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, *SMALL)
check("send_text_default_drive", rc == 0 and C.done.get(("internal", NAME)) == DATA, so + err[-400:])
check("send_text_says", "MB/s" in err and "left" in err and "Sent and verified" in so, so + err[-400:])
# the code from the environment, out of the command line
C.reset()
rc, so, err = run("send", P5Z, ADDR, *SMALL, env={"MAXPACK_SEND_CODE": CODE})
check("send_code_from_env", rc == 0 and C.done.get(("internal", NAME)) == DATA, err[-300:])

# ---- going on where it was -----------------------------------------------------------------------
C.reset()
C.partial[("internal", NAME)] = bytearray(DATA[:100000])
C.size = SIZE
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--json", *SMALL)
res = result(events(so))
check("send_resumes", rc == 0 and C.put_offsets[:1] == [100000] and res.get("resumed_from") == 100000
      and res.get("sent") == SIZE - 100000 and C.done.get(("internal", NAME)) == DATA, str(C.put_offsets[:3]) + err[-300:])
# a connection cut in the middle of a PUT: it asks where the console is and goes on from there
C.reset()
C.cut_puts = 2
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--json", *SMALL)
evs = events(so)
res = result(evs)
retries = [e for e in evs if e.get("event") == "retry"]
check("send_after_a_cut", rc == 0 and C.done.get(("internal", NAME)) == DATA and res.get("retries") == 2
      and len(retries) == 2 and retries[0].get("have") == 0 and retries[0].get("attempt") == 1, so[-500:] + err[-300:])
# the console started again meanwhile and no longer knows the receive: started again, from what it has
C.reset()
C.cut_puts = 1
C.forget_on_cut = True
C.partial[("internal", NAME)] = bytearray(DATA[:2 * 65536])
C.size = SIZE
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, *SMALL)
starts = [r for r in C.requests if r[1] == "/api/receive/start"]
check("send_after_a_restart", rc == 0 and len(starts) == 2 and C.done.get(("internal", NAME)) == DATA
      and C.put_offsets[:2] == [2 * 65536, 2 * 65536], str(C.put_offsets[:3]) + err[-300:])
# the console says it has other bytes than the PC thought (409 offset): from what it has
C.reset()
C.partial[("internal", NAME)] = bytearray(DATA[:3 * 65536])
C.size = SIZE
C.offset_once = 65536
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, *SMALL)
check("send_offset_409", rc == 0 and C.put_offsets[:3] == [3 * 65536, 65536, 2 * 65536]
      and C.done.get(("internal", NAME)) == DATA, str(C.put_offsets[:4]) + err[-300:])

# ---- what the console refuses: a message and an exit code each ---------------------------------
def refused(name, code, exit_code, setup, *extra, words=""):
    C.reset()
    setup()
    rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--json", *SMALL, *extra)
    res = result(events(so))
    check("refused_" + name, rc == exit_code and res.get("ok") is False and res.get("error") == code
          and words in err and no_code_shown(so, err), "rc=%d %s %s" % (rc, so[-300:], err[-300:]))
    return res


refused("receive_off", "receive_off", 4, lambda: setattr(C, "receive_on", False), words="Receive games from the PC")
refused("bad_code", "bad_code", 5, lambda: setattr(C, "code", "111111"), words="pairing code")
refused("locked", "locked", 6, lambda: setattr(C, "refuse_start", (429, "locked")), words="wrong codes")
refused("exists", "exists", 7, lambda: C.done.update({("internal", NAME): DATA}), words="already")
refused("busy", "busy", 8, lambda: setattr(C, "refuse_start", (409, "busy")), words="busy")
refused("no_space", "no_space", 9, lambda: setattr(C, "refuse_start", (507, "no_space")), words="space")
refused("fat32", "fat32", 10, lambda: setattr(C, "refuse_start", (400, "fat32")), words="FAT32")
refused("bad_name_console", "bad_name", 11, lambda: setattr(C, "refuse_start", (400, "bad_name")), words="name")
refused("no_drive", "no_drive", 12, lambda: None, "--drive", "usb9", words="usb9")
check("no_drive_never_starts", not any(r[1] == "/api/receive/start" for r in C.requests), str(C.requests))
bad = [{"kind": "volume", "volume": 0, "path": "ampr_zstd-000.pak", "why": "sha256",
        "expected": "aa" * 32, "found": "bb" * 32}]
res = refused("verify_failed", "verify_failed", 13, lambda: setattr(C, "verify_bad", bad), words="ampr_zstd-000.pak")
check("verify_failed_lists", res.get("bad") == [{"path": "ampr_zstd-000.pak", "volume": 0, "why": "sha256",
                                                 "expected": "aa" * 32, "found": "bb" * 32}], str(res))
check("verify_failed_keeps_it", C.partial.get(("internal", NAME)) == bytearray(DATA) and not C.done)
refused("console_cancelled", "console_cancelled", 14, lambda: setattr(C, "cancel_during_verify", True),
        words="cancelled on the console")
# a name the console would not take: said before anything is sent
odd = os.path.join(TMP, "renamed.bin")
shutil.copyfile(P5Z, odd)
C.reset()
rc, so, err = run("send", odd, ADDR, "--code", CODE, "--json", *SMALL)
check("bad_name_here", rc == 11 and result(events(so)).get("error") == "bad_name" and not C.requests, err[-300:])
# not a .p5z
junk = os.path.join(TMP, "junk.p5z")
open(junk, "wb").write(b"not a p5z" * 100)
C.reset()
rc, so, err = run("send", junk, ADDR, "--code", CODE, "--json", *SMALL)
check("not_a_p5z", rc == 1 and result(events(so)).get("error") == "invalid" and not C.requests, err[-300:])
# nobody there
lone = socket.socket()
lone.bind(("127.0.0.1", 0))
free_port = lone.getsockname()[1]
lone.close()
t0 = time.time()
rc, so, err = run("send", P5Z, "127.0.0.1:%d" % free_port, "--code", CODE, "--json", "--retries", "2",
                  "--retry-wait", "20")
check("unreachable", rc == 3 and result(events(so)).get("error") == "unreachable" and time.time() - t0 < 30,
      "rc=%d %s %s" % (rc, so[-300:], err[-300:]))

# ---- cancelled here: the console deletes what it got --------------------------------------------
C.reset()
C.slow_put = 0.2
err_file = open(os.path.join(TMP, "cancel.err"), "w+b")
p = subprocess.Popen([EXE, "send", P5Z, ADDR, "--code", CODE, "--json", "--control", "stdin", "--delete-on-cancel", *SMALL],
                     stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=err_file)
lines = []
reader = threading.Thread(target=lambda: lines.extend(iter(p.stdout.readline, b"")), daemon=True)
reader.start()
deadline = time.time() + 30
while time.time() < deadline and not any(b'"sending"' in l for l in lines) and p.poll() is None:
    time.sleep(0.05)
try:
    p.stdin.write(b"cancel\n")
    p.stdin.flush()
except OSError:
    pass
try:
    p.wait(timeout=60)
except subprocess.TimeoutExpired:
    p.kill()
    p.wait()
reader.join(timeout=10)
err_file.seek(0)
err = err_file.read().decode("utf-8", "replace")
err_file.close()
rest = b"".join(lines).decode("utf-8", "replace")
res = result(events(rest))
check("cancelled_here", p.returncode == 130 and res.get("error") == "cancelled" and C.cancelled
      and ("internal", NAME) not in C.partial and not C.done, "rc=%s %s %s" % (p.returncode, rest[-300:], err[-300:]))

# ---- contract v2: paused receives, 404, stop and keep, timeouts, a hostile console ---------------
# paused while idle: the PC starts it again and goes on from what the console has
C.reset()
C.pause_after = 3
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, *SMALL)
check("v2_paused_idle_resumes", rc == 0 and C.done.get(("internal", NAME)) == DATA and C.starts == 2, "rc=%d starts=%d %s" % (rc, C.starts, err[-300:]))
# a write error that stays: counted against the tries, then said
C.reset()
C.pause_after, C.pause_reason, C.pause_again = 2, "write_error", 50
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--json", "--retries", "3", *SMALL)
res = result(events(so))
check("v2_write_error_gives_up", rc == 17 and res.get("error") == "write_error" and 3 <= C.starts <= 6 and not C.cancelled,
      "rc=%d starts=%d %s %s" % (rc, C.starts, so[-200:], err[-300:]))
# stopped on the console: the PC stops, keeps what arrived and never starts it again
C.reset()
C.pause_after, C.pause_reason = 3, "stopped"
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--json", *SMALL)
res = result(events(so))
check("v2_stopped_on_the_console", rc == 15 and res.get("error") == "console_stopped" and C.starts == 1 and not C.cancelled
      and len(C.partial.get(("internal", NAME), b"")) == 3 * 65536 and "stopped on the console" in err, "rc=%d %s %s" % (rc, so[-200:], err[-300:]))
C.reset()
C.pause_in_verify = "stopped"
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, *SMALL)
check("v2_stopped_during_verify", rc == 15 and C.starts == 1 and not C.done, "rc=%d starts=%d %s" % (rc, C.starts, err[-300:]))
C.reset()
C.pause_in_verify = "idle"
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, *SMALL)
check("v2_paused_in_verify_resumes", rc == 0 and C.starts == 2 and C.done.get(("internal", NAME)) == DATA, "rc=%d starts=%d %s" % (rc, C.starts, err[-300:]))
# a token the console no longer knows on a PUT (it started again): started again, from what it has
C.reset()
C.partial[("internal", NAME)] = bytearray(DATA[:65536])
C.size = SIZE
C.forget_once = True
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, *SMALL)
check("v2_404_on_put_starts_again", rc == 0 and C.starts == 2 and C.done.get(("internal", NAME)) == DATA, "rc=%d %s" % (rc, err[-300:]))
# any answer resets the tries: more broken connections than --retries, never two in a row, still arrives
C.reset()
C.cut_status = 6
C.verify_bad = None
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--retries", "2", *SMALL)
check("v2_answers_reset_the_tries", rc == 0 and C.done.get(("internal", NAME)) == DATA, "rc=%d %s" % (rc, err[-300:]))
# a console that answers without taking anything: said, not a loop
C.reset()
C.stuck = True
t0 = time.time()
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--json", *SMALL)
check("v2_no_progress_is_an_error", rc == 1 and result(events(so)).get("error") == "console_error" and time.time() - t0 < 30, "rc=%d %s" % (rc, err[-300:]))
# a receive that does not move for --stall seconds: stopped, kept
C.reset()
C.verify_stuck = True
t0 = time.time()
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--json", "--stall", "2", *SMALL)
check("v2_stalled", rc == 16 and result(events(so)).get("error") == "stalled" and not C.cancelled and time.time() - t0 < 30,
      "rc=%d %s %s" % (rc, so[-200:], err[-300:]))
# a console never reached: "no answer" after 2 tries, the connect waiting 5 s at most
t0 = time.time()
rc, so, err = run("send", P5Z, "10.255.255.1:9", "--code", CODE, "--json")
check("v2_never_reached_is_quick", rc == 3 and result(events(so)).get("error") == "unreachable" and time.time() - t0 < 25,
      "rc=%d %.1f s %s" % (rc, time.time() - t0, err[-300:]))
# what the console says is printed without its control characters, and its list of what failed is capped
C.reset()
C.hostile = True
rc, so, err = run("send", "--drives", ADDR, "--code", CODE)
d_rc, d_so, d_err = run("send", "--drives", ADDR, "--code", CODE, "--json")
d = json.loads(d_so or "{}")
check("v2_hostile_drives_cleaned", rc == 0 and "\x1b" not in so + err and "\x07" not in so + err and "\x1b" not in d_so
      and all(len(x["fs"]) <= 1024 for x in d.get("drives", [])), repr(so[:300]))
C.reset()
C.verify_bad = [{"path": "ampr_zstd-%03d.pak\x1b[2J" % i, "why": "sha256\x07", "expected": "aa" * 32, "found": "bb" * 32} for i in range(100)]
rc, so, err = run("send", P5Z, ADDR, "--code", CODE, "--json", *SMALL)
res = result(events(so))
check("v2_hostile_bad_capped", rc == 13 and len(res.get("bad", [])) == 64 and "\x1b" not in err and "\x07" not in err
      and all("\x1b" not in b["path"] for b in res.get("bad", [])), "rc=%d %d %r" % (rc, len(res.get("bad", [])), err[-200:]))

# ---- stop and keep, and deleting what arrived ---------------------------------------------------
def send_and_stop(extra, close_only=False):
    """Starts a send that the console takes slowly, stops it after its first progress line (the line "cancel", or
    its input closed as when the window goes away) and gives its exit code and its JSON lines."""
    C.reset()
    C.slow_put = 0.2
    err_file = open(os.path.join(TMP, "stop.err"), "w+b")
    proc = subprocess.Popen([EXE, "send", P5Z, ADDR, "--code", CODE, "--json", "--control", "stdin", *SMALL, *extra],
                            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=err_file)
    got = []
    reader = threading.Thread(target=lambda: got.extend(iter(proc.stdout.readline, b"")), daemon=True)
    reader.start()
    deadline = time.time() + 30
    while time.time() < deadline and not any(b'"sending"' in line for line in got) and proc.poll() is None:
        time.sleep(0.05)
    try:
        if not close_only:
            proc.stdin.write(b"cancel\n")
            proc.stdin.flush()
        proc.stdin.close()
    except OSError:
        pass
    try:
        proc.wait(timeout=60)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()
    reader.join(timeout=10)
    err_file.close()
    return proc.returncode, result(events(b"".join(got).decode("utf-8", "replace")))


rc, res = send_and_stop([])
check("stop_keeps_by_default", rc == 130 and res.get("error") == "cancelled" and res.get("kept") is True and not C.cancelled
      and 0 < len(C.partial.get(("internal", NAME), b"")) < SIZE, "rc=%s %s" % (rc, res))
rc, res = send_and_stop(["--delete-on-cancel"])
check("delete_on_cancel", rc == 130 and res.get("kept") is False and C.cancelled and ("internal", NAME) not in C.partial, "rc=%s %s" % (rc, res))
rc, res = send_and_stop(["--delete-on-cancel"], close_only=True)
check("closed_input_keeps_even_with_delete_on_cancel", rc == 130 and res.get("kept") is True and not C.cancelled
      and ("internal", NAME) in C.partial, "rc=%s %s" % (rc, res))
# stopped before the console ever answered: nothing arrived, so nothing is "kept"
proc = subprocess.Popen([EXE, "send", P5Z, "10.255.255.1:9", "--code", CODE, "--json", "--control", "stdin"],
                        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
out, _ = proc.communicate(b"cancel\n", timeout=60)
res = result(events(out.decode("utf-8", "replace")))
check("stopped_before_anything_arrived", proc.returncode == 130 and res.get("kept") is False, "rc=%s %s" % (proc.returncode, res))
# Delete what arrived on the console: started, then cancelled
C.reset()
C.partial[("internal", NAME)] = bytearray(DATA[:5 * 65536])
C.size = SIZE
rc, so, err = run("send", "--discard", P5Z, ADDR, "--code", CODE, "--json")
check("discard_deletes_what_arrived", rc == 0 and C.cancelled and ("internal", NAME) not in C.partial and json.loads(so or "{}").get("ok") is True,
      "rc=%d %s %s" % (rc, so, err[-300:]))
C.reset()
C.done[("internal", NAME)] = DATA
rc, so, err = run("send", "--discard", P5Z, ADDR, "--code", CODE)
check("discard_of_one_there_whole", rc == 7 and not C.cancelled and ("internal", NAME) in C.done, "rc=%d %s" % (rc, err[-300:]))

# ---- usage -------------------------------------------------------------------------------------
rc, _, err = run("send", P5Z, ADDR)
check("usage_needs_the_code", rc == 2 and "--code" in err, err[-300:])
rc, _, err = run("send", P5Z, ADDR, "--code", "12345")
check("usage_code_six_digits", rc == 2, err[-300:])
rc, _, err = run("send", P5Z, "console.local", "--code", CODE)
check("usage_numeric_address", rc == 2, err[-300:])
rc, _, err = run("send", P5Z, "192.168.001.20", "--code", CODE)
check("usage_no_leading_zeros", rc == 2, err[-300:])
rc, _, err = run("send", P5Z, ADDR, "--code", CODE, "--retry-wait", "30001")
check("usage_retry_wait_at_most_30_s", rc == 2, err[-300:])
rc, _, err = run("send", P5Z, ADDR, "--code", CODE, "--chunk", "100")
check("usage_chunk", rc == 2, err[-300:])
rc, so, _ = run("--help")
check("help_send", "maxpack send <game.p5z> <ps5-ip> --code" in so and "maxpack send --drives" in so, so[-3000:])
rc, so, _ = run("--capabilities")
check("capabilities_send", json.loads(so or "{}").get("send") is True, so)

server.shutdown()
print("test_send: %d failures" % len(failures))
sys.exit(1 if failures else 0)
