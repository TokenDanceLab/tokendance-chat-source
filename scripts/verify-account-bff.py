#!/usr/bin/env python3
"""Disposable Account/PostgreSQL -> Chat protocol acceptance (stdlib only).

Requires Docker with a locally cached postgres:18-alpine image, OpenSSL, and
already built Account/Chat binaries. All listeners are loopback, all credentials
are generated fixtures, and the temporary database is removed on exit.
This does not exercise a browser, external social providers, or R2. Account
intentionally refuses loopback/private back-channel endpoints, so the joint
harness signs a real Account-key Logout Token and posts it directly to Chat;
production must additionally verify Account outbox delivery to the public URI.
"""

import argparse
import base64
import contextlib
import http.client
import http.server
import importlib.util
import json
import os
from pathlib import Path
import secrets
import socket
import sqlite3
import ssl
import struct
import subprocess
import tempfile
import threading
import time
import urllib.parse
import urllib.request


def check(ok, label):
    if not ok:
        raise RuntimeError(label)
    print("PASS " + label, flush=True)


def command(args, **kwargs):
    result = subprocess.run(args, capture_output=True, **kwargs)
    if result.returncode:
        raise RuntimeError(f"fixture command failed ({Path(args[0]).name}, exit {result.returncode})")
    return result.stdout.decode()


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def wait_for(probe):
    for _ in range(80):
        try:
            if probe():
                return
        # The Account test harness has its own CheckFailure exception type.
        # Readiness probes intentionally absorb transport/setup failures until
        # the 20-second deadline; the final exception below is diagnostic.
        except Exception:
            pass
        time.sleep(0.25)
    raise RuntimeError("fixture readiness timeout")


def log_tail(path, sensitive_values):
    if not path.exists():
        return "<log missing>"
    text = path.read_text(errors="replace")
    for value in sensitive_values:
        if value:
            text = text.replace(value, "<redacted>")
    lines = text.splitlines()[-40:]
    return "\n".join(lines) if lines else "<log empty>"


def ws_connect(base, cookie):
    parsed = urllib.parse.urlsplit(base)
    conn = socket.create_connection(("127.0.0.1", parsed.port), timeout=5)
    key = base64.b64encode(secrets.token_bytes(16)).decode()
    conn.sendall((f"GET /ws HTTP/1.1\r\nHost: {parsed.netloc}\r\nOrigin: {base}\r\n"
                  f"Upgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: {key}\r\n"
                  f"Sec-WebSocket-Version: 13\r\nCookie: {cookie}\r\n\r\n").encode())
    stream = conn.makefile("rb")
    if b"101" not in stream.readline():
        conn.close()
        raise RuntimeError("WebSocket cookie upgrade failed")
    while stream.readline() != b"\r\n":
        pass
    return conn, stream


def ws_send_json(conn, value):
    payload = json.dumps(value).encode()
    mask = secrets.token_bytes(4)
    if len(payload) >= 126:
        raise RuntimeError("test WebSocket writer only supports short payloads")
    conn.sendall(bytes([0x81, 0x80 | len(payload)]) + mask +
                 bytes(item ^ mask[i % 4] for i, item in enumerate(payload)))


def ws_read_json(stream):
    first = stream.read(2)
    if len(first) != 2:
        raise EOFError("WebSocket closed")
    opcode = first[0] & 0x0F
    if opcode == 0x8:
        raise EOFError("WebSocket close frame")
    if opcode != 0x1:
        raise RuntimeError(f"unexpected WebSocket opcode {opcode}")
    size = first[1] & 127
    if size == 126:
        size = struct.unpack("!H", stream.read(2))[0]
    if size > 32768 or size == 127:
        raise RuntimeError("unexpected WebSocket frame size")
    return json.loads(stream.read(size))


def ws_join(base, cookie, username):
    conn, stream = ws_connect(base, cookie)
    try:
        ws_send_json(conn, {"type": "join", "username": username})
        return ws_read_json(stream)
    finally:
        conn.close()


