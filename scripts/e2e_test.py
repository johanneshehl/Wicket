#!/usr/bin/env python3
"""End-to-end test against a throwaway Wicket instance (stdlib only).

Hosts under localtest.me resolve to 127.0.0.1, so cookies scoped to the
parent domain behave exactly like in production.
Usage: python3 e2e_test.py <port> <setup-code>
"""
import base64, hashlib, hmac, http.cookiejar, json, re, struct, sys, time, urllib.error, urllib.parse, urllib.request

PORT, SETUP_CODE = sys.argv[1], sys.argv[2]
ADMIN = f"http://wicket.localtest.me:{PORT}"
LOGIN = f"http://login.localtest.me:{PORT}"
RAW = f"http://127.0.0.1:{PORT}"
PW_ADMIN, PW_ANNA = "Adm1n-" + hashlib.sha1(str(time.time()).encode()).hexdigest()[:12], "anna-passwort-123"
results = []


def check(name, cond, info=""):
    results.append(cond)
    print(f"{'OK  ' if cond else 'FAIL'} {name}{' - ' + str(info) if info else ''}")


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
        return resp.status, resp.headers, resp.read().decode(), resp.geturl()
    except urllib.error.HTTPError as e:
        return e.code, e.headers, e.read().decode(), url


def csrf(html):
    m = re.search(r'name="csrf" value="([^"]+)"', html)
    return m.group(1) if m else ""


