#!/usr/bin/env python3
"""Fill a throwaway Wicket instance with demo data and dump pages for the README screenshots.

All hosts are under example.com and resolved to 127.0.0.1 inside this script;
IP addresses come from the documentation ranges (RFC 5737).
Usage: python3 seed_demo.py <port> <setup-code> <out-dir>
"""
import base64, hashlib, hmac, http.cookiejar, json, os, re, socket, struct, sys, time, urllib.error, urllib.parse, urllib.request

PORT, SETUP_CODE, OUT = sys.argv[1], sys.argv[2], sys.argv[3]
os.makedirs(OUT, exist_ok=True)

_gai = socket.getaddrinfo
socket.getaddrinfo = lambda host, *a, **k: _gai("127.0.0.1" if str(host).endswith("example.com") else host, *a, **k)

ADMIN = f"http://wicket.example.com:{PORT}"
LOGIN = f"http://login.example.com:{PORT}"
PW = "demo-" + hashlib.sha1(str(time.time()).encode()).hexdigest()[:14]
UA = {
    "mac": "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
    "win": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36 Edg/128.0",
    "iphone": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
    "linux": "Mozilla/5.0 (X11; Linux x86_64; rv:129.0) Gecko/20100101 Firefox/129.0",
    "curl": "python-requests/2.32",
}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None


def client(ip, ua, follow=True):
    jar = http.cookiejar.CookieJar()
    hs = [urllib.request.HTTPCookieProcessor(jar)] + ([] if follow else [NoRedirect()])
    op = urllib.request.build_opener(*hs)
    op.addheaders = [("X-Forwarded-For", ip), ("User-Agent", UA[ua])]
    return op


def req(op, method, url, data=None, json_body=None):
    headers, body = {}, None
    if json_body is not None:
        body, headers = json.dumps(json_body).encode(), {"Content-Type": "application/json", "X-Wicket": "1"}
    elif data is not None:
        body, headers = urllib.parse.urlencode(data).encode(), {"Content-Type": "application/x-www-form-urlencoded"}
    try:
        r = op.open(urllib.request.Request(url, data=body, method=method, headers=headers), timeout=15)
        return r.status, r.read().decode(), r.geturl()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(), url


def csrf(html):
    m = re.search(r'name="csrf" value="([^"]+)"', html)
    return m.group(1) if m else ""


