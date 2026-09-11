'use strict';

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = (v) => String(v ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

const view = $('#view');
const state = {
  me: null, timer: null, current: null,
  sitesData: null, caddy: null, sitesFilter: 'all', sitesQuery: '',
  users: [], userSel: null, meId: null,
  log: { kind: '', range: '24h', site: '', page: 1 },
};

// ---------------------------------------------------------------- api & helpers

async function api(method, url, body) {
  const res = await fetch(url, {
    method, credentials: 'same-origin',
    headers: { 'X-Wicket': '1', ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (res.status === 401) { location.href = '/login'; throw new Error('Nicht angemeldet'); }
  if (res.status === 403 && data.error === '2fa_setup_required') { location.href = '/setup-2fa'; throw new Error('Zwei-Faktor erforderlich'); }
  if (!res.ok) throw new Error(data.error || `Fehler ${res.status}`);
  return data;
}

function toast(msg, kind = '') {
  const t = $('#toast');
  t.textContent = msg;
  t.className = 'show ' + kind;
  clearTimeout(toast.t);
  toast.t = setTimeout(() => { t.className = ''; }, 3200);
}

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

function ago(ts) {
  if (!ts) return 'noch nie';
  const d = Date.now() / 1000 - ts;
  if (d < 45) return 'gerade eben';
  if (d < 3600) return `vor ${Math.round(d / 60)} Min.`;
  if (d < 86400) return `vor ${Math.round(d / 3600)} Std.`;
  if (d < 86400 * 30) return `vor ${plural(Math.round(d / 86400), 'Tag', 'Tagen')}`;
  return new Date(ts * 1000).toLocaleDateString('de-DE');
}

function stamp(ts) {
  const d = new Date(ts * 1000);
  return d.toLocaleDateString('de-DE', { day: '2-digit', month: '2-digit' }) + '. ' + d.toLocaleTimeString('de-DE');
}

function device(ua) {
  if (!ua) return '';
  if (/^(curl|wget|python|go-http|okhttp|java|node)/i.test(ua)) return ua.split(/[ /]/)[0];
  const b = /Edg\//.test(ua) ? 'Edge' : /OPR\//.test(ua) ? 'Opera' : /Firefox\//.test(ua) ? 'Firefox'
    : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'Browser';
  const o = /iPhone/.test(ua) ? 'iPhone' : /iPad/.test(ua) ? 'iPad' : /Android/.test(ua) ? 'Android'
    : /Windows/.test(ua) ? 'Windows' : /Mac OS X/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : '';
  return o ? `${b}, ${o}` : b;
}

function genPassword() {
  const abc = 'abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  return Array.from(crypto.getRandomValues(new Uint32Array(20)), (n) => abc[n % abc.length]).join('');
}

const I = {
  plus: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none"><path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>',
  search: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none"><circle cx="7" cy="7" r="4.5" stroke="currentColor" stroke-width="1.4"/><path d="M10.5 10.5L14 14" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
  ok: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none"><path d="M3 8.5l3 3 7-7" stroke="#50e3c2" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  fail: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none"><path d="M4 4l8 8M12 4l-8 8" stroke="#ee5253" stroke-width="1.6" stroke-linecap="round"/></svg>',
  lock: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none"><rect x="3.5" y="7" width="9" height="6.5" rx="1.5" stroke="#f5a623" stroke-width="1.4"/><path d="M5.5 7V5.5a2.5 2.5 0 015 0V7" stroke="#f5a623" stroke-width="1.4"/></svg>',
  deny: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="5.5" stroke="#a1a1a1" stroke-width="1.4"/><path d="M4.5 11.5l7-7" stroke="#a1a1a1" stroke-width="1.4"/></svg>',
  out: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none"><path d="M10 3h3v10h-3M7 11l3-3-3-3M10 8H3" stroke="#a1a1a1" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  key: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none"><circle cx="5.5" cy="10.5" r="3" stroke="#f5a623" stroke-width="1.4"/><path d="M7.7 8.3L13 3m-2 2l1.5 1.5" stroke="#f5a623" stroke-width="1.4" stroke-linecap="round"/></svg>',
  edit: '<svg width="14" height="14" viewBox="0 0 16 16" fill="none"><path d="M3 13h2.5L13 5.5 10.5 3 3 10.5V13z" stroke="#666" stroke-width="1.3" stroke-linejoin="round"/></svg>',
  laptop: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><rect x="1.5" y="3" width="13" height="8.5" rx="1.5" stroke="#a1a1a1" stroke-width="1.3"/><path d="M5 14h6" stroke="#a1a1a1" stroke-width="1.3" stroke-linecap="round"/></svg>',
  phone: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><rect x="4" y="1.5" width="8" height="13" rx="1.8" stroke="#a1a1a1" stroke-width="1.3"/></svg>',
};

// kind -> [icon, short text (overview), label (log), class]
const EV = {
  login_ok: ['ok', 'angemeldet', 'Anmeldung', ''],
  login_fail_password: ['fail', 'falsches Passwort', 'Falsches Passwort', 'err-t'],
  login_fail_user: ['fail', 'unbekannter Benutzer', 'Unbekannter Benutzer', 'err-t'],
  mfa_fail: ['fail', 'falscher 2FA-Code', 'Falscher 2FA-Code', 'err-t'],
  denied: ['deny', 'kein Zugriff', 'Kein Zugriff', ''],
  locked: ['lock', 'gesperrt', 'IP gesperrt', 'warn-t'],
  logout: ['out', 'abgemeldet', 'Abgemeldet', ''],
  recovery_used: ['key', 'Wiederherstellungscode genutzt', 'Wiederherstellungscode', 'warn-t'],
  mfa_enabled: ['ok', '2FA eingerichtet', '2FA eingerichtet', ''],
  user_created: ['edit', 'Benutzer angelegt', 'Benutzer angelegt', ''],
  user_updated: ['edit', 'Benutzer geändert', 'Benutzer geändert', ''],
  user_deleted: ['edit', 'Benutzer gelöscht', 'Benutzer gelöscht', ''],
  password_set: ['edit', 'Passwort gesetzt', 'Passwort gesetzt', ''],
  mfa_reset: ['edit', '2FA zurückgesetzt', '2FA zurückgesetzt', ''],
  sessions_revoked: ['edit', 'überall abgemeldet', 'Sitzungen beendet', ''],
  site_created: ['edit', 'Seite angelegt', 'Seite angelegt', ''],
  site_updated: ['edit', 'Seite geändert', 'Seite geändert', ''],
  site_deleted: ['edit', 'Seite gelöscht', 'Seite gelöscht', ''],
  settings_changed: ['edit', 'Einstellungen geändert', 'Einstellungen geändert', ''],
};
const ev = (kind) => EV[kind] || ['edit', kind, kind, ''];

const accessLabel = (s) => s.access === 'all' ? 'Alle Benutzer' : s.access === 'admins' ? 'Nur Admins' : plural(s.users.length, 'Benutzer', 'Benutzer');
const initial = (d) => (d.replace(/^\*\./, '')[0] || '?').toUpperCase();

// ---------------------------------------------------------------- dialogs

function closeDialog() { $('#modal').innerHTML = ''; }

function dialog({ title, lead = '', body, submit, submitClass = 'btn-light', width = 560, footLeft = '', onSubmit, onMount }) {
  $('#modal').innerHTML = `<div class="backdrop"><form class="dialog" style="max-width:${width}px" novalidate>
    <div class="dlg-head"><h2>${title}</h2>${lead ? `<p class="muted">${lead}</p>` : ''}</div>
    <div class="dlg-body">${body}<div class="alert" data-err hidden></div></div>
    <div class="dlg-foot">${footLeft}<button type="button" class="btn right" data-close>${submit ? 'Abbrechen' : 'Schließen'}</button>${submit ? `<button type="submit" class="${submitClass}">${submit}</button>` : ''}</div>
  </form></div>`;
  const form = $('#modal form');
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const err = $('[data-err]', form);
    err.hidden = true;
    const btn = $('[type=submit]', form);
    if (btn) btn.disabled = true;
    try {
      const keep = await onSubmit(form);
      if (keep !== true) closeDialog();
    } catch (ex) {
      err.textContent = ex.message;
      err.hidden = false;
    }
    if (btn) btn.disabled = false;
  });
  if (onMount) onMount(form);
  const first = $('input:not([type=hidden]):not([type=checkbox]), select', form);
  if (first) first.focus();
  return form;
}

function confirmDialog(title, text, button, danger = false) {
  return new Promise((resolve) => {
    dialog({
      title, lead: text, body: '', submit: button, submitClass: danger ? 'btn-danger' : 'btn-light', width: 440,
      onSubmit: () => resolve(true),
    });
    $('#modal [data-close]').addEventListener('click', () => resolve(false));
  });
}

function codesDialog(codes, username) {
  const text = codes.join('\n');
  const file = 'data:text/plain;charset=utf-8,' + encodeURIComponent(`Wicket – Wiederherstellungscodes für ${username}\n\n${text}\n`);
  dialog({
    title: 'Wiederherstellungscodes',
    lead: 'Falls du dein Handy verlierst. Jeder Code funktioniert genau einmal und wird nur jetzt angezeigt – sicher aufbewahren, z. B. in Vaultwarden.',
    width: 600,
    body: `<div class="codes">${codes.map((c) => `<div>${esc(c)}</div>`).join('')}</div>
      <div class="row gap8"><button type="button" class="btn sm" data-copy-text="${esc(text)}">Alle kopieren</button><a class="btn sm" download="wicket-wiederherstellungscodes.txt" href="${esc(file)}">Herunterladen</a></div>`,
  });
}

$('#modal').addEventListener('click', async (e) => {
  if (e.target.classList.contains('backdrop') || e.target.closest('[data-close]')) { closeDialog(); return; }
  const c = e.target.closest('[data-copy-text]');
  if (c) { await navigator.clipboard.writeText(c.dataset.copyText); toast('Kopiert'); }
});
document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && $('#modal').innerHTML) closeDialog(); });