def totp(secret, step=None):
    key = base64.b32decode(secret + "=" * (-len(secret) % 8))
    step = int(time.time() // 30) if step is None else step
    mac = hmac.new(key, struct.pack(">Q", step), hashlib.sha1).digest()
    off = mac[-1] & 0x0F
    return "%06d" % ((struct.unpack(">I", mac[off:off + 4])[0] & 0x7FFFFFFF) % 1000000)


def cookie_header(jar):
    return "; ".join(f"{c.name}={c.value}" for c in jar if c.name == "wicket_session")


def verify(host, uri, cookie=""):
    op, _ = client(follow=False)
    h = {"X-Forwarded-Host": host, "X-Forwarded-Uri": uri, "X-Forwarded-Proto": "https"}
    if cookie:
        h["Cookie"] = cookie
    return req(op, "GET", RAW + "/verify", headers=h)


# ------------------------------------------------------------------ first-run setup
admin, ajar = client()
st, _, html, url = req(admin, "GET", ADMIN + "/")
check("unset instance redirects to /setup", url.endswith("/setup") and "Setup-Code" in html)
st, _, html, _ = req(admin, "POST", ADMIN + "/setup", {"csrf": csrf(html), "code": "xxxx-xxxx", "username": "testadmin", "password": PW_ADMIN, "password2": PW_ADMIN})
check("wrong setup code rejected", st == 400 and "Setup-Code stimmt nicht" in html)
st, _, html, url = req(admin, "POST", ADMIN + "/setup", {"csrf": csrf(html), "code": SETUP_CODE, "username": "testadmin", "password": PW_ADMIN, "password2": PW_ADMIN})
check("step 1 creates admin -> step 2", st == 200 and 'name="cookieDomain"' in html, st)
st, _, html, url = req(admin, "POST", ADMIN + "/setup", {"csrf": csrf(html), "cookieDomain": "localtest.me", "loginHost": "login.localtest.me", "adminHost": "wicket.localtest.me"})
check("step 2 -> forced 2FA setup", url.endswith("/setup-2fa") and "Pflicht" in html, url)
secret = re.search(r'data-copy="([A-Z2-7]{32})"', html).group(1)
check("QR code rendered", "data:image/png;base64," in html)
st, _, html, _ = req(admin, "POST", ADMIN + "/setup-2fa", {"csrf": csrf(html), "code": "000000"})
check("wrong TOTP rejected", st == 400)
st, _, html, _ = req(admin, "POST", ADMIN + "/setup-2fa", {"csrf": csrf(html), "code": totp(secret)})
codes = re.findall(r"<div>([a-z0-9]{4}-[a-z0-9]{4})</div>", html)
check("2FA enabled, 10 recovery codes shown", st == 200 and len(codes) == 10, len(codes))
st, _, html, _ = req(admin, "GET", ADMIN + "/")
check("admin UI served", st == 200 and "admin.js" in html)

# ------------------------------------------------------------------ admin API
st, _, body, _ = req(admin, "GET", ADMIN + "/api/me")
me = json.loads(body)
check("GET /api/me", st == 200 and me["user"]["username"] == "testadmin" and me["user"]["totpEnabled"])
st, _, body, _ = req(admin, "POST", ADMIN + "/api/users", headers={"X-Wicket": "0"}, json_body={"username": "x", "role": "user", "password": "x"})
check("mutation without X-Wicket header refused", st == 403)
st, _, body, _ = req(admin, "POST", ADMIN + "/api/users", json_body={"username": "anna", "email": "anna@example.com", "role": "user", "password": PW_ANNA})
anna_id = json.loads(body).get("id")
check("create user anna", st == 201 and anna_id, body[:120])
st, _, body, _ = req(admin, "POST", ADMIN + "/api/sites", json_body={"domain": "app.localtest.me", "target": "", "access": "users", "users": [anna_id], "require2fa": False, "bypass": ["/healthz", "/public/*"], "enabled": True, "managed": False})
check("create site app (users: anna, unmanaged)", st == 201, body[:160])
st, _, body, _ = req(admin, "POST", ADMIN + "/api/sites", json_body={"domain": "secret.localtest.me", "target": "", "access": "admins", "users": [], "require2fa": False, "bypass": [], "enabled": True, "managed": False})
check("create site secret (admins only)", st == 201, body[:160])
st, _, body, _ = req(admin, "POST", ADMIN + "/api/sites", json_body={"domain": "bad domain", "access": "all", "bypass": [], "enabled": True, "managed": False})
check("invalid domain rejected", st == 400 and "Domain" in body)
st, _, body, _ = req(admin, "POST", ADMIN + "/api/sites", json_body={"domain": "x.localtest.me", "access": "all", "bypass": ["/a*b"], "enabled": True, "managed": False})
check("invalid bypass rejected", st == 400)
st, _, body, _ = req(admin, "GET", ADMIN + "/api/overview")
ov = json.loads(body)
check("overview", st == 200 and ov["stats"]["sites"] == 2, ov.get("stats"))

# ------------------------------------------------------------------ forward auth
st, h, _, _ = verify("app.localtest.me", "/dashboard")
check("verify anonymous -> redirect to login", st == 302 and h["Location"].startswith("https://login.localtest.me/?rd="), h.get("Location"))
st, _, _, _ = verify("app.localtest.me", "/healthz")
check("verify bypass /healthz", st == 200)
st, _, _, _ = verify("app.localtest.me", "/public/img/a.png")
check("verify bypass /public/*", st == 200)
st, _, _, _ = verify("app.localtest.me", "/healthz/../admin")
check("path traversal does not bypass", st == 302)
st, _, _, _ = verify("unknown.localtest.me", "/")
check("unknown domain -> 403", st == 403)
st, _, _, _ = verify("login.localtest.me", "/")
check("verify ignores Wicket's own hosts", st == 404)

# anna logs in through the login host
anna, jjar = client(follow=False)
rd = "https://app.localtest.me/dashboard"
st, _, html, _ = req(anna, "GET", LOGIN + "/?rd=" + urllib.parse.quote(rd))
check("site login page shows target", st == 200 and "app.localtest.me" in html)
st, h, _, _ = req(anna, "POST", LOGIN + "/login", {"csrf": csrf(html), "rd": rd, "username": "anna", "password": PW_ANNA, "remember": "1"})
check("anna login redirects back to rd", st == 303 and h.get("Location") == rd, h.get("Location"))
ck = cookie_header(jjar)
dom = [c.domain for c in jjar if c.name == "wicket_session"]
check("session cookie scoped to parent domain", dom and dom[0].lstrip(".") == "localtest.me", dom)
st, h, _, _ = verify("app.localtest.me", "/dashboard", ck)
check("anna allowed on app, Remote-User header", st == 200 and h.get("Remote-User") == "anna", h.get("Remote-User"))
st, h, _, _ = verify("secret.localtest.me", "/", ck)
check("anna denied on admins-only site", st == 302 and "/denied" in h.get("Location", ""), h.get("Location"))
st, h, _, _ = req(anna, "POST", LOGIN + "/login", {"csrf": csrf(html), "rd": "https://evil.example.com/", "username": "anna", "password": PW_ANNA})
check("open redirect blocked", st == 303 and h.get("Location") == "/", h.get("Location"))
st, _, body, _ = req(anna, "GET", ADMIN + "/api/me")
check("non-admin cannot use admin API", st == 401)

# admin 2FA login with a recovery code (TOTP step already used)
adm2, ajar2 = client(follow=False)
st, _, html, _ = req(adm2, "GET", ADMIN + "/login")
check("admin login page (split)", st == 200 and "Willkommen zurück" in html)
st, h, _, _ = req(adm2, "POST", ADMIN + "/login", {"csrf": csrf(html), "username": "testadmin", "password": PW_ADMIN})
check("password ok -> 2FA step", st == 303 and h["Location"].startswith("/2fa"), h.get("Location"))
st, _, html, _ = req(adm2, "GET", ADMIN + "/2fa?recovery=1")
st, h, _, _ = req(adm2, "POST", ADMIN + "/2fa", {"csrf": csrf(html), "mode": "recovery", "recovery": codes[0]})
check("recovery code accepted", st == 303 and h.get("Location") == "/", h.get("Location"))
st, h, _, _ = req(adm2, "POST", ADMIN + "/login", {"csrf": csrf(html), "username": "testadmin", "password": PW_ADMIN})
st, _, html, _ = req(adm2, "GET", ADMIN + "/2fa?recovery=1")
st, _, html, _ = req(adm2, "POST", ADMIN + "/2fa", {"csrf": csrf(html), "mode": "recovery", "recovery": codes[0]})
check("recovery code is single-use", st == 401)

# log & sessions
st, _, body, _ = req(admin, "GET", ADMIN + "/api/events?range=24h")
kinds = {e["kind"] for e in json.loads(body)["items"]}
check("events logged", {"login_ok", "denied", "mfa_enabled", "recovery_used", "mfa_fail"} <= kinds, sorted(kinds))
st, h, body, _ = req(admin, "GET", ADMIN + "/api/events.csv?range=24h")
check("CSV export", st == 200 and "Zeit;Ereignis" in body)
st, _, body, _ = req(admin, "GET", ADMIN + f"/api/users/{anna_id}/sessions")
check("anna has active sessions", st == 200 and len(json.loads(body)["sessions"]) >= 1)
st, _, _, _ = req(admin, "POST", ADMIN + f"/api/users/{anna_id}/logout", json_body={})
st, _, _, _ = verify("app.localtest.me", "/dashboard", ck)
check("revoked session no longer passes", st == 302)

# brute force (last – locks 127.0.0.1)
bf, _ = client(follow=False)
st, _, html, _ = req(bf, "GET", LOGIN + "/")
codes_seen = []
for i in range(6):
    st, _, html2, _ = req(bf, "POST", LOGIN + "/login", {"csrf": csrf(html), "username": "anna", "password": "wrong"})
    codes_seen.append(st)
check("locked after 5 failures (429)", codes_seen[-1] == 429 and "Vorübergehend gesperrt" in html2, codes_seen)

print(f"\n{sum(results)}/{len(results)} passed")
sys.exit(0 if all(results) else 1)
