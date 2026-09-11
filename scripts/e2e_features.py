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
        return resp.status, resp.headers, resp.read().decode(), resp.geturl()
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

print(f"\n{sum(results)}/{len(results)} passed")
sys.exit(0 if all(results) else 1)