// ---------------------------------------------------------------- router

const routes = { '': renderOverview, sites: renderSites, users: renderUsers, log: renderLog, settings: renderSettings };
const polling = new Set(['', 'log']);

function route() {
  clearInterval(state.timer);
  const [page = '', sub] = location.hash.replace(/^#\/?/, '').split('/');
  $$('#tabs a').forEach((a) => a.classList.toggle('on', a.dataset.tab === page));
  const fn = routes[page] || renderOverview;
  state.current = () => fn(sub);
  state.current().catch((e) => { view.innerHTML = `<div class="page"><div class="alert">${esc(e.message)}</div></div>`; });
  if (polling.has(page)) {
    state.timer = setInterval(() => {
      if (!document.hidden && !$('#modal').innerHTML && (page !== 'log' || state.log.page === 1)) state.current().catch(() => {});
    }, 15000);
  }
}
const refresh = () => state.current && state.current().catch((e) => toast(e.message, 'err'));
window.addEventListener('hashchange', route);

// ---------------------------------------------------------------- overview

const stat = (k, v, extra = '') => `<div class="stat"><div class="k">${k}</div><div class="v"><b>${v}</b>${extra}</div></div>`;

function siteMini(s) {
  const meta = s.managed ? '→ ' + esc(s.target) : 'eigener Caddy-Eintrag';
  const extra = s.bypass.length ? ' · ' + plural(s.bypass.length, 'Ausnahme', 'Ausnahmen') : '';
  return `<div class="row-item clickable" data-edit-site="${s.id}">
    <span class="dot ${s.enabled ? 'on' : ''}"></span>
    <div class="grow"><div class="strong ${s.enabled ? '' : 'muted'}">${esc(s.domain)}</div><div class="mono dim small">${meta}${extra}${s.enabled ? '' : ' · Schutz pausiert'}</div></div>
    <span class="muted small">${accessLabel(s)}</span>
    <span>${s.require2fa ? '<span class="tag">2FA Pflicht</span>' : '<span class="dim small">optional</span>'}</span>
  </div>`;
}

function eventMini(e) {
  const [icon, short] = ev(e.kind);
  const who = e.kind === 'locked' ? e.ip : (e.username || e.ip);
  const line2 = e.kind === 'locked' ? e.detail : [e.ip, device(e.ua), e.site].filter(Boolean).join(' · ');
  return `<div class="ev"><span class="ev-i">${I[icon]}</span>
    <div class="grow"><div><b>${esc(who)}</b> <span class="muted">${esc(short)}</span></div><div class="mono dim small">${esc(line2)}</div></div>
    <span class="dim small nowrap">${ago(e.at)}</span></div>`;
}

async function renderOverview() {
  const d = await api('GET', '/api/overview');
  state.sitesData = d.sites;
  const st = d.stats;
  const diff = st.fails24h - st.failsPrev;
  view.innerHTML = `<div class="page">
    <div class="page-head">
      <div><h1>Übersicht</h1><p class="muted">${plural(st.sites, 'Seite', 'Seiten')} geschützt</p></div>
      <div class="actions"><button class="btn" data-act="new-user">Benutzer anlegen</button><button class="btn-light" data-act="new-site">${I.plus}Domain hinzufügen</button></div>
    </div>
    <div class="stats">
      ${stat('Geschützte Seiten', st.sites)}
      ${stat('Aktive Sitzungen', st.sessions)}
      ${stat('Fehlversuche · 24 h', st.fails24h, diff > 0 ? `<span class="small warn-t">+${diff} seit gestern</span>` : '')}
      ${stat('Gesperrte IPs', st.locked)}
    </div>
    <div class="two-col">
      <section class="panel">
        <div class="panel-head"><h3>Seiten</h3><a class="muted small" href="#/sites">Alle anzeigen →</a></div>
        ${d.sites.length ? d.sites.slice(0, 6).map(siteMini).join('') : `<div class="empty">Noch keine Seite geschützt.<button class="btn-light sm" data-act="new-site">Domain hinzufügen</button></div>`}
      </section>
      <section class="panel">
        <div class="panel-head"><h3>Letzte Anmeldungen</h3><span class="live"><i></i>Live</span></div>
        ${d.events.length ? d.events.map(eventMini).join('') : '<div class="empty">Noch keine Anmeldungen.</div>'}
      </section>
    </div>
  </div>`;
}

// ---------------------------------------------------------------- sites

function caddyNotice(c) {
  if (!c) return '';
  if (!c.enabled) return `<div class="info" style="margin-bottom:16px">Caddy-Verwaltung ist aus – der Ordner <code>${esc(c.dir)}</code> ist nicht eingebunden. Du kannst Seiten trotzdem schützen, indem du in ihrem Caddy-Block <code>import wicket</code> ergänzt.</div>`;
  if (!c.imported) return `<div class="info warn" style="margin-bottom:16px">Dein Caddyfile enthält noch nicht <code>${esc(c.importLine)}</code>. Erst mit dieser Zeile werden Seiten, die Wicket verwaltet, aktiv.</div>`;
  if (!c.reachable) return `<div class="info warn" style="margin-bottom:16px">Die Caddy-Admin-API unter <code>${esc(c.admin)}</code> ist nicht erreichbar.</div>`;
  return '';
}

function siteRow(s) {
  return `<div class="tr sites-grid clickable ${s.enabled ? '' : 'paused'}" data-edit-site="${s.id}">
    <div class="cell-domain"><span class="fav-sq ${s.enabled ? '' : 'dim'}">${esc(initial(s.domain))}</span><div>
      <div class="strong">${esc(s.domain)}</div>${s.enabled ? '' : '<div class="small warn-t">Schutz pausiert – Seite ist offen</div>'}</div></div>
    <div class="mono small ${s.enabled ? 'muted' : 'dim'}">${s.managed ? esc(s.target) : 'eigener Caddy-Eintrag'}</div>
    <div class="${s.enabled ? 'muted' : 'dim'}">${accessLabel(s)}</div>
    <div>${s.require2fa ? '<span class="tag">Pflicht</span>' : '<span class="dim small">optional</span>'}</div>
    <div class="chips-sm">${s.bypass.length ? s.bypass.map((b) => `<span class="code-chip">${esc(b)}</span>`).join('') : '<span class="dim">–</span>'}</div>
    <div class="tr-end"><button type="button" class="switch ${s.enabled ? 'on' : ''}" data-toggle-site="${s.id}" role="switch" aria-checked="${s.enabled}" aria-label="Schutz umschalten"></button></div>
  </div>`;
}

function drawSites() {
  const all = state.sitesData || [];
  const q = state.sitesQuery.toLowerCase();
  const list = all.filter((s) => (state.sitesFilter === 'all' || (state.sitesFilter === 'on') === s.enabled) && (!q || s.domain.includes(q)));
  const counts = { all: all.length, on: all.filter((s) => s.enabled).length, off: all.filter((s) => !s.enabled).length };
  $('#siteSeg').innerHTML = [['all', 'Alle'], ['on', 'Aktiv'], ['off', 'Pausiert']]
    .map(([k, l]) => `<button type="button" class="${state.sitesFilter === k ? 'on' : ''}" data-sites-filter="${k}">${l} ${counts[k]}</button>`).join('');
  $('#siteRows').innerHTML = list.length ? list.map(siteRow).join('')
    : `<div class="empty">${all.length ? 'Keine Seite passt zum Filter.' : 'Noch keine Seite geschützt.<button class="btn-light sm" data-act="new-site">Domain hinzufügen</button>'}</div>`;
}

async function renderSites() {
  const d = await api('GET', '/api/sites');
  state.sitesData = d.sites;
  state.caddy = d.caddy;
  view.innerHTML = `<div class="page">
    <div class="page-head">
      <div><h1>Seiten</h1><p class="muted">Domains, die Wicket schützt.${d.caddy.enabled && d.caddy.imported ? ' Wicket pflegt die Caddy-Einträge automatisch.' : ''}</p></div>
      <div class="actions"><button class="btn-light" data-act="new-site">${I.plus}Domain hinzufügen</button></div>
    </div>
    ${caddyNotice(d.caddy)}
    <div class="toolbar">
      <label class="search">${I.search}<input id="siteSearch" placeholder="Domain suchen" value="${esc(state.sitesQuery)}" autocomplete="off"></label>
      <div class="seg" id="siteSeg"></div>
    </div>
    <div class="table">
      <div class="tr th sites-grid"><div>Domain</div><div>Ziel</div><div>Zugriff</div><div>2FA</div><div>Ausnahmen</div><div class="tr-end">Schutz</div></div>
      <div id="siteRows"></div>
    </div>
  </div>`;
  $('#siteSearch').addEventListener('input', (e) => { state.sitesQuery = e.target.value.trim(); drawSites(); });
  drawSites();
}

function sitePreview(d) {
  const dom = esc(d.domain || 'app.example.com');
  if (d.managed) return `<b>${dom}</b> {\n  <i>import</i> wicket\n  <i>reverse_proxy</i> ${esc(d.target || '127.0.0.1:8080')}\n}`;
  const note = state.caddy && state.caddy.caddyfileWritable
    ? '# Wicket ergänzt das in deinem Caddyfile automatisch:'
    : '# In deinem bestehenden Caddy-Block ergänzen:';
  return `${note}\n<b>${dom}</b> {\n  <i>import</i> wicket\n  …\n}`;
}

async function siteDialog(site) {
  const edit = Boolean(site);
  if (!state.caddy) state.caddy = (await api('GET', '/api/sites')).caddy;
  const users = (await api('GET', '/api/users')).users.filter((u) => u.role !== 'admin');
  const d = site ? JSON.parse(JSON.stringify(site))
    : { domain: '', target: '', access: 'all', require2fa: false, bypass: [], enabled: true, managed: state.caddy.enabled, users: [] };

  const form = dialog({
    title: edit ? 'Seite bearbeiten' : 'Domain hinzufügen',
    lead: d.managed || !edit ? 'Wicket richtet den Proxy ein und schützt die Seite sofort.' : 'Die Seite nutzt deinen eigenen Caddy-Eintrag.',
    submit: edit ? 'Speichern' : 'Domain schützen',
    width: 580,
    footLeft: edit ? '<button type="button" class="btn danger" data-delete-site>Löschen</button>' : '',
    body: `
      <div class="fg"><div class="lbl">Domain</div>
        <input class="input" name="domain" value="${esc(d.domain)}" placeholder="app.example.com" autocomplete="off" spellcheck="false">
        <div class="dns" data-dns></div></div>
      <div class="info" data-caddy-info hidden></div>
      <div class="fg" data-target><div class="lbl">Ziel</div>
        <input class="input mono" name="target" value="${esc(d.target)}" placeholder="127.0.0.1:8080" autocomplete="off" spellcheck="false">
        <div class="dim small">Adresse, unter der der Dienst auf dem Server erreichbar ist, z. B. 127.0.0.1:5050 oder http://container:80.</div></div>
      <div class="grid2">
        <div class="fg"><div class="lbl">Zugriff</div>
          <select class="input" name="access">
            <option value="all" ${d.access === 'all' ? 'selected' : ''}>Alle Benutzer</option>
            <option value="admins" ${d.access === 'admins' ? 'selected' : ''}>Nur Admins</option>
            <option value="users" ${d.access === 'users' ? 'selected' : ''}>Ausgewählte Benutzer</option>
          </select></div>
        <div class="fg"><div class="lbl">Zwei-Faktor</div>
          <div class="input toggle-field">Pflicht<button type="button" class="switch ${d.require2fa ? 'on' : ''}" data-sw="require2fa" role="switch" aria-checked="${d.require2fa}"></button></div></div>
      </div>
      <div class="fg" data-userlist ${d.access === 'users' ? '' : 'hidden'}><div class="lbl">Benutzer mit Zugriff <span>– Admins dürfen immer</span></div>
        <div class="checklist">${users.length ? users.map((u) => `<label class="check"><input type="checkbox" value="${u.id}" ${d.users.includes(u.id) ? 'checked' : ''}><span class="box"></span>${esc(u.username)}</label>`).join('') : '<span class="dim small">Noch keine normalen Benutzer angelegt.</span>'}</div></div>
      <div class="fg"><div class="lbl">Ausnahmen <span>– Pfade ohne Login</span></div>
        <div class="chip-input" data-chips><input placeholder="Pfad eingeben, Enter" spellcheck="false"></div>
        <div class="dim small">z. B. /healthz oder /api/public/* – ein * ist nur am Ende erlaubt.</div></div>
      <div class="fg"><div class="lbl">Aktiv</div>
        <div class="input toggle-field">Seite ist geschützt<button type="button" class="switch ${d.enabled ? 'on' : ''}" data-sw="enabled" role="switch" aria-checked="${d.enabled}"></button></div></div>
      ${state.caddy.enabled ? `<label class="check" data-managed-row><input type="checkbox" name="managed" ${d.managed ? 'checked' : ''}><span class="box"></span>Caddy-Eintrag von Wicket anlegen lassen</label>` : ''}
      <div class="code-box"><div class="code-head"><span data-code-title></span><button type="button" class="link right" data-copy-code>Kopieren</button></div><pre data-preview></pre></div>`,
    async onSubmit() {
      d.users = $$('[data-userlist] input:checked', form).map((i) => Number(i.value));
      const chipInput = $('[data-chips] input', form);
      if (chipInput.value.trim()) { d.bypass.push(chipInput.value.trim()); chipInput.value = ''; }
      const saved = edit ? await api('PUT', `/api/sites/${site.id}`, d) : await api('POST', '/api/sites', d);
      toast(edit ? 'Gespeichert' : `${saved.domain} ist jetzt geschützt`);
      refresh();
    },
  });

  const update = () => {
    const existing = Boolean(d.caddyBlock);
    const auto = state.caddy.caddyfileWritable;
    $('[data-target]', form).hidden = !d.managed;
    $('[data-userlist]', form).hidden = d.access !== 'users';
    const row = $('[data-managed-row]', form);
    if (row) { row.hidden = existing; $('input', row).checked = d.managed; }
    const info = $('[data-caddy-info]', form);
    info.hidden = !existing;
    info.innerHTML = auto
      ? 'Für diese Domain gibt es schon einen Eintrag in deinem Caddyfile. Wicket ergänzt dort automatisch <code>import wicket</code> und schaltet ein vorhandenes Browser-Login (basic_auth) ab.'
      : 'Für diese Domain gibt es schon einen Eintrag in deinem Caddyfile. Wicket darf das Caddyfile nicht bearbeiten – ergänze dort <code>import wicket</code>.';
    $('[data-preview]', form).innerHTML = sitePreview(d);
    $('[data-code-title]', form).textContent = d.managed ? 'Caddy-Eintrag, den Wicket anlegt' : (auto ? 'So bindet Wicket die Seite ein' : 'So bindest du Wicket selbst ein');
  };
  const drawChips = () => {
    const box = $('[data-chips]', form);
    $$('.code-chip', box).forEach((c) => c.remove());
    d.bypass.forEach((b, i) => box.insertBefore(Object.assign(document.createElement('span'), {
      className: 'code-chip', innerHTML: `${esc(b)}<button type="button" data-rm="${i}" aria-label="Entfernen">×</button>`,
    }), $('input', box)));
  };
  let dnsTimer;
  const checkDNS = () => {
    clearTimeout(dnsTimer);
    const el = $('[data-dns]', form);
    const dom = d.domain.trim();
    if (!dom) { el.innerHTML = ''; return; }
    dnsTimer = setTimeout(async () => {
      el.innerHTML = '<span class="dim">DNS wird geprüft…</span>';
      try {
        const r = await api('GET', '/api/dns?domain=' + encodeURIComponent(dom));
        if (d.domain.trim() !== dom) return;
        d.caddyBlock = Boolean(r.caddyBlock);
        if (d.caddyBlock) d.managed = false;
        else if (!edit) d.managed = state.caddy.enabled;
        update();
        el.innerHTML = r.ok ? `${I.ok}<span class="ok-t">DNS zeigt auf diesen Server</span>`
          : r.reason === 'missing' ? '<span class="warn-t">Kein DNS-Eintrag gefunden – ohne ihn gibt es kein Zertifikat.</span>'
          : r.reason === 'other' ? `<span class="warn-t">DNS zeigt auf ${esc(r.ips.join(', '))}, nicht auf diesen Server.</span>` : '';
      } catch { el.innerHTML = ''; }
    }, 500);
  };

  form.addEventListener('input', (e) => {
    const n = e.target.name;
    if (n === 'domain') { d.domain = e.target.value.trim().toLowerCase(); checkDNS(); }
    if (n === 'target') d.target = e.target.value.trim();
    if (n === 'access') d.access = e.target.value;
    if (n === 'managed') d.managed = e.target.checked;
    update();
  });
  form.addEventListener('change', (e) => { if (e.target.name === 'access' || e.target.name === 'managed') form.dispatchEvent(new Event('input')); });
  form.addEventListener('click', async (e) => {
    const sw = e.target.closest('[data-sw]');
    if (sw) { d[sw.dataset.sw] = !d[sw.dataset.sw]; sw.classList.toggle('on', d[sw.dataset.sw]); sw.setAttribute('aria-checked', d[sw.dataset.sw]); }
    const rm = e.target.closest('[data-rm]');
    if (rm) { d.bypass.splice(Number(rm.dataset.rm), 1); drawChips(); }
    if (e.target.closest('[data-chips]') && !rm) $('[data-chips] input', form).focus();
    if (e.target.closest('[data-copy-code]')) { await navigator.clipboard.writeText($('[data-preview]', form).textContent); toast('Kopiert'); }
    if (e.target.closest('[data-delete-site]')) {
      closeDialog();
      if (await confirmDialog('Seite löschen', `${esc(site.domain)} wird nicht mehr von Wicket geschützt${site.managed ? ' und der Caddy-Eintrag entfernt' : ''}.`, 'Löschen', true)) {
        try { const r = await api('DELETE', `/api/sites/${site.id}`); toast(r.warning || 'Seite gelöscht', r.warning ? 'err' : ''); refresh(); } catch (ex) { toast(ex.message, 'err'); }
      }
    }
  });
  $('[data-chips] input', form).addEventListener('keydown', (e) => {
    const v = e.target.value.trim();
    if ((e.key === 'Enter' || e.key === ',' || e.key === ' ') && v) { e.preventDefault(); if (!d.bypass.includes(v)) d.bypass.push(v); e.target.value = ''; drawChips(); }
    else if (e.key === 'Enter') e.preventDefault();
    else if (e.key === 'Backspace' && !e.target.value && d.bypass.length) { d.bypass.pop(); drawChips(); }
  });
  drawChips();
  update();
  if (d.domain) checkDNS();
}

async function toggleSite(id) {
  const s = (state.sitesData || []).find((x) => x.id === id);
  if (!s) return;
  try {
    await api('PUT', `/api/sites/${id}`, { ...s, enabled: !s.enabled });
    toast(s.enabled ? `Schutz für ${s.domain} pausiert` : `${s.domain} ist wieder geschützt`);
    refresh();
  } catch (ex) { toast(ex.message, 'err'); }
}

// ---------------------------------------------------------------- users

function userRow(u) {
  return `<div class="tr users-grid clickable ${u.id === state.userSel ? 'sel' : ''}" data-user="${u.id}">
    <div class="cell-user"><span class="uavatar ${u.id === state.meId ? 'me' : ''}">${esc(u.username[0].toUpperCase())}</span>
      <div class="grow"><div class="strong ${u.disabled ? 'muted' : ''}">${esc(u.username)}${u.disabled ? ' <span class="tag dim">deaktiviert</span>' : ''}</div><div class="small dim">${esc(u.email || (u.id === state.meId ? 'das bist du' : '–'))}</div></div></div>
    <div>${u.role === 'admin' ? '<span class="tag">Admin</span>' : '<span class="muted small">Benutzer</span>'}</div>
    <div>${u.totpEnabled ? '<span class="status ok-t"><i></i>Aktiv</span>' : '<span class="status missing warn-t"><i></i>Fehlt</span>'}</div>
    <div class="muted small">${u.id === state.meId ? 'gerade eben' : ago(u.lastSeen)}</div>
  </div>`;
}

async function renderUsers() {
  const [ud, sd] = await Promise.all([api('GET', '/api/users'), api('GET', '/api/sites')]);
  state.users = ud.users;
  state.meId = ud.me;
  state.sitesData = sd.sites;
  if (!ud.users.some((u) => u.id === state.userSel)) state.userSel = ud.me;
  const with2fa = ud.users.filter((u) => u.totpEnabled).length;
  view.innerHTML = `<div class="page">
    <div class="page-head">
      <div><h1>Benutzer</h1><p class="muted">${plural(ud.users.length, 'Benutzer', 'Benutzer')} · ${with2fa} mit Zwei-Faktor</p></div>
      <div class="actions"><button class="btn-light" data-act="new-user">${I.plus}Benutzer anlegen</button></div>
    </div>
    <div class="users-layout">
      <div class="table">
        <div class="tr th users-grid"><div>Benutzer</div><div>Rolle</div><div>2FA</div><div>Zuletzt aktiv</div></div>
        ${ud.users.map(userRow).join('')}
      </div>
      <div class="detail" id="userDetail"></div>
    </div>
  </div>`;
  await drawUserDetail();
}

async function drawUserDetail() {
  const u = state.users.find((x) => x.id === state.userSel);
  const box = $('#userDetail');
  if (!u || !box) return;
  const { sessions } = await api('GET', `/api/users/${u.id}/sessions`);
  const sites = state.sitesData || [];
  const reach = u.role === 'admin' ? sites.length : sites.filter((s) => s.access === 'all' || (s.access === 'users' && s.users.includes(u.id))).length;
  const self = u.id === state.meId;
  box.innerHTML = `
    <div class="row gap8" style="gap:12px"><span class="uavatar lg ${self ? 'me' : ''}">${esc(u.username[0].toUpperCase())}</span>
      <div class="grow"><div class="strong" style="font-size:16px">${esc(u.username)}</div><div class="small muted">${u.role === 'admin' ? 'Admin' : 'Benutzer'} · seit ${new Date(u.createdAt * 1000).toLocaleDateString('de-DE')}</div></div>
      <button class="btn sm" data-act="edit-user">Bearbeiten</button></div>
    <div>
      <div class="kv"><span>Zugriff auf</span><span>${u.role === 'admin' ? `alle ${plural(sites.length, 'Seite', 'Seiten')}` : plural(reach, 'Seite', 'Seiten')}</span></div>
      <div class="kv"><span>Zwei-Faktor</span><span class="${u.totpEnabled ? 'ok-t' : 'warn-t'}">${u.totpEnabled ? 'Authenticator-App' : 'nicht eingerichtet'}</span></div>
      ${u.totpEnabled ? `<div class="kv"><span>Wiederherstellungscodes</span><span>${u.recoveryLeft} von 10 übrig</span></div>` : ''}
    </div>
    <div>
      <div class="strong small" style="margin-bottom:12px">Aktive Sitzungen</div>
      ${sessions.length ? sessions.map((s) => `<div class="sess">${/iPhone|Android|iPad/.test(s.ua) ? I.phone : I.laptop}
        <div class="grow"><div class="small">${esc(device(s.ua) || 'Unbekanntes Gerät')}${s.current ? ' <span class="ok-t">· dieses Gerät</span>' : ''}</div><div class="mono dim" style="font-size:11px">${esc(s.ip)} · ${ago(s.lastSeen)}</div></div>
        ${s.current ? '' : `<button class="link" data-end-session="${s.id}">Beenden</button>`}</div>`).join('') : '<div class="dim small">Keine aktive Sitzung.</div>'}
    </div>
    <div class="detail-actions">
      <button class="btn sm" data-act="user-password">Passwort ${self ? 'ändern' : 'setzen'}</button>
      ${u.totpEnabled && !self ? '<button class="btn sm" data-act="user-reset-2fa">2FA zurücksetzen</button>' : ''}
      <button class="btn sm danger" data-act="user-logout">Überall abmelden</button>
      ${self ? '' : '<button class="btn sm danger" data-act="user-delete">Löschen</button>'}
    </div>`;
}

function userDialog(u) {
  const edit = Boolean(u);
  dialog({
    title: edit ? `${esc(u.username)} bearbeiten` : 'Benutzer anlegen',
    submit: edit ? 'Speichern' : 'Anlegen',
    width: 520,
    body: `
      ${edit ? '' : `<div class="fg"><div class="lbl">Benutzername</div><input class="input" name="username" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="z. B. anna"></div>`}
      <div class="fg"><div class="lbl">E-Mail <span>optional</span></div><input class="input" name="email" type="email" value="${esc(u?.email || '')}" autocomplete="off"></div>
      <div class="grid2">
        <div class="fg"><div class="lbl">Rolle</div><select class="input" name="role">
          <option value="user" ${u?.role === 'user' ? 'selected' : ''}>Benutzer</option>
          <option value="admin" ${u?.role === 'admin' ? 'selected' : ''}>Admin</option></select></div>
        ${edit ? `<div class="fg"><div class="lbl">Status</div><select class="input" name="disabled"><option value="0">Aktiv</option><option value="1" ${u.disabled ? 'selected' : ''}>Deaktiviert</option></select></div>` : ''}
      </div>
      ${edit ? '' : `<div class="fg"><div class="lbl">Passwort <span>mindestens 10 Zeichen</span></div>
        <div class="row gap8"><input class="input mono" name="password" autocomplete="new-password"><button type="button" class="btn" data-gen>Erzeugen</button></div>
        <div class="dim small">Gib es dem Benutzer weiter – er kann es später selbst nicht ändern, ein Admin kann es neu setzen.</div></div>`}
      <div class="dim small">Admins verwalten Wicket und dürfen auf jede Seite. Benutzer dürfen nur, was bei der Seite freigegeben ist.</div>`,
    onMount(form) {
      const g = $('[data-gen]', form);
      if (g) g.addEventListener('click', () => { $('[name=password]', form).value = genPassword(); });
    },
    async onSubmit(form) {
      const val = (n) => $(`[name=${n}]`, form)?.value ?? '';
      if (edit) {
        await api('PUT', `/api/users/${u.id}`, { email: val('email'), role: val('role'), disabled: val('disabled') === '1' });
        toast('Gespeichert');
      } else {
        const created = await api('POST', '/api/users', { username: val('username').trim(), email: val('email'), role: val('role'), password: val('password') });
        state.userSel = created.id;
        toast(`${created.username} angelegt`);
        if (!location.hash.startsWith('#/users')) { location.hash = '#/users'; return; }
      }
      refresh();
    },
  });
}

function passwordDialog(u) {
  const self = u.id === state.meId;
  dialog({
    title: self ? 'Passwort ändern' : `Passwort für ${esc(u.username)} setzen`,
    lead: self ? '' : 'Alle Sitzungen dieses Benutzers werden beendet.',
    submit: 'Speichern',
    width: 480,
    body: `${self ? '<div class="fg"><div class="lbl">Aktuelles Passwort</div><input class="input" type="password" name="current" autocomplete="current-password"></div>' : ''}
      <div class="fg"><div class="lbl">Neues Passwort <span>mindestens 10 Zeichen</span></div>
      <div class="row gap8"><input class="input mono" name="password" autocomplete="new-password"><button type="button" class="btn" data-gen>Erzeugen</button></div></div>`,
    onMount(form) { $('[data-gen]', form).addEventListener('click', () => { $('[name=password]', form).value = genPassword(); }); },
    async onSubmit(form) {
      const pw = $('[name=password]', form).value;
      if (self) await api('PUT', '/api/me/password', { current: $('[name=current]', form).value, new: pw });
      else await api('PUT', `/api/users/${u.id}/password`, { password: pw });
      toast('Passwort gespeichert');
      refresh();
    },
  });
}

async function userAction(act) {
  const u = state.users.find((x) => x.id === state.userSel);
  if (!u) return;
  const self = u.id === state.meId;
  try {
    if (act === 'edit-user') userDialog(u);
    else if (act === 'user-password') passwordDialog(u);
    else if (act === 'user-reset-2fa') {
      if (!(await confirmDialog('2FA zurücksetzen', `${esc(u.username)} muss Zwei-Faktor danach neu einrichten.`, 'Zurücksetzen'))) return;
      await api('POST', `/api/users/${u.id}/reset-2fa`); toast('2FA zurückgesetzt'); refresh();
    } else if (act === 'user-logout') {
      if (!(await confirmDialog('Überall abmelden', self ? 'Du wirst auch hier abgemeldet.' : `Alle Sitzungen von ${esc(u.username)} werden beendet.`, 'Abmelden', true))) return;
      const r = await api('POST', `/api/users/${u.id}/logout`);
      if (r.self) { location.href = '/login'; return; }
      toast('Sitzungen beendet'); refresh();
    } else if (act === 'user-delete') {
      if (!(await confirmDialog('Benutzer löschen', `${esc(u.username)} und alle Sitzungen werden gelöscht.`, 'Löschen', true))) return;
      await api('DELETE', `/api/users/${u.id}`); state.userSel = null; toast('Benutzer gelöscht'); refresh();
    }
  } catch (ex) { toast(ex.message, 'err'); }
}

// ---------------------------------------------------------------- log

function eventRow(e) {
  const [icon, , label, cls] = ev(e.kind);
  const who = e.username || '–';
  return `<div class="tr log-grid ${e.kind === 'locked' ? 'lock-row' : ''}">
    <div class="mono small muted">${stamp(e.at)}</div>
    <span class="ev-i">${I[icon]}</span>
    <div class="${cls}" title="${esc(e.detail)}">${esc(label)}${e.detail && e.kind !== 'locked' ? ` <span class="dim small">· ${esc(e.detail)}</span>` : ''}${e.kind === 'locked' ? ` <span class="dim small">· ${esc(e.detail)}</span>` : ''}</div>
    <div class="muted" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(e.site || '–')}</div>
    <div style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(who)} <span class="dim">${e.ua ? '· ' + esc(device(e.ua)) : ''}</span></div>
    <div class="mono small muted">${esc(e.ip)}</div>
  </div>`;
}

async function renderLog() {
  const f = state.log;
  const qs = new URLSearchParams({ kind: f.kind, range: f.range, site: f.site, page: String(f.page) });
  const [d, sites] = await Promise.all([api('GET', '/api/events?' + qs), state.sitesData ? state.sitesData : api('GET', '/api/sites').then((r) => r.sites)]);
  state.sitesData = sites;
  const pages = Math.max(1, Math.ceil(d.total / d.pageSize));
  const kinds = [['', 'Alle'], ['ok', 'Erfolgreich'], ['fail', 'Fehlgeschlagen'], ['lock', 'Sperren'], ['admin', 'Änderungen']];
  const ranges = [['24h', 'Letzte 24 Stunden'], ['7d', 'Letzte 7 Tage'], ['30d', 'Letzte 30 Tage'], ['90d', 'Letzte 90 Tage']];
  view.innerHTML = `<div class="page">
    <div class="page-head">
      <div><h1>Protokoll</h1><p class="muted">Jede Anmeldung, jeder Fehlversuch, jede Sperre.</p></div>
      <div class="actions"><a class="btn" href="/api/events.csv?${qs}" download>Als CSV exportieren</a></div>
    </div>
    <div class="toolbar">
      <div class="seg">${kinds.map(([k, l]) => `<button type="button" class="${f.kind === k ? 'on' : ''}" data-log-kind="${k}">${l}</button>`).join('')}</div>
      <select class="select" data-log-range>${ranges.map(([k, l]) => `<option value="${k}" ${f.range === k ? 'selected' : ''}>${l}</option>`).join('')}</select>
      <select class="select" data-log-site><option value="">Alle Seiten</option>${sites.map((s) => `<option value="${esc(s.domain)}" ${f.site === s.domain ? 'selected' : ''}>${esc(s.domain)}</option>`).join('')}</select>
      <span class="live right"><i></i>Live</span>
    </div>
    <div class="table">
      <div class="tr th log-grid"><div>Zeit</div><div></div><div>Ereignis</div><div>Seite</div><div>Benutzer · Gerät</div><div>IP</div></div>
      ${d.items.length ? d.items.map(eventRow).join('') : '<div class="empty">Keine Einträge in diesem Zeitraum.</div>'}
    </div>
    <div class="pager"><span>${d.items.length ? `${(f.page - 1) * d.pageSize + 1}–${(f.page - 1) * d.pageSize + d.items.length} von ${d.total} Einträgen` : ''}</span>
      <div class="row gap8"><button class="btn sm" data-log-page="-1" ${f.page <= 1 ? 'disabled' : ''}>Zurück</button><button class="btn sm" data-log-page="1" ${f.page >= pages ? 'disabled' : ''}>Weiter</button></div></div>
  </div>`;
  $('[data-log-range]').addEventListener('change', (e) => { f.range = e.target.value; f.page = 1; refresh(); });
  $('[data-log-site]').addEventListener('change', (e) => { f.site = e.target.value; f.page = 1; refresh(); });
}

// ---------------------------------------------------------------- settings

const SECTIONS = [['allgemein', 'Allgemein'], ['sitzungen', 'Sitzungen'], ['sicherheit', 'Sicherheit'], ['caddy', 'Caddy'], ['konto', 'Mein Konto']];

async function renderSettings(sub) {
  const [d, me] = await Promise.all([api('GET', '/api/settings'), api('GET', '/api/me')]);
  state.me = me;
  const s = d.settings;
  const c = d.caddy;
  const u = me.user;
  const caddyOk = c.enabled && c.writable && c.reachable && c.imported;
  const save = (label = 'Speichern') => `<button type="submit" class="btn-light sm right">${label}</button>`;
  view.innerHTML = `<div class="page settings">
    <aside class="side-nav"><h1>Einstellungen</h1>${SECTIONS.map(([id, l]) => `<a href="#/settings/${id}" data-sec="${id}">${l}</a>`).join('')}</aside>
    <div class="cards">
      <form class="scard" id="allgemein" data-settings>
        <div class="scard-body"><h2>Hauptdomain</h2><p class="sub">Das Login-Cookie gilt für diese Domain und alle Subdomains – so reicht ein Login für alles.</p>
          <div class="fields">
            <label>Hauptdomain<input class="input mono" name="cookieDomain" value="${esc(s.cookieDomain.replace(/^\./, ''))}"></label>
            <label>Login-Adresse<input class="input mono" name="loginHost" value="${esc(s.loginHost)}"></label>
            <label>Admin-Adresse<input class="input mono" name="adminHost" value="${esc(s.adminHost)}"></label>
          </div></div>
        <div class="scard-foot"><span class="small dim">Eine neue Hauptdomain meldet alle Benutzer ab.</span>${save()}</div>
      </form>
      <form class="scard" id="sitzungen" data-settings>
        <div class="scard-body"><h2>Sitzungen</h2><p class="sub">Wie lange eine Anmeldung gültig bleibt.</p>
          <div class="inline-fields">Normale Anmeldung<input class="input mono" type="number" name="sessionHours" value="${s.sessionHours}" min="1" max="720">Stunden · „Angemeldet bleiben“<input class="input mono" type="number" name="rememberDays" value="${s.rememberDays}" min="1" max="365">Tage</div></div>
        <div class="scard-foot"><span class="small dim">Gilt für neue Anmeldungen.</span>${save()}</div>
      </form>
      <form class="scard" id="sicherheit" data-settings>
        <div class="scard-body"><h2>Schutz vor Passwort-Raten</h2><p class="sub">Nach zu vielen Fehlversuchen wird die IP vorübergehend gesperrt.</p>
          <div class="inline-fields"><input class="input mono" type="number" name="lockAttempts" value="${s.lockAttempts}" min="3" max="50">Fehlversuche in<input class="input mono" type="number" name="lockWindowMin" value="${Math.round(s.lockWindowSec / 60)}" min="1" max="60">Min. sperren für<input class="input mono" type="number" name="lockDurationMin" value="${Math.round(s.lockDurationSec / 60)}" min="1" max="1440">Min.</div>
          <div class="toggle-line"><div class="grow"><div class="strong">Zwei-Faktor für Admins erzwingen</div><div class="small muted">Admins ohne 2FA müssen es beim nächsten Login einrichten.</div></div>
            <button type="button" class="switch ${s.enforceAdmin2fa ? 'on' : ''}" data-sw-setting="enforceAdmin2fa" role="switch" aria-checked="${s.enforceAdmin2fa}"></button></div>
          <div class="toggle-line"><div class="grow"><div class="strong">Protokoll aufbewahren</div><div class="small muted">Ältere Einträge werden automatisch gelöscht.</div></div>
            <div class="inline-fields" style="margin:0"><input class="input mono" type="number" name="logRetentionDays" value="${s.logRetentionDays}" min="7" max="3650">Tage</div></div>
        </div>
        <div class="scard-foot">${save()}</div>
      </form>
      <div class="scard" id="caddy">
        <div class="scard-body"><h2>Caddy-Verbindung <span class="badge ${caddyOk ? 'ok' : 'warn'}"><i></i>${caddyOk ? 'Verbunden' : 'Unvollständig'}</span></h2>
          <p class="sub">Über die Admin-API legt Wicket Einträge für neue Domains an.</p>
          <div class="fields">
            <label>Admin-API<input class="input mono" value="${esc(c.admin)}" readonly></label>
            <label>Snippet-Ordner<input class="input mono" value="${esc(c.dir)}" readonly></label>
          </div>
          <div class="inline-fields small" style="gap:18px">
            <span class="${c.enabled && c.writable ? 'ok-t' : 'warn-t'}">${c.enabled ? (c.writable ? '✓ Ordner beschreibbar' : '✗ Ordner nicht beschreibbar') : '✗ Ordner nicht eingebunden'}</span>
            <span class="${c.reachable ? 'ok-t' : 'warn-t'}">${c.reachable ? '✓ Admin-API erreichbar' : '✗ Admin-API nicht erreichbar'}</span>
            <span class="${c.imported ? 'ok-t' : 'warn-t'}">${c.imported ? '✓ im Caddyfile eingebunden' : '✗ Caddyfile bindet den Ordner nicht ein'}</span>
            <span class="${c.caddyfileWritable ? 'ok-t' : 'muted'}">${c.caddyfileWritable ? '✓ bestehende Einträge werden automatisch geschützt' : '– Caddyfile schreibgeschützt: import wicket von Hand'}</span>
          </div>
          ${c.imported ? '' : `<div class="info" style="margin-top:16px">Füge diese Zeile in dein Caddyfile ein und lade Caddy neu:<br><code>${esc(c.importLine)}</code></div>`}
          <div class="info" style="margin-top:16px">Eigene Caddy-Blöcke schützt du mit einer Zeile: <code>import wicket</code> – Wicket prüft dann jede Anfrage über <code>${esc(d.authAddr)}</code>.</div>
        </div>
      </div>
      <div class="scard" id="konto">
        <div class="scard-body"><h2>Mein Konto</h2><p class="sub">Angemeldet als <b>${esc(u.username)}</b> · Wicket ${esc(d.version)}</p>
          <div class="toggle-line"><div class="grow"><div class="strong">Passwort</div><div class="small muted">Mindestens 10 Zeichen.</div></div><button type="button" class="btn sm" data-act="my-password">Ändern</button></div>
          <div class="toggle-line"><div class="grow"><div class="strong">Zwei-Faktor <span class="badge ${u.totpEnabled ? 'ok' : 'warn'}" style="margin-left:6px"><i></i>${u.totpEnabled ? 'Aktiv' : 'Aus'}</span></div>
            <div class="small muted">${u.totpEnabled ? `${me.recoveryLeft} von 10 Wiederherstellungscodes übrig.` : 'Schützt dein Konto mit einem Code aus einer Authenticator-App.'}</div></div>
            ${u.totpEnabled ? `<button type="button" class="btn sm" data-act="my-recovery">Neue Codes</button>${me.enforce2fa ? '' : '<button type="button" class="btn sm danger" data-act="my-2fa-off">Deaktivieren</button>'}` : '<button type="button" class="btn-light sm" data-act="my-2fa">Einrichten</button>'}</div>
        </div>
      </div>
    </div>
  </div>`;

  const current = { ...s };
  $$('[data-sw-setting]').forEach((b) => b.addEventListener('click', () => {
    current[b.dataset.swSetting] = !current[b.dataset.swSetting];
    b.classList.toggle('on', current[b.dataset.swSetting]);
  }));
  $$('form[data-settings]').forEach((form) => form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const all = {};
    $$('form[data-settings] [name]').forEach((i) => { all[i.name] = i.value; });
    const body = {
      ...current,
      cookieDomain: all.cookieDomain, loginHost: all.loginHost, adminHost: all.adminHost,
      sessionHours: Number(all.sessionHours), rememberDays: Number(all.rememberDays),
      lockAttempts: Number(all.lockAttempts), lockWindowSec: Number(all.lockWindowMin) * 60, lockDurationSec: Number(all.lockDurationMin) * 60,
      logRetentionDays: Number(all.logRetentionDays),
    };
    try {
      const r = await api('PUT', '/api/settings', body);
      if (r.relogin) { location.href = '/login'; return; }
      toast(r.caddyError ? 'Gespeichert – Caddy: ' + r.caddyError : 'Gespeichert', r.caddyError ? 'err' : '');
    } catch (ex) { toast(ex.message, 'err'); }
  }));
  const secs = $$('[data-sec]');
  const mark = (id) => secs.forEach((a) => a.classList.toggle('on', a.dataset.sec === id));
  mark(sub || 'allgemein');
  if (sub) { const el = document.getElementById(sub); if (el) el.scrollIntoView({ behavior: 'smooth' }); }
}

