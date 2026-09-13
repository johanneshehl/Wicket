#!/usr/bin/env python3
"""End-to-end test of the 1.3 features (groups, IP rules, nginx mode, metrics, OIDC provider,
auditor role, mail and notification settings) against a throwaway Wicket instance.
Usage: python3 e2e_features.py <port> <setup-code>
"""
import base64, hashlib, hmac, http.cookiejar, json, re, struct, sys, time, urllib.error, urllib.parse, urllib.request

PORT, SETUP_CODE = sys.argv[1], sys.argv[2]
ADMIN = f"http://wicket.localtest.me:{PORT}"
LOGIN = f"http://login.localtest.me:{PORT}"
RAW = f"http://127.0.0.1:{PORT}"
PW = "Adm1n-" + hashlib.sha1(str(time.time()).encode()).hexdigest()[:12]
results = []


def check(name, cond, info=""):
    ok = bool(cond)
    results.append(ok)
    print(f"{'OK  ' if ok else 'FAIL'} {name}{' - ' + str(info) if info else ''}")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None


def client(follow=True):
    jar = http.cookiejar.CookieJar()
    handlers = [urllib.request.HTTPCookieProcessor(jar)]
    if not follow:
        handlers.append(NoRedirect())
    return urllib.request.build_opener(*handlers), jar


def req(opener, method, url, data=None, headers=None, json_body=None):
    headers = dict(headers or {})
    body = None
    if json_body is not None:
        body = json.dumps(json_body).encode()
        headers["Content-Type"] = "application/json"
        headers.setdefault("X-Wicket", "1")
    elif data is not None:
        body = urllib.parse.urlencode(data).encode()
        headers["Content-Type"] = "application/x-www-form-urlencoded"
    r = urllib.request.Request(url, data=body, method=method, headers=headers)
    try:
        resp = opener.open(r, timeout=15)
        return resp.status, resp.headers, resp.read().decode(errors="replace"), resp.geturl()
    except urllib.error.HTTPError as e:
        return e.code, e.headers, e.read().decode(), url


def csrf(html):
    m = re.search(r'name="csrf" value="([^"]+)"', html)
    return m.group(1) if m else ""