def totp(secret):
    key = base64.b32decode(secret + "=" * (-len(secret) % 8))
    mac = hmac.new(key, struct.pack(">Q", int(time.time() // 30)), hashlib.sha1).digest()
    off = mac[-1] & 0x0F
    return "%06d" % ((struct.unpack(">I", mac[off:off + 4])[0] & 0x7FFFFFFF) % 1000000)


def save(name, text):
    with open(os.path.join(OUT, name), "w", encoding="utf-8") as f:
        f.write(text)


def must(cond, what):
    if not cond:
        sys.exit("seed failed: " + what)


# first-run setup
admin = client("203.0.113.10", "mac")
st, html, _ = req(admin, "GET", ADMIN + "/setup")
save("setup.html", html)
st, html, _ = req(admin, "POST", ADMIN + "/setup", {"csrf": csrf(html), "code": SETUP_CODE, "username": "alex", "password": PW, "password2": PW})
st, html, _ = req(admin, "POST", ADMIN + "/setup", {"csrf": csrf(html), "cookieDomain": "example.com", "loginHost": "login.example.com", "adminHost": "wicket.example.com"})
secret = re.search(r'data-copy="([A-Z2-7]{32})"', html)
must(secret, "2FA setup page")
st, html, _ = req(admin, "POST", ADMIN + "/setup-2fa", {"csrf": csrf(html), "code": totp(secret.group(1))})
must(st == 200, "2FA confirm")
req(admin, "PUT", ADMIN + "/api/users/me", json_body={"email": "alex@example.com"})

ids = {}
for name, role in [("anna", "user"), ("ben", "user"), ("clara", "user"), ("sam", "admin")]:
    st, body, _ = req(admin, "POST", ADMIN + "/api/users", json_body={"username": name, "email": f"{name}@example.com", "role": role, "password": PW})
    must(st == 201, "user " + name + " " + body)
    ids[name] = json.loads(body)["id"]

sites = [
    ("grafana.example.com", "127.0.0.1:3000", "admins", [], True, []),
    ("app.example.com", "127.0.0.1:8080", "all", [], False, ["/healthz", "/api/public/*"]),
    ("files.example.com", "127.0.0.1:8384", "users", ["anna", "ben"], False, []),
    ("status.example.com", "127.0.0.1:3001", "all", [], False, ["/api/badge/*"]),
    ("n8n.example.com", "127.0.0.1:5678", "admins", [], True, ["/webhook/*"]),
    ("docs.example.com", "127.0.0.1:4000", "users", ["clara"], False, []),
]
for domain, target, access, users, mfa, bypass in sites:
    body = {"domain": domain, "target": target, "access": access, "users": [ids[u] for u in users],
            "require2fa": mfa, "bypass": bypass, "enabled": domain != "docs.example.com", "managed": True}
    st, text, _ = req(admin, "POST", ADMIN + "/api/sites", json_body=body)
    if st != 201:  # no Caddy directory in the throwaway container
        body["managed"] = False
        st, text, _ = req(admin, "POST", ADMIN + "/api/sites", json_body=body)
    must(st == 201, "site " + domain + " " + text)

# some traffic for the log
def site_login(user, ip, ua, rd, password=PW):
    op = client(ip, ua, follow=False)
    st, html, _ = req(op, "GET", LOGIN + "/?rd=" + urllib.parse.quote(rd))
    return req(op, "POST", LOGIN + "/login", {"csrf": csrf(html), "rd": rd, "username": user, "password": password, "remember": "1"})

site_login("clara", "198.51.100.23", "win", "https://docs.example.com/")
site_login("ben", "198.51.100.7", "linux", "https://files.example.com/", "wrong-password")
site_login("ben", "198.51.100.7", "linux", "https://files.example.com/")
site_login("anna", "203.0.113.42", "iphone", "https://files.example.com/")
site_login("anna", "203.0.113.42", "iphone", "https://app.example.com/")
for _ in range(5):
    site_login("root", "192.0.2.66", "curl", "https://grafana.example.com/", "admin")
site_login("anna", "203.0.113.24", "mac", "https://app.example.com/")

# settings: English, glass template for protected sites
st, body, _ = req(admin, "GET", ADMIN + "/api/settings")
s = json.loads(body)["settings"]
s.update(language="en", loginTemplate="glass")
st, text, _ = req(admin, "PUT", ADMIN + "/api/settings", json_body=s)
must(st == 200, "settings " + text)

# dumps
for name, path in [("overview", "/api/overview"), ("me", "/api/me"), ("settings", "/api/settings"), ("oauth", "/api/oauth"),
                   ("sites", "/api/sites"), ("users", "/api/users"), ("events", "/api/events?range=24h"),
                   ("sessions", f"/api/users/{ids['anna']}/sessions")]:
    st, body, _ = req(admin, "GET", ADMIN + path)
    must(st == 200, "dump " + path)
    save(name + ".json", body)

anon = client("203.0.113.24", "mac")
for tpl in ["glass", "centered", "split", "light", "dock", "terminal"]:
    s["loginTemplate"] = tpl
    req(admin, "PUT", ADMIN + "/api/settings", json_body=s)
    st, html, _ = req(anon, "GET", LOGIN + "/?rd=" + urllib.parse.quote("https://grafana.example.com/"))
    save(f"login-{tpl}.html", html)
s["loginTemplate"] = "glass"
req(admin, "PUT", ADMIN + "/api/settings", json_body=s)
st, html, _ = req(client("203.0.113.24", "mac"), "GET", ADMIN + "/login")
save("admin-login.html", html)
print("seeded", OUT)