async function twoFactorDialog() {
  const r = await api('POST', '/api/me/2fa');
  dialog({
    title: 'Zwei-Faktor einrichten',
    lead: 'Scanne den Code mit einer Authenticator-App, z. B. Aegis, 2FAS oder Google Authenticator.',
    submit: 'Aktivieren',
    width: 640,
    body: `<div class="totp">
      <div class="qr"><img src="${esc(r.qr)}" alt="QR-Code" width="180" height="180"></div>
      <div class="totp-side">
        <div class="fg"><div class="lbl">Oder Schlüssel manuell eingeben</div><div class="secret"><span>${esc(r.secretGrouped)}</span><button type="button" class="mini" data-copy-text="${esc(r.secret)}">Kopieren</button></div></div>
        <div class="fg"><div class="lbl">Code aus der App</div><input class="input mono" name="code" inputmode="numeric" autocomplete="one-time-code" maxlength="7" placeholder="123 456" style="font-size:18px;letter-spacing:4px"></div>
      </div></div>`,
    async onSubmit(form) {
      const res = await api('POST', '/api/me/2fa/confirm', { code: $('[name=code]', form).value });
      codesDialog(res.codes, state.me.user.username);
      refresh();
      return true;
    },
  });
}

function passwordConfirm(title, lead, button, fn, danger = false) {
  dialog({
    title, lead, submit: button, submitClass: danger ? 'btn-danger' : 'btn-light', width: 460,
    body: '<div class="fg"><div class="lbl">Dein Passwort</div><input class="input" type="password" name="password" autocomplete="current-password"></div>',
    onSubmit: (form) => fn($('[name=password]', form).value),
  });
}