def totp(secret):
    key = base64.b32decode(secret + "=" * (-len(secret) % 8))
    mac = hmac.new(key, struct.pack(">Q", int(time.time() // 30)), hashlib.sha1).digest()
    off = mac[-1] & 0x0F
    return "%06d" % ((struct.unpack(">I", mac[off:off + 4])[0] & 0x7FFFFFFF) % 1000000)


def session_cookie(jar):
    return "; ".join(f"{c.name}={c.value}" for c in jar if c.name == "wicket_session")


def verify(host, uri, cookie="", ip="", extra=None):
    op, _ = client(follow=False)
    h = {"X-Forwarded-Host": host, "X-Forwarded-Uri": uri, "X-Forwarded-Proto": "https"}
    if cookie:
        h["Cookie"] = cookie
    if ip:
        h["X-Forwarded-For"] = ip
    h.update(extra or {})
    return req(op, "GET", RAW + "/verify", headers=h)


def enroll_2fa(opener, base, html):
    secret = re.search(r'data-copy="([A-Z2-7]{32})"', html).group(1)
    return req(opener, "POST", base + "/setup-2fa", {"csrf": csrf(html), "code": totp(secret)})


# ------------------------------------------------------------------ setup
admin, _ = client()
st, _, html, _ = req(admin, "GET", ADMIN + "/")
st, _, html, _ = req(admin, "POST", ADMIN + "/setup", {"csrf": csrf(html), "code": SETUP_CODE, "username": "boss", "password": PW, "password2": PW})
st, _, html, _ = req(admin, "POST", ADMIN + "/setup", {"csrf": csrf(html), "cookieDomain": "localtest.me", "loginHost": "login.localtest.me", "adminHost": "wicket.localtest.me"})
st, _, html, _ = enroll_2fa(admin, ADMIN, html)
check("admin set up with 2FA", st == 200)

api = lambda m, p, body=None: req(admin, m, ADMIN + p, json_body=body) if body is not None else req(admin, m, ADMIN + p)

# ------------------------------------------------------------------ groups
st, _, body, _ = api("POST", "/api/users", {"username": "anna", "email": "anna@example.com", "role": "user", "password": "anna-passwort-123"})
anna = json.loads(body)["id"]
st, _, body, _ = api("POST", "/api/users", {"username": "ben", "role": "user", "password": "ben-passwort-1234"})
ben = json.loads(body)["id"]
st, _, body, _ = api("POST", "/api/groups", {"name": "ops", "description": "Operations", "members": [anna]})
ops = json.loads(body).get("id")
check("create group ops", st == 201 and ops, body[:120])
st, _, body, _ = api("POST", "/api/groups", {"name": "OPS", "members": []})
check("duplicate group name rejected", st == 409)
st, _, body, _ = api("GET", "/api/users")
users = {u["username"]: u for u in json.loads(body)["users"]}
check("user list shows group membership", users["anna"]["groups"] == [ops], users["anna"].get("groups"))

st, _, body, _ = api("POST", "/api/sites", {"domain": "app.localtest.me", "access": "users", "users": [], "groups": [ops], "bypass": [], "enabled": True, "managed": False,
                                            "allowIps": ["198.51.100.0/24"], "denyIps": ["203.0.113.9"], "maxSessionHours": 0})
site = json.loads(body)
check("site with group, IP rules", st == 201 and site.get("groups") == [ops] and site.get("allowIps") == ["198.51.100.0/24"], body[:200])
st, _, body, _ = api("POST", "/api/sites", {"domain": "bad.localtest.me", "access": "all", "enabled": True, "managed": False, "denyIps": ["999.1.1.1"]})
check("invalid IP rule rejected", st == 400 and "IP" in body, body)

# ------------------------------------------------------------------ forward auth with the new rules
def signin(user, password):
    op, jar = client(follow=False)
    st, _, html, _ = req(op, "GET", LOGIN + "/")
    st, h, _, _ = req(op, "POST", LOGIN + "/login", {"csrf": csrf(html), "rd": "", "username": user, "password": password})
    return op, jar, st

_, anna_jar, st = signin("anna", "anna-passwort-123")
_, ben_jar, _ = signin("ben", "ben-passwort-1234")
st, h, _, _ = verify("app.localtest.me", "/", session_cookie(anna_jar))
check("group member allowed", st == 200 and h.get("Remote-User") == "anna" and h.get("Remote-Groups") == "ops" and h.get("Remote-Email") == "anna@example.com", dict(h))
st, h, _, _ = verify("app.localtest.me", "/", session_cookie(ben_jar))
check("non-member denied", st == 302 and "/denied" in h.get("Location", ""))
st, _, _, _ = verify("app.localtest.me", "/", "", ip="198.51.100.20")
check("allowed network passes without login", st == 200)
st, _, _, _ = verify("app.localtest.me", "/", session_cookie(anna_jar), ip="203.0.113.9")
check("denied IP blocked even when signed in", st == 403)
st, h, _, _ = verify("app.localtest.me", "/", "", extra={"X-Original-URL": "https://app.localtest.me/x?y=1"})
check("nginx mode answers 401 with login location", st == 401 and h.get("X-Wicket-Location", "").startswith("https://login.localtest.me/?rd="), h.get("X-Wicket-Location"))

# ------------------------------------------------------------------ metrics
op, _ = client()
st, _, body, _ = req(op, "GET", RAW + "/metrics")
check("metrics on localhost", st == 200 and 'wicket_verify_total{result="allow"}' in body and "wicket_build_info" in body)
st, _, _, _ = req(op, "GET", LOGIN + "/metrics")
check("metrics hidden on public hosts", st == 404)

# ------------------------------------------------------------------ OIDC provider
st, _, body, _ = api("POST", "/api/oidc", {"name": "Grafana", "redirectUris": ["https://grafana.localtest.me/login/generic_oauth"], "access": "users", "users": [], "groups": [ops]})
created = json.loads(body)
cid, secret = created["client"]["id"], created["secret"]
check("create OIDC client", st == 201 and cid == "grafana" and secret, body[:160])
st, _, body, _ = api("POST", "/api/oidc", {"name": "x", "redirectUris": ["http://evil.example.com/cb"], "access": "all"})
check("insecure redirect URI rejected", st == 400)
st, _, body, _ = req(op, "GET", LOGIN + "/.well-known/openid-configuration")
disc = json.loads(body)
check("discovery document", disc.get("issuer") == "https://login.localtest.me" and disc.get("token_endpoint", "").endswith("/oidc/token"))
st, _, body, _ = req(op, "GET", LOGIN + "/oidc/jwks")
check("JWKS", st == 200 and json.loads(body)["keys"][0]["crv"] == "P-256")

redirect = "https://grafana.localtest.me/login/generic_oauth"
verifier = "v" * 50
challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
q = urllib.parse.urlencode({"client_id": cid, "redirect_uri": redirect, "response_type": "code", "scope": "openid profile email groups",
                            "state": "s1", "nonce": "n1", "code_challenge": challenge, "code_challenge_method": "S256"})
anna_op, _ = client(follow=False)
anna_op.addheaders = [("Cookie", session_cookie(anna_jar))]
st, h, _, _ = req(anna_op, "GET", LOGIN + "/oidc/authorize?" + q)
loc = h.get("Location", "")
code = urllib.parse.parse_qs(urllib.parse.urlparse(loc).query).get("code", [""])[0]
check("authorize returns a code for a group member", st == 302 and loc.startswith(redirect) and code and "state=s1" in loc, loc[:120])
ben_op, _ = client(follow=False)
ben_op.addheaders = [("Cookie", session_cookie(ben_jar))]
st, h, _, _ = req(ben_op, "GET", LOGIN + "/oidc/authorize?" + q)
check("authorize denies non-members", "error=access_denied" in h.get("Location", ""))
st, h, _, _ = req(client(follow=False)[0], "GET", LOGIN + "/oidc/authorize?" + q)
check("authorize without session goes to login", st == 302 and h.get("Location", "").startswith("https://login.localtest.me/?rd="))

basic = base64.b64encode(f"{cid}:{secret}".encode()).decode()
form = {"grant_type": "authorization_code", "code": code, "redirect_uri": redirect, "code_verifier": verifier}
st, _, body, _ = req(op, "POST", LOGIN + "/oidc/token", form, headers={"Authorization": "Basic " + basic})
tok = json.loads(body)
check("token exchange", st == 200 and tok.get("id_token") and tok.get("access_token"), body[:160])
claims = json.loads(base64.urlsafe_b64decode(tok["id_token"].split(".")[1] + "=="))
check("ID token claims", claims.get("aud") == cid and claims.get("preferred_username") == "anna" and claims.get("groups") == ["ops"] and claims.get("nonce") == "n1", claims)
st, _, body, _ = req(op, "POST", LOGIN + "/oidc/token", form, headers={"Authorization": "Basic " + basic})
check("code cannot be replayed", st == 400 and "invalid_grant" in body)
st, _, body, _ = req(op, "GET", LOGIN + "/oidc/userinfo", headers={"Authorization": "Bearer " + tok["access_token"]})
check("userinfo", st == 200 and json.loads(body).get("email") == "anna@example.com")
wrong = base64.b64encode(f"{cid}:nope".encode()).decode()
st, _, body, _ = req(op, "POST", LOGIN + "/oidc/token", form, headers={"Authorization": "Basic " + wrong})
check("wrong client secret rejected", st == 401)

# ------------------------------------------------------------------ auditor role
st, _, body, _ = api("POST", "/api/users", {"username": "audit", "role": "auditor", "password": "audit-passwort-12"})
check("create auditor", st == 201, body[:100])
aud, aud_jar = client()
st, _, html, _ = req(aud, "GET", ADMIN + "/login")
st, _, html, url = req(aud, "POST", ADMIN + "/login", {"csrf": csrf(html), "username": "audit", "password": "audit-passwort-12"})
check("auditor must set up 2FA", "/setup-2fa" in url, url)
st, _, html, _ = enroll_2fa(aud, ADMIN, html)
st, _, body, _ = req(aud, "GET", ADMIN + "/api/sites")
check("auditor can read", st == 200)
st, _, body, _ = req(aud, "POST", ADMIN + "/api/groups", json_body={"name": "nope", "members": []})
check("auditor cannot change", st == 403, body)

# ------------------------------------------------------------------ mail + notifications
st, _, body, _ = api("PUT", "/api/mail", {"host": "mail.example.com", "port": 587, "security": "ssl", "from": "wicket@example.com"})
check("invalid mail security rejected", st == 400)
st, _, body, _ = api("PUT", "/api/mail", {"host": "mail.example.com", "port": 587, "security": "starttls", "username": "w", "password": "secret", "from": "wicket@example.com"})
st, _, body, _ = api("GET", "/api/mail")
m = json.loads(body)
check("mail config stored, password hidden", m["smtp"]["host"] == "mail.example.com" and m["hasPassword"] and not m["smtp"].get("password"))
st, _, body, _ = api("PUT", "/api/notify", {"channels": [{"name": "ops", "type": "webhook", "target": "ftp://x", "events": [], "enabled": True}]})
check("invalid channel rejected", st == 400)
st, _, body, _ = api("PUT", "/api/notify", {"channels": [{"name": "ops", "type": "ntfy", "target": "https://ntfy.sh/wicket-test", "token": "tk", "events": ["ip_locked"], "enabled": True}]})
st, _, body, _ = api("GET", "/api/notify")
n = json.loads(body)
check("channel stored, token hidden", len(n["channels"]) == 1 and n["channels"][0]["hasToken"] and not n["channels"][0].get("token"))
st, _, body, _ = api("GET", "/api/integrations")
integ = json.loads(body)
check("integration snippets", "forwardAuth" in integ.get("traefik", "") and "auth_request" in integ.get("nginx", ""))

# ------------------------------------------------------------------ password reset + invitations (fake SMTP server)
import email, quopri, socketserver, threading

MAILS = []


class SMTPHandler(socketserver.StreamRequestHandler):
    def handle(self):
        self.wfile.write(b"220 fake ESMTP\r\n")
        data, in_data = [], False
        while True:
            line = self.rfile.readline()
            if not line:
                return
            if in_data:
                if line in (b".\r\n", b".\n"):
                    MAILS.append(b"".join(data).decode("utf-8", "replace"))
                    data, in_data = [], False
                    self.wfile.write(b"250 queued\r\n")
                else:
                    data.append(line[1:] if line.startswith(b"..") else line)
                continue
            cmd = line.strip().upper()
            if cmd.startswith(b"EHLO") or cmd.startswith(b"HELO"):
                self.wfile.write(b"250-fake\r\n250 8BITMIME\r\n")
            elif cmd.startswith(b"DATA"):
                in_data = True
                self.wfile.write(b"354 go ahead\r\n")
            elif cmd.startswith(b"QUIT"):
                self.wfile.write(b"221 bye\r\n")
                return
            else:
                self.wfile.write(b"250 ok\r\n")


smtp = socketserver.ThreadingTCPServer(("127.0.0.1", 0), SMTPHandler)
smtp.daemon_threads = True
threading.Thread(target=smtp.serve_forever, daemon=True).start()
SMTP_PORT = smtp.server_address[1]


def wait_mail(n, timeout=5):
    end = time.time() + timeout
    while time.time() < end and len(MAILS) < n:
        time.sleep(0.1)
    return MAILS[n - 1] if len(MAILS) >= n else ""


def mail_link(raw, kind):
    msg = email.message_from_string(raw)
    for part in msg.walk():
        if part.get_content_type() == "text/plain":
            text = quopri.decodestring(part.get_payload()).decode("utf-8")
            m = re.search(r"https://login\.localtest\.me/" + kind + r"/[A-Za-z0-9_-]+", text)
            if m:
                return m.group(0).replace("https://login.localtest.me", LOGIN), msg
    return "", msg


st, _, body, _ = api("PUT", "/api/mail", {"host": "127.0.0.1", "port": SMTP_PORT, "security": "none", "from": "wicket@localtest.me", "fromName": "Wicket"})
check("local mail server configured", st == 200, body)
st, _, body, _ = api("POST", "/api/mail/test", {"to": "boss@example.com"})
check("test mail sent", st == 200 and "Wicket" in wait_mail(1), body)
st, _, html, _ = req(client()[0], "GET", LOGIN + "/")
check("login page offers password reset", "/reset" in html)

rs, _ = client(follow=False)
st, _, html, _ = req(rs, "GET", LOGIN + "/reset")
st, _, html, _ = req(rs, "POST", LOGIN + "/reset", {"csrf": csrf(html), "login": "anna@example.com"})
check("reset request answered", st == 200)
link, msg = mail_link(wait_mail(2), "reset")
check("reset mail with link", link and msg["To"] == "anna@example.com", msg["Subject"])
st, _, html, _ = req(rs, "GET", link)
check("reset form", st == 200 and 'name="password2"' in html)
st, _, html, _ = req(rs, "POST", link, {"csrf": csrf(html), "password": "anna-neues-pw-99", "password2": "anna-neues-pw-99"})
check("new password saved", st == 200)
st, _, _, _ = req(rs, "GET", link)
check("reset link works only once", st == 410)
_, _, st_old = signin("anna", "anna-passwort-123")
_, _, st_new = signin("anna", "anna-neues-pw-99")
check("old password rejected, new accepted", st_old == 401 and st_new == 303, (st_old, st_new))
count = len(MAILS)
st, _, html, _ = req(rs, "GET", LOGIN + "/reset")
st, _, html, _ = req(rs, "POST", LOGIN + "/reset", {"csrf": csrf(html), "login": "nobody"})
time.sleep(1)
check("unknown user gets the same answer and no mail", st == 200 and len(MAILS) == count)

st, _, body, _ = api("POST", "/api/users", {"username": "carla", "email": "carla@example.com", "role": "user", "invite": True})
check("user created with invitation", st == 201, body[:120])
link, msg = mail_link(wait_mail(count + 1), "invite")
check("invitation mail", link and msg["To"] == "carla@example.com", msg["Subject"])
st, _, html, _ = req(rs, "GET", link)
st, _, html, _ = req(rs, "POST", link, {"csrf": csrf(html), "password": "carla-passwort-1", "password2": "carla-passwort-1"})
_, _, st = signin("carla", "carla-passwort-1")
check("invited user signs in with chosen password", st == 303)
st, _, body, _ = api("POST", f"/api/users/{ben}/reset-link", {})
check("reset link needs an email address", st == 400 and "email" in body.lower(), body)
smtp.shutdown()

# ------------------------------------------------------------------ passkeys (ceremony start; a real authenticator is needed to finish)
pk, _ = client(follow=False)
st, _, body, _ = req(pk, "POST", LOGIN + "/passkey/login/begin", headers={"X-Wicket": "0"})
check("passkey endpoint needs the custom header", st == 403)
st, _, body, _ = req(pk, "POST", LOGIN + "/passkey/login/begin", json_body={})
opts = json.loads(body).get("publicKey", {})
check("discoverable login options", st == 200 and opts.get("rpId") == "localtest.me" and len(opts.get("challenge", "")) >= 16, body[:160])
st, _, body, _ = req(pk, "POST", LOGIN + "/passkey/login/finish", json_body={"id": "x", "rawId": "eA", "type": "public-key", "response": {}})
check("forged assertion rejected", st in (400, 401), body[:120])
anna_pk, _ = client(follow=False)
anna_pk.addheaders = [("Cookie", session_cookie(signin("anna", "anna-neues-pw-99")[1]))]
st, _, body, _ = req(anna_pk, "POST", LOGIN + "/passkey/register/begin", json_body={})
reg = json.loads(body).get("publicKey", {})
check("registration options for a signed-in user", st == 200 and reg.get("user", {}).get("name") == "anna" and reg.get("authenticatorSelection", {}).get("residentKey") == "required", body[:200])
st, _, body, _ = req(client(follow=False)[0], "POST", LOGIN + "/passkey/register/begin", json_body={})
check("registration needs a session", st == 401)
st, _, body, _ = api("GET", "/api/me/passkeys")
check("passkey list for the admin", st == 200 and json.loads(body)["passkeys"] == [])

# ------------------------------------------------------------------ branding
pub, _ = client(follow=False)
st, _, html, _ = req(pub, "GET", LOGIN + "/")
check("default look: Wicket name and footer", "· Wicket</title>" in html and "brand-logo" not in html and "<style>" not in html)
st, _, body, _ = api("PUT", "/api/branding", {"name": "Acme", "accent": "#zz0000"})
check("invalid accent rejected", st == 400)
st, _, body, _ = api("PUT", "/api/branding", {"name": "Acme", "logo": "data:text/html;base64,PGI+"})
check("non-image logo rejected", st == 400)
svg = base64.b64encode(b'<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>').decode()
st, _, body, _ = api("PUT", "/api/branding", {"name": "Acme", "logo": "data:image/svg+xml;base64," + svg})
check("SVG logo with a handler rejected", st == 400)
logo = base64.b64encode(bytes.fromhex("89504e470d0a1a0a0000000d4948445200000001000000010806000000")).decode()
st, _, body, _ = api("PUT", "/api/branding", {"name": "Acme", "accent": "#0070F3", "footer": "Acme IT", "logo": "data:image/png;base64," + logo})
b = json.loads(body) if st == 200 else {}
check("branding saved", st == 200 and b.get("accent") == "#0070F3" and b.get("logo", "").startswith("/branding/logo?v="), body[:160])
st, _, html, _ = req(pub, "GET", LOGIN + "/")
check("login page uses the branding", "· Acme</title>" in html and 'class="brand-logo"' in html and "background:#0070f3;color:#fff" in html and "Acme IT" in html, html[:200])
lo, _ = client(follow=False)
st, hdr, data, _ = req(lo, "GET", LOGIN + b.get("logo", "/branding/logo"))
check("logo served as an image", st == 200 and "image/png" in str(hdr.get("Content-Type", "")), (st, hdr.get("Content-Type") if hdr else None))
st, _, body, _ = api("PUT", "/api/branding", {"name": "", "accent": "", "footer": "", "hideFooter": True, "logo": ""})
st2, _, _, _ = req(lo, "GET", LOGIN + "/branding/logo")
st, _, html, _ = req(pub, "GET", LOGIN + "/")
check("branding reset, footer hidden", st2 == 404 and "· Wicket</title>" in html and 'class="protected"' not in html)
api("PUT", "/api/branding", {"hideFooter": False})

# ------------------------------------------------------------------ admin UI assets
st, _, html, _ = req(pub, "GET", ADMIN + "/static/admin.html")
order = [html.find(s) for s in ("i18n.js", "i18n_ext.js", "passkey.js", "admin.js", "admin_ext.js")]
check("admin page loads the extension scripts in order", st == 200 and -1 not in order and order == sorted(order), order)
st, _, js, _ = req(pub, "GET", ADMIN + "/static/admin_ext.js")
st2, _, js2, _ = req(pub, "GET", ADMIN + "/static/i18n_ext.js")
check("extension scripts served", st == 200 and "window.WX" in js and st2 == 200 and "'nav.groups'" in js2)

# ------------------------------------------------------------------ updates (fake GitHub API and fake Watchtower on port 9099)
import http.server
UPD = {"releases": [
    {"tag_name": "v1.4.0", "body": "New things <!-- a comment -->", "html_url": "https://example.com/v1.4.0", "published_at": "2026-09-20T10:00:00Z"},
    {"tag_name": "v1.3.0", "body": "", "html_url": "https://example.com/v1.3.0"},
], "hits": []}


class FakeUpstream(http.server.BaseHTTPRequestHandler):
    def reply(self, code, body=b""):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/repos/johanneshehl/Wicket/releases"):
            self.reply(200, json.dumps(UPD["releases"]).encode())
        elif self.path.startswith("/v1/update"):
            self.do_POST()
        else:
            self.reply(404)

    def do_POST(self):
        UPD["hits"].append(self.headers.get("Authorization"))
        self.reply(200)

    def log_message(self, *args):
        pass


upstream = http.server.ThreadingHTTPServer(("127.0.0.1", 9099), FakeUpstream)
threading.Thread(target=upstream.serve_forever, daemon=True).start()

st, _, body, _ = api("GET", "/api/update")
u = json.loads(body) if st == 200 else {}
check("update status before the first check", st == 200 and u.get("current") == "1.3.0" and u.get("method") == "watchtower" and not u.get("available"), body[:200])
st, _, body, _ = api("POST", "/api/update/check", {})
u = json.loads(body) if st == 200 else {}
check("newer release found", u.get("available") and u.get("latest") == "1.4.0" and not u.get("locked") and u.get("notes") == "New things" and u.get("canApply"), body[:300])
st, _, body, _ = api("POST", "/api/update/apply", {})
r = json.loads(body) if st == 200 else {}
check("update started with a database backup", st == 200 and r.get("run", {}).get("backup", "").startswith("wicket-before-1.4.0-"), body[:200])
for _ in range(30):
    if UPD["hits"]:
        break
    time.sleep(0.1)
check("Watchtower called with the token", UPD["hits"] == ["Bearer e2e-token"], UPD["hits"])

outcome = lambda v: (v[0], v[1].get("Location"))  # status and redirect of a forward_auth answer
before = outcome(verify("upd-check.localtest.me", "/"))
UPD["releases"].insert(0, {"tag_name": "v1.5.0", "body": "Security fix <!-- wicket:required -->", "html_url": "https://example.com/v1.5.0"})
st, _, body, _ = api("POST", "/api/update/check", {})
u = json.loads(body) if st == 200 else {}
check("required release locks this version", u.get("locked") and u.get("minVersion") == "1.5.0", body[:200])
st, _, body, _ = api("GET", "/api/sites")
st2, _, _, _ = api("GET", "/api/me")
check("admin API locked, own account still readable", st == 423 and "update_required" in body and st2 == 200, (st, st2))
after = outcome(verify("upd-check.localtest.me", "/"))
check("forward_auth not affected by the lock", after == before, (before, after))
st, _, body, _ = api("PUT", "/api/update/config", {"check": False})
check("check cannot be switched off while locked", st == 409)
UPD["releases"].pop(0)
st, _, body, _ = api("POST", "/api/update/check", {})
st2, _, _, _ = api("GET", "/api/sites")
check("lock lifted when the required release is gone", not json.loads(body).get("locked") and st2 == 200)
st, _, body, _ = api("PUT", "/api/update/config", {"check": False})
check("automatic check can be switched off", st == 200 and json.loads(body).get("check") is False)
api("PUT", "/api/update/config", {"check": True})
upstream.shutdown()

print(f"\n{sum(results)}/{len(results)} passed")
sys.exit(0 if all(results) else 1)