def ws_join_live(base, cookie, username):
    conn, stream = ws_connect(base, cookie)
    ws_send_json(conn, {"type": "join", "username": username})
    for _ in range(20):
        event = ws_read_json(stream)
        if event.get("type") == "history":
            return conn, stream
    conn.close()
    raise RuntimeError("WebSocket join did not return history")


def sign_logout_token(private_key, issuer, client_id, subject, jti, now):
    def encoded(value):
        raw = json.dumps(value, separators=(",", ":"), sort_keys=True).encode()
        return base64.urlsafe_b64encode(raw).decode().rstrip("=")

    header = encoded({"alg": "RS256", "kid": "e2e-only", "typ": "JWT"})
    claims = encoded({
        "aud": client_id,
        "events": {"http://schemas.openid.net/event/backchannel-logout": {}},
        "exp": int(now + 3600),
        "iat": int(now),
        "iss": issuer,
        "jti": jti,
        "sub": subject,
    })
    signing_input = f"{header}.{claims}".encode()
    result = subprocess.run(
        ["openssl", "dgst", "-sha256", "-sign", private_key],
        input=signing_input,
        capture_output=True,
    )
    if result.returncode:
        raise RuntimeError("fixture Logout Token signing failed")
    signature = base64.urlsafe_b64encode(result.stdout).decode().rstrip("=")
    return f"{header}.{claims}.{signature}"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--account-dir", type=Path, required=True)
    parser.add_argument("--account-bin", type=Path, required=True)
    parser.add_argument("--chat-bin", type=Path, required=True)
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location("account_acceptance", args.account_dir / "scripts/verify-oidc.py")
    harness = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(harness)
    container = "tdchat-oidc-e2e-" + secrets.token_hex(8)
    stage = "fixture setup"
    processes = []
    proxy = None
    sensitive_values = []
    log_paths = {}
    with tempfile.TemporaryDirectory(prefix="tdchat-oidc-e2e-") as directory, contextlib.ExitStack() as stack:
        root = Path(directory)

        def fixture(name, value):
            path = root / name
            path.touch(mode=0o600)
            path.write_bytes(value.encode() if isinstance(value, str) else value)
            return str(path)

        try:
            password = secrets.token_urlsafe(24)
            sensitive_values.append(password)
            db_env = fixture("postgres.env", f"POSTGRES_USER=acceptance\nPOSTGRES_DB=acceptance\nPOSTGRES_PASSWORD={password}\n")
            command(["docker", "image", "inspect", "postgres:18-alpine"])
            command(["docker", "run", "--detach", "--rm", "--name", container,
                     "--memory=512m", "--cpus=1", "--pids-limit=128",
                     "--tmpfs", "/var/lib/postgresql:rw,size=512m", "--env-file", db_env,
                     "--publish", "127.0.0.1::5432", "postgres:18-alpine"])
            mapping = json.loads(command(["docker", "inspect", "--format", "{{json .NetworkSettings.Ports}}", container]))
            db_port = mapping["5432/tcp"][0]["HostPort"]
            wait_for(lambda: subprocess.run(["docker", "exec", container, "pg_isready", "-U", "acceptance"],
                                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0)
            db_file = fixture(
                "dsn",
                f"host=127.0.0.1 port={db_port} user=acceptance dbname=acceptance "
                f"password={password} sslmode=disable",
            )
            account_port, tls_port, chat_port = port(), port(), port()
            issuer = f"https://127.0.0.1:{tls_port}"
            chat_base = f"http://127.0.0.1:{chat_port}"
            private_key, certificate = str(root / "key.pem"), str(root / "certificate.pem")
            command(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                     "-subj", "/CN=127.0.0.1", "-addext", "subjectAltName=IP:127.0.0.1",
                     "-keyout", private_key, "-out", certificate])
            os.chmod(private_key, 0o600)
            env = {k: v for k, v in os.environ.items() if not k.startswith(("ACCOUNT_", "CHAT_"))}
            env.update(ACCOUNT_DATABASE_URL_FILE=db_file, ACCOUNT_ADMIN_ACTOR="operator:disposable-e2e",
                       ACCOUNT_ADDR=f"127.0.0.1:{account_port}", ACCOUNT_ISSUER=issuer,
                       ACCOUNT_ENABLE_PASSWORD_AUTH="true", ACCOUNT_ENABLE_OIDC="true",
                       ACCOUNT_SECURITY_EPOCH_FILE=fixture("epoch", "1\n"),
                       ACCOUNT_OIDC_SIGNING_KEY_FILE=private_key, ACCOUNT_OIDC_SIGNING_KID="e2e-only",
                       ACCOUNT_OIDC_CRYPTO_KEY_FILE=fixture("crypto-key", secrets.token_bytes(32)))
            command([str(args.account_bin), "migrate"], env=env, cwd=root)
            login = "bff-user@example.test"
            account_password = secrets.token_urlsafe(24)
            sensitive_values.append(account_password)
            command([str(args.account_bin), "account", "create", "--login", login,
                     "--password-file", fixture("password", account_password), "--display-name", "BFF First"], env=env, cwd=root)
            client = json.loads(command([str(args.account_bin), "client", "create", "--type", "confidential",
                                        "--name", "Disposable Chat", "--redirect", chat_base + "/api/oidc/callback",
                                        "--post-logout-redirect", chat_base + "/api/logout/account/callback"], env=env, cwd=root))
            sensitive_values.append(client["client_secret"])

            class Proxy(http.server.BaseHTTPRequestHandler):
                def log_message(self, *_):
                    pass

                def forward(self):
                    upstream = http.client.HTTPConnection("127.0.0.1", account_port, timeout=5)
                    try:
                        headers = {k: v for k, v in self.headers.items() if k.lower() not in {"connection", "transfer-encoding"}}
                        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
                        upstream.request(self.command, self.path, body, headers)
                        response = upstream.getresponse()
                        data = response.read()
                        self.send_response(response.status)
                        for key, value in response.getheaders():
                            if key.lower() not in {"connection", "transfer-encoding", "content-length"}:
                                self.send_header(key, value)
                        self.send_header("Content-Length", str(len(data)))
                        self.end_headers()
                        self.wfile.write(data)
                    finally:
                        upstream.close()

                do_GET = do_POST = do_PATCH = forward

            proxy = http.server.ThreadingHTTPServer(("127.0.0.1", tls_port), Proxy)
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            context.load_cert_chain(certificate, private_key)
            proxy.socket = context.wrap_socket(proxy.socket, server_side=True)
            threading.Thread(target=proxy.serve_forever, daemon=True).start()
            account_log = stack.enter_context((root / "account.log").open("wb"))
            chat_log = stack.enter_context((root / "chat.log").open("wb"))
            log_paths = {"Account": root / "account.log", "Chat": root / "chat.log"}
            processes.append(subprocess.Popen([str(args.account_bin)], cwd=root, env=env, stdout=account_log, stderr=account_log))
            account = harness.Client(issuer, issuer, 5, False)
            account.opener = urllib.request.build_opener(harness.NoRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=certificate)))
            stage = "Account readiness"
            wait_for(lambda: account.request("GET", "/ready")[0] == 200)
            chat_env = dict(env, CHAT_OIDC_ENABLED="true", CHAT_OIDC_ISSUER=issuer,
                            CHAT_OIDC_CLIENT_ID=client["client_id"], CHAT_OIDC_CLIENT_SECRET=client["client_secret"],
                            CHAT_OIDC_REDIRECT_URI=chat_base + "/api/oidc/callback",
                            CHAT_OIDC_POST_LOGOUT_REDIRECT_URI=chat_base + "/api/logout/account/callback",
                            CHAT_COOKIE_SECURE="false",
                            CHAT_ADDR=f"127.0.0.1:{chat_port}", CHAT_DB_PATH=str(root / "chat.db"),
                            CHAT_FRONTEND_DIR=str(root), CHAT_SESSION_SECRET=secrets.token_urlsafe(32),
                            # This protocol harness intentionally revisits
                            # /api/session after each negative auth assertion.
                            CHAT_API_RATE_LIMIT_PER_MINUTE="500",
                            SSL_CERT_FILE=certificate)
            processes.append(subprocess.Popen([str(args.chat_bin)], cwd=root, env=chat_env, stdout=chat_log, stderr=chat_log))
            chat = harness.Client(chat_base, issuer, 5, False)
            stage = "Chat readiness"
            wait_for(lambda: chat.request("GET", "/api/health")[0] == 200)
            stage = "local credential retirement"
            for path in ("/api/login", "/api/register", "/api/invite/list", "/api/invite/generate", "/api/oidc/exchange", "/api/oidc/refresh", "/api/oidc/redeem"):
                check(chat.request("POST", path)[0] == 404, "retired " + path)
            def login_once(label):
                nonlocal stage
                stage = label
                status, headers, _ = chat.request("GET", "/api/oidc/login")
                check(status == 302, "Chat authorization redirect")
                status, headers, _ = account.request("GET", headers["Location"], send_cookies=True)
                check(status == 302, "real Account authorization transaction")
                request_id = urllib.parse.parse_qs(urllib.parse.urlsplit(headers["Location"]).query)["request_id"][0]
                status, _, _ = account.json_request("POST", "/api/v1/auth/login", {
                    "method": "password", "accountAddress": login, "secret": account_password,
                }, extra_headers={"Origin": issuer})
                check(status == 200, "real Account password session")
                status, _, view = account.json_request("GET", "/api/v1/oauth/authorize/" + request_id)
                check(status == 200, "Account consent view")
                callback = view.get("redirectTo")
                if not callback:
                    status, _, decision = account.json_request("POST", "/api/v1/oauth/authorize/" + request_id + "/decision", {"decision": "allow"},
                                                              extra_headers={"Origin": issuer, "X-CSRF-Token": account.cookies.get("__Host-account_csrf")})
                    check(status == 200, "Account consent decision")
                    callback = decision["redirectTo"]
                # Approval returns Account's own authorization callback. It
                # issues the client redirect only after consuming the request.
                callback = account.localize(callback)
                if urllib.parse.urlsplit(callback).netloc == urllib.parse.urlsplit(account.base_url).netloc:
                    status, headers, _ = account.request("GET", callback, send_cookies=True)
                    check(status == 302, "Account authorization callback")
                    callback = headers["Location"]
                check(urllib.parse.urlsplit(callback).netloc == urllib.parse.urlsplit(chat_base).netloc, "Account redirects to the registered Chat callback")
                status, headers, body = chat.request("GET", callback, send_cookies=True)
                check(status == 303 and headers["Location"] == "/?oidc_success=1", "Account -> Chat confidential BFF callback")
                check(b"access_token" not in body and "access_token" not in headers["Location"] and "oidc_rid" not in headers["Location"], "callback has no browser token handoff")
                raw_cookies = headers.get_all("Set-Cookie") or []
                check(any(c.startswith("tdchat_session=") and "HttpOnly" in c and "SameSite=Lax" in c for c in raw_cookies), "HttpOnly SameSite Chat session")
                _, _, session = chat.json_request("GET", "/api/session")
                check(session.get("authenticated") and session.get("kind") == "oidc", "Chat session identity")
                return session

            username = None
            for iteration in range(2):
                session = login_once(f"OIDC login {iteration + 1}")
                if username:
                    check(session["username"] == username, "same sub keeps Chat handle after profile rename")
                username = session["username"]
                with sqlite3.connect(root / "chat.db") as db:
                    row = db.execute("SELECT name, picture FROM oidc_users WHERE chat_username=?", (username,)).fetchone()
                    count = db.execute("SELECT count(*) FROM oidc_users").fetchone()[0]
                check(row[0] == ("BFF First" if iteration == 0 else "BFF Renamed") and count == 1, "UserInfo profile synchronized without duplicate account")
                check(not row[1], "missing Account avatar stays absent")
                if iteration == 0:
                    status, _, _ = account.json_request("PATCH", "/api/v1/account", {"display_name": "BFF Renamed", "locale": "en"},
                                                      extra_headers={"Origin": issuer, "X-CSRF-Token": account.cookies.get("__Host-account_csrf")})
                    check(status == 200, "Account profile update")
            stage = "WebSocket identity"
            check(ws_join(chat_base, chat.cookies.header(), "impostor").get("code") == "AUTH_FAILED", "cookie-bound WebSocket rejects identity override")
            check(ws_join(chat_base, chat.cookies.header(), username).get("type") == "history", "cookie-bound WebSocket joins authenticated user")
            check(ws_join(chat_base, "", username).get("code") == "OIDC_REQUIRED", "anonymous WebSocket cannot claim Account handle")

            stage = "local Chat logout"
            check(session.get("csrf_token"), "session exposes derived CSRF token")
            status, _, _ = chat.request(
                "POST",
                "/api/logout",
                headers={"Origin": "https://wrong.example.test", "X-CSRF-Token": session["csrf_token"]},
                send_cookies=True,
            )
            check(status == 403, "cross-origin local logout rejected")
            status, _, _ = chat.request(
                "POST",
                "/api/logout",
                headers={"Origin": chat_base},
                send_cookies=True,
            )
            check(status == 403, "local logout without CSRF rejected")
            _, _, still_session = chat.json_request("GET", "/api/session")
            check(still_session.get("authenticated") is True, "rejected logout keeps Chat session")
            status, _, _ = chat.request(
                "POST",
                "/api/logout",
                headers={"Origin": chat_base, "X-CSRF-Token": session["csrf_token"]},
                send_cookies=True,
            )
            check(status == 204, "same-origin CSRF local logout")
            _, _, no_session = chat.json_request("GET", "/api/session")
            check(no_session.get("authenticated") is False, "local logout clears Chat session")

            session = login_once("OIDC login after local logout")
            check(session.get("username") == username, "local logout preserves Account session and identity")

            stage = "Account global logout"
            status, logout_headers, logout_body = chat.json_request(
                "POST",
                "/api/logout/account",
                None,
                extra_headers={"Origin": chat_base, "X-CSRF-Token": session["csrf_token"]},
            )
            check(status == 200, "Chat builds Account end-session navigation")
            logout_url = logout_body.get("redirect_url", "")
            logout_query = urllib.parse.parse_qs(urllib.parse.urlsplit(logout_url).query)
            check(
                urllib.parse.urlsplit(logout_url).netloc == urllib.parse.urlsplit(issuer).netloc
                and logout_query.get("client_id") == [client["client_id"]]
                and logout_query.get("post_logout_redirect_uri") == [chat_base + "/api/logout/account/callback"]
                and bool(logout_query.get("state", [""])[0]),
                "Account logout URL uses discovery and exact registered redirect",
            )
            logout_cookie = [
                value for value in (logout_headers.get_all("Set-Cookie") or [])
                if value.startswith("tdchat_logout=") and "HttpOnly" in value and "SameSite=Lax" in value
            ]
            check(len(logout_cookie) == 1, "Account logout binds state to an HttpOnly browser cookie")
            check(not any(name in logout_url for name in ("id_token", "access_token", "refresh_token")), "Account logout URL carries no tokens")
            _, _, no_session = chat.json_request("GET", "/api/session")
            check(no_session.get("authenticated") is False, "Account logout first clears Chat session")
            status, end_headers, _ = account.request("GET", logout_url, send_cookies=True)
            check(status == 302, "real Account end-session completes")
            returned = urllib.parse.urlsplit(end_headers["Location"])
            returned_query = urllib.parse.parse_qs(returned.query)
            check(
                returned.netloc == urllib.parse.urlsplit(chat_base).netloc
                and returned.path == "/api/logout/account/callback"
                and returned_query.get("state") == logout_query.get("state"),
                "Account returns to exact Chat logout callback with state",
            )
            status, callback_headers, _ = chat.request(
                "GET",
                returned.path + ("?" + returned.query if returned.query else ""),
                send_cookies=True,
            )
            check(
                status == 302 and callback_headers.get("Location") == "/?oidc_logged_out=1",
                "Chat consumes browser-bound logout state and returns to the clean marker",
            )
            status, replay_headers, _ = chat.request(
                "GET",
                returned.path + ("?" + returned.query if returned.query else ""),
                send_cookies=True,
            )
            check(
                status == 302 and replay_headers.get("Location") == "/?oidc_logout_error=1",
                "Chat rejects replayed Account logout callback state",
            )

            session = login_once("OIDC login after Account logout")
            check(session.get("username") == username, "global logout can re-authenticate the same stable subject")
            stage = "back-channel logout"
            live_conn, live_stream = ws_join_live(chat_base, chat.cookies.header(), username)
            with sqlite3.connect(root / "chat.db") as db:
                subject = db.execute(
                    "SELECT sub FROM oidc_users WHERE chat_username=?",
                    (username,),
                ).fetchone()[0]
                generation_before = db.execute(
                    "SELECT generation FROM oidc_logout_epochs WHERE sub=?",
                    (subject,),
                ).fetchone()
            jti = "joint-logout-" + secrets.token_hex(8)
            logout_token = sign_logout_token(
                private_key,
                issuer,
                client["client_id"],
                subject,
                jti,
                time.time(),
            )
            form = urllib.parse.urlencode({"logout_token": logout_token}).encode()
            status, _, _ = chat.request(
                "POST",
                "/api/oidc/backchannel-logout",
                headers={"Content-Type": "application/x-www-form-urlencoded"},
                data=form,
            )
            check(status == 204, "Chat accepts real Account-key Logout Token")
            _, _, no_session = chat.json_request("GET", "/api/session")
            check(no_session.get("authenticated") is False, "back-channel logout invalidates REST session")
            saw_logout = False
            for _ in range(40):
                try:
                    event = ws_read_json(live_stream)
                except EOFError:
                    break
                if event.get("type") == "account_logout":
                    saw_logout = True
                    break
            check(saw_logout, "back-channel logout sends typed live WebSocket event")
            try:
                ws_read_json(live_stream)
                closed_after_event = False
            except EOFError:
                closed_after_event = True
            live_conn.close()
            check(closed_after_event, "back-channel logout closes live WebSocket")
            status, _, _ = chat.request(
                "POST",
                "/api/oidc/backchannel-logout",
                headers={"Content-Type": "application/x-www-form-urlencoded"},
                data=urllib.parse.urlencode({"logout_token": logout_token}).encode(),
            )
            check(status == 204, "identical Logout Token retry is idempotent")
            changed = sign_logout_token(
                private_key,
                issuer,
                client["client_id"],
                "different-subject",
                jti,
                time.time() + 1,
            )
            status, _, _ = chat.request(
                "POST",
                "/api/oidc/backchannel-logout",
                headers={"Content-Type": "application/x-www-form-urlencoded"},
                data=urllib.parse.urlencode({"logout_token": changed}).encode(),
            )
            check(status == 400, "same JTI with different claims is rejected")
            with sqlite3.connect(root / "chat.db") as db:
                generation_after = db.execute(
                    "SELECT generation FROM oidc_logout_epochs WHERE sub=?",
                    (subject,),
                ).fetchone()[0]
                jti_count = db.execute(
                    "SELECT count(*) FROM oidc_backchannel_logout_jtis WHERE jti=?",
                    (jti,),
                ).fetchone()[0]
            check(
                generation_after == (generation_before[0] if generation_before else 0) + 1,
                "Logout Token advances subject generation exactly once",
            )
            check(jti_count == 1, "Logout Token ledger stores one replay guard")
            print("PASS disposable real Account/PostgreSQL -> Chat BFF protocol acceptance", flush=True)
        except Exception as error:
            diagnostics = "\n".join(
                f"{name} log tail:\n{log_tail(path, sensitive_values)}"
                for name, path in log_paths.items()
            )
            suffix = f"\n{diagnostics}" if diagnostics else ""
            raise RuntimeError(f"{stage}: {type(error).__name__}: {error}{suffix}") from None
        finally:
            for process in reversed(processes):
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            if proxy:
                proxy.shutdown()
                proxy.server_close()
            subprocess.run(["docker", "rm", "--force", container], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