async function accountAction(act) {
  try {
    if (act === 'my-password') passwordDialog({ id: state.me.user.id });
    else if (act === 'my-2fa') await twoFactorDialog();
    else if (act === 'my-recovery') passwordConfirm('Neue Wiederherstellungscodes', 'Die bisherigen Codes werden ungültig.', 'Erzeugen', async (password) => {
      const r = await api('POST', '/api/me/recovery', { password });
      codesDialog(r.codes, state.me.user.username);
      refresh();
      return true;
    });
    else if (act === 'my-2fa-off') passwordConfirm('Zwei-Faktor deaktivieren', 'Dein Konto ist danach nur noch mit dem Passwort geschützt.', 'Deaktivieren', async (password) => {
      await api('POST', '/api/me/2fa/disable', { password });
      toast('Zwei-Faktor deaktiviert');
      refresh();
    }, true);
  } catch (ex) { toast(ex.message, 'err'); }
}

// ---------------------------------------------------------------- global events

document.addEventListener('click', async (e) => {
  if (e.target.closest('#modal')) return;
  const menu = $('#userMenu');
  if (e.target.closest('#avatarBtn')) { menu.hidden = !menu.hidden; return; }
  if (!e.target.closest('#userMenu')) menu.hidden = true;

  const tgl = e.target.closest('[data-toggle-site]');
  if (tgl) { e.stopPropagation(); toggleSite(Number(tgl.dataset.toggleSite)); return; }
  const editSite = e.target.closest('[data-edit-site]');
  if (editSite) {
    const s = (state.sitesData || []).find((x) => x.id === Number(editSite.dataset.editSite));
    if (s) siteDialog(s).catch((ex) => toast(ex.message, 'err'));
    return;
  }
  const filt = e.target.closest('[data-sites-filter]');
  if (filt) { state.sitesFilter = filt.dataset.sitesFilter; drawSites(); return; }
  const row = e.target.closest('[data-user]');
  if (row) {
    state.userSel = Number(row.dataset.user);
    $$('[data-user]').forEach((r) => r.classList.toggle('sel', r === row));
    drawUserDetail().catch((ex) => toast(ex.message, 'err'));
    return;
  }
  const endS = e.target.closest('[data-end-session]');
  if (endS) {
    try { await api('DELETE', `/api/sessions/${endS.dataset.endSession}`); toast('Sitzung beendet'); drawUserDetail(); } catch (ex) { toast(ex.message, 'err'); }
    return;
  }
  const lk = e.target.closest('[data-log-kind]');
  if (lk) { state.log.kind = lk.dataset.logKind; state.log.page = 1; refresh(); return; }
  const lp = e.target.closest('[data-log-page]');
  if (lp) { state.log.page = Math.max(1, state.log.page + Number(lp.dataset.logPage)); refresh(); return; }
  const sec = e.target.closest('[data-sec]');
  if (sec && location.hash === sec.getAttribute('href')) { e.preventDefault(); document.getElementById(sec.dataset.sec)?.scrollIntoView({ behavior: 'smooth' }); return; }

  const act = e.target.closest('[data-act]')?.dataset.act;
  if (!act) return;
  if (act === 'new-site') siteDialog(null).catch((ex) => toast(ex.message, 'err'));
  else if (act === 'new-user') userDialog(null);
  else if (act.startsWith('user-') || act === 'edit-user') userAction(act);
  else if (act.startsWith('my-')) accountAction(act);
});

(async function init() {
  try {
    state.me = await api('GET', '/api/me');
  } catch (e) {
    view.innerHTML = `<div class="page"><div class="alert">${esc(e.message)}</div></div>`;
    return;
  }
  const u = state.me.user;
  $('#domainLabel').textContent = state.me.domain || '';
  $('#avatarBtn').textContent = u.username[0].toUpperCase();
  $('#menuHead').innerHTML = `<b>${esc(u.username)}</b><span>${u.role === 'admin' ? 'Admin' : 'Benutzer'}</span>`;
  route();
})();
