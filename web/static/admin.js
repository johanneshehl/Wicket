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

// ---------------------------------------------------------------- i18n

let LANG = 'en';
const LOCALES = { en: 'en-GB', de: 'de-DE', es: 'es-ES' };

// t('key', ...args): {0}, {1} are replaced; plural entries [one, other] use the first argument as count.
function t(key, ...args) {
  const dict = window.WICKET_I18N || {};
  let s = (dict[LANG] || {})[key] ?? (dict.en || {})[key] ?? key;
  if (Array.isArray(s)) s = s[Number(args[0]) === 1 ? 0 : 1];
  return String(s).replace(/\{(\d)\}/g, (_, i) => (args[i] ?? ''));
}
const code = (s) => `<code>${esc(s)}</code>`;

function applyStaticTexts() {
  document.documentElement.lang = LANG;
  $$('[data-i18n]').forEach((el) => { el.textContent = t(el.dataset.i18n); });
  $$('[data-i18n-label]').forEach((el) => el.setAttribute('aria-label', t(el.dataset.i18nLabel)));
}

// ---------------------------------------------------------------- api & helpers

async function api(method, url, body) {
  const res = await fetch(url, {
    method, credentials: 'same-origin',
    headers: { 'X-Wicket': '1', ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (res.status === 401) { location.href = '/login'; throw new Error(t('err.notSignedIn')); }
  if (res.status === 403 && data.error === '2fa_setup_required') { location.href = '/setup-2fa'; throw new Error(t('err.2faRequired')); }
  if (!res.ok) throw new Error(data.error || t('err.http', res.status));
  return data;
}

function toast(msg, kind = '') {
  const el = $('#toast');
  el.textContent = msg;
  el.className = 'show ' + kind;
  clearTimeout(toast.t);
  toast.t = setTimeout(() => { el.className = ''; }, 3200);
}

function ago(ts) {
  if (!ts) return t('time.never');
  const d = Date.now() / 1000 - ts;
  if (d < 45) return t('time.justNow');
  const rtf = new Intl.RelativeTimeFormat(LANG, { numeric: 'auto' });
  if (d < 3600) return rtf.format(-Math.round(d / 60), 'minute');
  if (d < 86400) return rtf.format(-Math.round(d / 3600), 'hour');
  if (d < 86400 * 30) return rtf.format(-Math.round(d / 86400), 'day');
  return new Date(ts * 1000).toLocaleDateString(LOCALES[LANG]);
}

function stamp(ts) {
  const d = new Date(ts * 1000);
  return d.toLocaleDateString(LOCALES[LANG], { day: '2-digit', month: '2-digit' }) + ' ' + d.toLocaleTimeString(LOCALES[LANG]);
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

// kind -> [icon, css class]; texts come from 'ev.<kind>' = [short (overview), label (log)]
const EV = {
  login_ok: ['ok', ''], login_fail_password: ['fail', 'err-t'], login_fail_user: ['fail', 'err-t'], mfa_fail: ['fail', 'err-t'],
  denied: ['deny', ''], locked: ['lock', 'warn-t'], logout: ['out', ''], recovery_used: ['key', 'warn-t'], mfa_enabled: ['ok', ''],
};
function ev(kind) {
  const [icon, cls] = EV[kind] || ['edit', ''];
  const dict = (window.WICKET_I18N[LANG] || {})['ev.' + kind] || window.WICKET_I18N.en['ev.' + kind] || [kind, kind];
  return [icon, dict[0], dict[1], cls];
}

// details are stored as neutral codes ("2fa", "lock:5:15") or plain values (user names)
function detailText(d) {
  if (!d) return '';
  const m = /^lock:(\d+):(\d+)$/.exec(d);
  if (m) return t('detail.lock', m[1], m[2]);
  const key = 'detail.' + d;
  const s = t(key);
  return s === key ? d : s;
}

const accessLabel = (s) => s.access === 'all' ? t('access.all') : s.access === 'admins' ? t('access.admins') : t('n.users', s.users.length);
const initial = (d) => (d.replace(/^\*\./, '')[0] || '?').toUpperCase();

// ---------------------------------------------------------------- dialogs

function closeDialog() { $('#modal').innerHTML = ''; }

function dialog({ title, lead = '', body, submit, submitClass = 'btn-light', width = 560, footLeft = '', onSubmit, onMount }) {
  $('#modal').innerHTML = `<div class="backdrop"><form class="dialog" style="max-width:${width}px" novalidate>
    <div class="dlg-head"><h2>${title}</h2>${lead ? `<p class="muted">${lead}</p>` : ''}</div>
    <div class="dlg-body">${body}<div class="alert" data-err hidden></div></div>
    <div class="dlg-foot">${footLeft}<button type="button" class="btn right" data-close>${submit ? t('common.cancel') : t('common.close')}</button>${submit ? `<button type="submit" class="${submitClass}">${submit}</button>` : ''}</div>
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
  const file = 'data:text/plain;charset=utf-8,' + encodeURIComponent(`${t('codes.fileTitle', username)}\n\n${text}\n`);
  dialog({
    title: t('codes.title'),
    lead: t('codes.lead'),
    width: 600,
    body: `<div class="codes">${codes.map((c) => `<div>${esc(c)}</div>`).join('')}</div>
      <div class="row gap8"><button type="button" class="btn sm" data-copy-text="${esc(text)}">${t('common.copyAll')}</button><a class="btn sm" download="wicket-recovery-codes.txt" href="${esc(file)}">${t('common.download')}</a></div>`,
  });
}

$('#modal').addEventListener('click', async (e) => {
  if (e.target.classList.contains('backdrop') || e.target.closest('[data-close]')) { closeDialog(); return; }
  const c = e.target.closest('[data-copy-text]');
  if (c) { await navigator.clipboard.writeText(c.dataset.copyText); toast(t('toast.copied')); }
});
document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && $('#modal').innerHTML) closeDialog(); });

// ---------------------------------------------------------------- router

const routes = { '': renderOverview, sites: renderSites, users: renderUsers, templates: renderTemplates, log: renderLog, settings: renderSettings };
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
  const meta = s.managed ? '→ ' + esc(s.target) : t('site.ownBlock');
  const extra = s.bypass.length ? ' · ' + t('n.bypass', s.bypass.length) : '';
  return `<div class="row-item clickable" data-edit-site="${s.id}">
    <span class="dot ${s.enabled ? 'on' : ''}"></span>
    <div class="grow"><div class="strong ${s.enabled ? '' : 'muted'}">${esc(s.domain)}</div><div class="mono dim small">${meta}${extra}${s.enabled ? '' : ' · ' + t('site.paused')}</div></div>
    <span class="muted small">${accessLabel(s)}</span>
    <span>${s.require2fa ? `<span class="tag">${t('site.2faRequired')}</span>` : `<span class="dim small">${t('site.optional')}</span>`}</span>
  </div>`;
}

function eventMini(e) {
  const [icon, short] = ev(e.kind);
  const who = e.kind === 'locked' ? e.ip : (e.username || e.ip);
  const line2 = e.kind === 'locked' ? detailText(e.detail) : [e.ip, device(e.ua), e.site].filter(Boolean).join(' · ');
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
      <div><h1>${t('ov.title')}</h1><p class="muted">${t('ov.sub', t('n.sites', st.sites))}</p></div>
      <div class="actions"><button class="btn" data-act="new-user">${t('ov.newUser')}</button><button class="btn-light" data-act="new-site">${I.plus}${t('ov.addDomain')}</button></div>
    </div>
    <div class="stats">
      ${stat(t('ov.statSites'), st.sites)}
      ${stat(t('ov.statSessions'), st.sessions)}
      ${stat(t('ov.statFails'), st.fails24h, diff > 0 ? `<span class="small warn-t">${t('ov.sinceYesterday', diff)}</span>` : '')}
      ${stat(t('ov.statLocked'), st.locked)}
    </div>
    <div class="two-col">
      <section class="panel">
        <div class="panel-head"><h3>${t('ov.sites')}</h3><a class="muted small" href="#/sites">${t('ov.showAll')}</a></div>
        ${d.sites.length ? d.sites.slice(0, 6).map(siteMini).join('') : `<div class="empty">${t('ov.noSites')}<button class="btn-light sm" data-act="new-site">${t('ov.addDomain')}</button></div>`}
      </section>
      <section class="panel">
        <div class="panel-head"><h3>${t('ov.recent')}</h3><span class="live"><i></i>${t('ov.live')}</span></div>
        ${d.events.length ? d.events.map(eventMini).join('') : `<div class="empty">${t('ov.noEvents')}</div>`}
      </section>
    </div>
  </div>`;
}

// ---------------------------------------------------------------- sites

function caddyNotice(c) {
  if (!c) return '';
  if (!c.enabled) return `<div class="info" style="margin-bottom:16px">${t('caddy.off', code(c.dir), code('import wicket'))}</div>`;
  if (!c.imported) return `<div class="info warn" style="margin-bottom:16px">${t('caddy.notImported', code(c.importLine))}</div>`;
  if (!c.reachable) return `<div class="info warn" style="margin-bottom:16px">${t('caddy.unreachable', code(c.admin))}</div>`;
  return '';
}

function siteRow(s) {
  return `<div class="tr sites-grid clickable ${s.enabled ? '' : 'paused'}" data-edit-site="${s.id}">
    <div class="cell-domain"><span class="fav-sq ${s.enabled ? '' : 'dim'}">${esc(initial(s.domain))}</span><div>
      <div class="strong">${esc(s.domain)}</div>${s.enabled ? '' : `<div class="small warn-t">${t('site.pausedOpen')}</div>`}</div></div>
    <div class="mono small ${s.enabled ? 'muted' : 'dim'}">${s.managed ? esc(s.target) : t('site.ownBlock')}</div>
    <div class="${s.enabled ? 'muted' : 'dim'}">${accessLabel(s)}</div>
    <div>${s.require2fa ? `<span class="tag">${t('site.required')}</span>` : `<span class="dim small">${t('site.optional')}</span>`}</div>
    <div class="chips-sm">${s.bypass.length ? s.bypass.map((b) => `<span class="code-chip">${esc(b)}</span>`).join('') : '<span class="dim">–</span>'}</div>
    <div class="tr-end"><button type="button" class="switch ${s.enabled ? 'on' : ''}" data-toggle-site="${s.id}" role="switch" aria-checked="${s.enabled}" aria-label="${t('sites.toggle')}"></button></div>
  </div>`;
}

function drawSites() {
  const all = state.sitesData || [];
  const q = state.sitesQuery.toLowerCase();
  const list = all.filter((s) => (state.sitesFilter === 'all' || (state.sitesFilter === 'on') === s.enabled) && (!q || s.domain.includes(q)));
  const counts = { all: all.length, on: all.filter((s) => s.enabled).length, off: all.filter((s) => !s.enabled).length };
  $('#siteSeg').innerHTML = [['all', t('sites.filterAll')], ['on', t('sites.filterOn')], ['off', t('sites.filterOff')]]
    .map(([k, l]) => `<button type="button" class="${state.sitesFilter === k ? 'on' : ''}" data-sites-filter="${k}">${l} ${counts[k]}</button>`).join('');
  $('#siteRows').innerHTML = list.length ? list.map(siteRow).join('')
    : `<div class="empty">${all.length ? t('sites.noMatch') : `${t('ov.noSites')}<button class="btn-light sm" data-act="new-site">${t('ov.addDomain')}</button>`}</div>`;
}

async function renderSites() {
  const d = await api('GET', '/api/sites');
  state.sitesData = d.sites;
  state.caddy = d.caddy;
  view.innerHTML = `<div class="page">
    <div class="page-head">
      <div><h1>${t('sites.title')}</h1><p class="muted">${t('sites.sub')}${d.caddy.enabled && d.caddy.imported ? ' ' + t('sites.subAuto') : ''}</p></div>
      <div class="actions"><button class="btn-light" data-act="new-site">${I.plus}${t('ov.addDomain')}</button></div>
    </div>
    ${caddyNotice(d.caddy)}
    <div class="toolbar">
      <label class="search">${I.search}<input id="siteSearch" placeholder="${t('sites.search')}" value="${esc(state.sitesQuery)}" autocomplete="off"></label>
      <div class="seg" id="siteSeg"></div>
    </div>
    <div class="table">
      <div class="tr th sites-grid"><div>${t('col.domain')}</div><div>${t('col.target')}</div><div>${t('col.access')}</div><div>${t('col.2fa')}</div><div>${t('col.bypass')}</div><div class="tr-end">${t('col.protection')}</div></div>
      <div id="siteRows"></div>
    </div>
  </div>`;
  $('#siteSearch').addEventListener('input', (e) => { state.sitesQuery = e.target.value.trim(); drawSites(); });
  drawSites();
}

function sitePreview(d) {
  const dom = esc(d.domain || 'app.example.com');
  if (d.managed) return `<b>${dom}</b> {\n  <i>import</i> wicket\n  <i>reverse_proxy</i> ${esc(d.target || '127.0.0.1:8080')}\n}`;
  const note = state.caddy && state.caddy.caddyfileWritable ? t('preview.auto') : t('preview.manual');
  return `${esc(note)}\n<b>${dom}</b> {\n  <i>import</i> wicket\n  …\n}`;
}

async function siteDialog(site) {
  const edit = Boolean(site);
  if (!state.caddy) state.caddy = (await api('GET', '/api/sites')).caddy;
  const users = (await api('GET', '/api/users')).users.filter((u) => u.role !== 'admin');
  const d = site ? JSON.parse(JSON.stringify(site))
    : { domain: '', target: '', access: 'all', require2fa: false, bypass: [], enabled: true, managed: state.caddy.enabled, users: [] };

  const form = dialog({
    title: edit ? t('dlg.editSite') : t('dlg.addDomain'),
    lead: d.managed || !edit ? t('dlg.leadManaged') : t('dlg.leadOwn'),
    submit: edit ? t('common.save') : t('dlg.protect'),
    width: 580,
    footLeft: edit ? `<button type="button" class="btn danger" data-delete-site>${t('common.delete')}</button>` : '',
    body: `
      <div class="fg"><div class="lbl">${t('f.domain')}</div>
        <input class="input" name="domain" value="${esc(d.domain)}" placeholder="app.example.com" autocomplete="off" spellcheck="false">
        <div class="dns" data-dns></div></div>
      <div class="info" data-caddy-info hidden></div>
      <div class="fg" data-target><div class="lbl">${t('f.target')}</div>
        <input class="input mono" name="target" value="${esc(d.target)}" placeholder="127.0.0.1:8080" autocomplete="off" spellcheck="false">
        <div class="dim small">${t('f.targetHint')}</div></div>
      <div class="grid2">
        <div class="fg"><div class="lbl">${t('f.access')}</div>
          <select class="input" name="access">
            <option value="all" ${d.access === 'all' ? 'selected' : ''}>${t('access.all')}</option>
            <option value="admins" ${d.access === 'admins' ? 'selected' : ''}>${t('access.admins')}</option>
            <option value="users" ${d.access === 'users' ? 'selected' : ''}>${t('f.usersWithAccess')}</option>
          </select></div>
        <div class="fg"><div class="lbl">${t('f.2fa')}</div>
          <div class="input toggle-field">${t('f.required')}<button type="button" class="switch ${d.require2fa ? 'on' : ''}" data-sw="require2fa" role="switch" aria-checked="${d.require2fa}"></button></div></div>
      </div>
      <div class="fg" data-userlist ${d.access === 'users' ? '' : 'hidden'}><div class="lbl">${t('f.usersWithAccess')} <span>${t('f.adminsAlways')}</span></div>
        <div class="checklist">${users.length ? users.map((u) => `<label class="check"><input type="checkbox" value="${u.id}" ${d.users.includes(u.id) ? 'checked' : ''}><span class="box"></span>${esc(u.username)}</label>`).join('') : `<span class="dim small">${t('f.noUsers')}</span>`}</div></div>
      <div class="fg"><div class="lbl">${t('f.bypass')} <span>${t('f.bypassSub')}</span></div>
        <div class="chip-input" data-chips><input placeholder="${t('f.bypassPh')}" spellcheck="false"></div>
        <div class="dim small">${t('f.bypassHint')}</div></div>
      <div class="fg"><div class="lbl">${t('f.active')}</div>
        <div class="input toggle-field">${t('f.protected')}<button type="button" class="switch ${d.enabled ? 'on' : ''}" data-sw="enabled" role="switch" aria-checked="${d.enabled}"></button></div></div>
      ${state.caddy.enabled ? `<label class="check" data-managed-row><input type="checkbox" name="managed" ${d.managed ? 'checked' : ''}><span class="box"></span>${t('f.managed')}</label>` : ''}
      <div class="code-box"><div class="code-head"><span data-code-title></span><button type="button" class="link right" data-copy-code>${t('common.copy')}</button></div><pre data-preview></pre></div>`,
    async onSubmit() {
      d.users = $$('[data-userlist] input:checked', form).map((i) => Number(i.value));
      const chipInput = $('[data-chips] input', form);
      if (chipInput.value.trim()) { d.bypass.push(chipInput.value.trim()); chipInput.value = ''; }
      const saved = edit ? await api('PUT', `/api/sites/${site.id}`, d) : await api('POST', '/api/sites', d);
      toast(edit ? t('common.saved') : t('toast.protected', saved.domain));
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
    info.innerHTML = auto ? t('info.existingAuto', code('import wicket')) : t('info.existingManual', code('import wicket'));
    $('[data-preview]', form).innerHTML = sitePreview(d);
    $('[data-code-title]', form).textContent = d.managed ? t('code.managed') : (auto ? t('code.auto') : t('code.manual'));
  };
  const drawChips = () => {
    const box = $('[data-chips]', form);
    $$('.code-chip', box).forEach((c) => c.remove());
    d.bypass.forEach((b, i) => box.insertBefore(Object.assign(document.createElement('span'), {
      className: 'code-chip', innerHTML: `${esc(b)}<button type="button" data-rm="${i}" aria-label="${t('common.remove')}">×</button>`,
    }), $('input', box)));
  };
  let dnsTimer;
  const checkDNS = () => {
    clearTimeout(dnsTimer);
    const el = $('[data-dns]', form);
    const dom = d.domain.trim();
    if (!dom) { el.innerHTML = ''; return; }
    dnsTimer = setTimeout(async () => {
      el.innerHTML = `<span class="dim">${t('dns.checking')}</span>`;
      try {
        const r = await api('GET', '/api/dns?domain=' + encodeURIComponent(dom));
        if (d.domain.trim() !== dom) return;
        d.caddyBlock = Boolean(r.caddyBlock);
        if (d.caddyBlock) d.managed = false;
        else if (!edit) d.managed = state.caddy.enabled;
        update();
        el.innerHTML = r.ok ? `${I.ok}<span class="ok-t">${t('dns.ok')}</span>`
          : r.reason === 'missing' ? `<span class="warn-t">${t('dns.missing')}</span>`
          : r.reason === 'other' ? `<span class="warn-t">${t('dns.other', esc(r.ips.join(', ')))}</span>` : '';
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
    if (e.target.closest('[data-copy-code]')) { await navigator.clipboard.writeText($('[data-preview]', form).textContent); toast(t('toast.copied')); }
    if (e.target.closest('[data-delete-site]')) {
      closeDialog();
      const text = t('confirm.deleteSite', esc(site.domain)) + (site.managed ? ' ' + t('confirm.deleteSiteManaged') : '');
      if (await confirmDialog(t('dlg.deleteSite'), text, t('common.delete'), true)) {
        try { const r = await api('DELETE', `/api/sites/${site.id}`); toast(r.warning || t('toast.siteDeleted'), r.warning ? 'err' : ''); refresh(); } catch (ex) { toast(ex.message, 'err'); }
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
    toast(s.enabled ? t('toast.paused', s.domain) : t('toast.resumed', s.domain));
    refresh();
  } catch (ex) { toast(ex.message, 'err'); }
}

// ---------------------------------------------------------------- users

function userRow(u) {
  return `<div class="tr users-grid clickable ${u.id === state.userSel ? 'sel' : ''}" data-user="${u.id}">
    <div class="cell-user"><span class="uavatar ${u.id === state.meId ? 'me' : ''}">${esc(u.username[0].toUpperCase())}</span>
      <div class="grow"><div class="strong ${u.disabled ? 'muted' : ''}">${esc(u.username)}${u.disabled ? ` <span class="tag dim">${t('u.disabled')}</span>` : ''}</div><div class="small dim">${esc(u.email || (u.id === state.meId ? t('u.thatsYou') : '–'))}</div></div></div>
    <div>${u.role === 'admin' ? `<span class="tag">${t('role.admin')}</span>` : `<span class="muted small">${t('role.user')}</span>`}</div>
    <div>${u.totpEnabled ? `<span class="status ok-t"><i></i>${t('u.active')}</span>` : `<span class="status missing warn-t"><i></i>${t('u.missing')}</span>`}</div>
    <div class="muted small">${u.id === state.meId ? t('time.justNow') : ago(u.lastSeen)}</div>
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
      <div><h1>${t('users.title')}</h1><p class="muted">${t('users.sub', t('n.users', ud.users.length), with2fa)}</p></div>
      <div class="actions"><button class="btn-light" data-act="new-user">${I.plus}${t('users.create')}</button></div>
    </div>
    <div class="users-layout">
      <div class="table">
        <div class="tr th users-grid"><div>${t('col.user')}</div><div>${t('col.role')}</div><div>${t('col.2fa')}</div><div>${t('col.lastActive')}</div></div>
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
  const since = new Date(u.createdAt * 1000).toLocaleDateString(LOCALES[LANG]);
  box.innerHTML = `
    <div class="row gap8" style="gap:12px"><span class="uavatar lg ${self ? 'me' : ''}">${esc(u.username[0].toUpperCase())}</span>
      <div class="grow"><div class="strong" style="font-size:16px">${esc(u.username)}</div><div class="small muted">${t('detail.since', u.role === 'admin' ? t('role.admin') : t('role.user'), since)}</div></div>
      <button class="btn sm" data-act="edit-user">${t('common.edit')}</button></div>
    <div>
      <div class="kv"><span>${t('detail.access')}</span><span>${u.role === 'admin' ? t('detail.allSites', t('n.sites', sites.length)) : t('n.sites', reach)}</span></div>
      <div class="kv"><span>${t('detail.2faLabel')}</span><span class="${u.totpEnabled ? 'ok-t' : 'warn-t'}">${u.totpEnabled ? t('detail.app') : t('detail.notSet')}</span></div>
      ${u.totpEnabled ? `<div class="kv"><span>${t('detail.recovery')}</span><span>${t('detail.left', u.recoveryLeft)}</span></div>` : ''}
    </div>
    <div>
      <div class="strong small" style="margin-bottom:12px">${t('detail.sessions')}</div>
      ${sessions.length ? sessions.map((s) => `<div class="sess">${/iPhone|Android|iPad/.test(s.ua) ? I.phone : I.laptop}
        <div class="grow"><div class="small">${esc(device(s.ua) || t('detail.unknownDevice'))}${s.current ? ` <span class="ok-t">· ${t('detail.thisDevice')}</span>` : ''}</div><div class="mono dim" style="font-size:11px">${esc(s.ip)} · ${ago(s.lastSeen)}</div></div>
        ${s.current ? '' : `<button class="link" data-end-session="${s.id}">${t('detail.end')}</button>`}</div>`).join('') : `<div class="dim small">${t('detail.noSessions')}</div>`}
    </div>
    <div class="detail-actions">
      <button class="btn sm" data-act="user-password">${self ? t('detail.changePw') : t('detail.setPw')}</button>
      ${u.totpEnabled && !self ? `<button class="btn sm" data-act="user-reset-2fa">${t('detail.reset2fa')}</button>` : ''}
      <button class="btn sm danger" data-act="user-logout">${t('detail.logoutAll')}</button>
      ${self ? '' : `<button class="btn sm danger" data-act="user-delete">${t('common.delete')}</button>`}
    </div>`;
}

function userDialog(u) {
  const edit = Boolean(u);
  dialog({
    title: edit ? t('userDlg.edit', esc(u.username)) : t('userDlg.create'),
    submit: edit ? t('common.save') : t('userDlg.createBtn'),
    width: 520,
    body: `
      ${edit ? '' : `<div class="fg"><div class="lbl">${t('f.username')}</div><input class="input" name="username" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="${t('f.usernamePh')}"></div>`}
      <div class="fg"><div class="lbl">${t('f.email')} <span>${t('common.optional')}</span></div><input class="input" name="email" type="email" value="${esc(u?.email || '')}" autocomplete="off"></div>
      <div class="grid2">
        <div class="fg"><div class="lbl">${t('f.role')}</div><select class="input" name="role">
          <option value="user" ${u?.role === 'user' ? 'selected' : ''}>${t('role.user')}</option>
          <option value="admin" ${u?.role === 'admin' ? 'selected' : ''}>${t('role.admin')}</option></select></div>
        ${edit ? `<div class="fg"><div class="lbl">${t('f.status')}</div><select class="input" name="disabled"><option value="0">${t('f.statusActive')}</option><option value="1" ${u.disabled ? 'selected' : ''}>${t('f.statusDisabled')}</option></select></div>` : ''}
      </div>
      ${edit ? '' : `<div class="fg"><div class="lbl">${t('f.password')} <span>${t('f.min10')}</span></div>
        <div class="row gap8"><input class="input mono" name="password" autocomplete="new-password"><button type="button" class="btn" data-gen>${t('common.generate')}</button></div>
        <div class="dim small">${t('userDlg.pwHint')}</div></div>`}
      <div class="dim small">${t('userDlg.roleHint')}</div>`,
    onMount(form) {
      const g = $('[data-gen]', form);
      if (g) g.addEventListener('click', () => { $('[name=password]', form).value = genPassword(); });
    },
    async onSubmit(form) {
      const val = (n) => $(`[name=${n}]`, form)?.value ?? '';
      if (edit) {
        await api('PUT', `/api/users/${u.id}`, { email: val('email'), role: val('role'), disabled: val('disabled') === '1' });
        toast(t('common.saved'));
      } else {
        const created = await api('POST', '/api/users', { username: val('username').trim(), email: val('email'), role: val('role'), password: val('password') });
        state.userSel = created.id;
        toast(t('toast.created', created.username));
        if (!location.hash.startsWith('#/users')) { location.hash = '#/users'; return; }
      }
      refresh();
    },
  });
}

function passwordDialog(u) {
  const self = u.id === state.meId || u.id === state.me?.user?.id;
  dialog({
    title: self ? t('pwDlg.change') : t('pwDlg.set', esc(u.username)),
    lead: self ? '' : t('pwDlg.lead'),
    submit: t('common.save'),
    width: 480,
    body: `${self ? `<div class="fg"><div class="lbl">${t('pwDlg.current')}</div><input class="input" type="password" name="current" autocomplete="current-password"></div>` : ''}
      <div class="fg"><div class="lbl">${t('pwDlg.new')} <span>${t('f.min10')}</span></div>
      <div class="row gap8"><input class="input mono" name="password" autocomplete="new-password"><button type="button" class="btn" data-gen>${t('common.generate')}</button></div></div>`,
    onMount(form) { $('[data-gen]', form).addEventListener('click', () => { $('[name=password]', form).value = genPassword(); }); },
    async onSubmit(form) {
      const pw = $('[name=password]', form).value;
      if (self) await api('PUT', '/api/me/password', { current: $('[name=current]', form).value, new: pw });
      else await api('PUT', `/api/users/${u.id}/password`, { password: pw });
      toast(t('toast.pwSaved'));
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
      if (!(await confirmDialog(t('confirm.reset2fa'), t('confirm.reset2faText', esc(u.username)), t('confirm.reset2faBtn')))) return;
      await api('POST', `/api/users/${u.id}/reset-2fa`); toast(t('toast.2faReset')); refresh();
    } else if (act === 'user-logout') {
      if (!(await confirmDialog(t('confirm.logout'), self ? t('confirm.logoutSelf') : t('confirm.logoutOther', esc(u.username)), t('confirm.logoutBtn'), true))) return;
      const r = await api('POST', `/api/users/${u.id}/logout`);
      if (r.self) { location.href = '/login'; return; }
      toast(t('toast.sessionsEnded')); refresh();
    } else if (act === 'user-delete') {
      if (!(await confirmDialog(t('confirm.deleteUser'), t('confirm.deleteUserText', esc(u.username)), t('common.delete'), true))) return;
      await api('DELETE', `/api/users/${u.id}`); state.userSel = null; toast(t('toast.userDeleted')); refresh();
    }
  } catch (ex) { toast(ex.message, 'err'); }
}

// ---------------------------------------------------------------- templates

const TEMPLATES = ['centered', 'split', 'light', 'dock', 'terminal', 'glass'];

// miniature previews of the login templates
const box = (s) => `<div style="${s}"></div>`;
const THUMBS = {
  centered: () => `<div style="height:100%;background-color:#000;background-image:radial-gradient(#1c1c1c 1px,transparent 1px);background-size:12px 12px;display:flex;align-items:center;justify-content:center">
    <div style="width:120px;display:flex;flex-direction:column;align-items:center;gap:6px">${box('width:16px;height:16px;border:1.5px solid #ededed;border-radius:4px')}${box('width:50px;height:6px;border-radius:3px;background:#ededed;margin-top:4px')}${box('width:80px;height:10px;border-radius:999px;border:1px solid #262626')}${box('width:120px;height:14px;border-radius:3px;border:1px solid #333;margin-top:6px')}${box('width:120px;height:14px;border-radius:3px;border:1px solid #333')}${box('width:120px;height:14px;border-radius:3px;background:#ededed')}</div></div>`,
  split: () => `<div style="height:100%;display:grid;grid-template-columns:repeat(2,minmax(0,1fr))">
    <div style="background-color:#000;background-image:linear-gradient(#111 1px,transparent 1px),linear-gradient(90deg,#111 1px,transparent 1px);background-size:20px 20px;border-right:1px solid #1f1f1f;padding:18px;display:flex;flex-direction:column;justify-content:flex-end;gap:6px">${box('width:18px;height:18px;border-radius:5px;border:1px solid #333')}${box('width:90px;height:10px;border-radius:3px;background:#ededed')}${box('width:70px;height:10px;border-radius:3px;background:#333')}</div>
    <div style="background:#0a0a0a;display:flex;flex-direction:column;justify-content:center;gap:6px;padding:0 22px">${box('width:60px;height:8px;border-radius:3px;background:#ededed')}${box('height:14px;border-radius:3px;border:1px solid #333;margin-top:6px')}${box('height:14px;border-radius:3px;border:1px solid #333')}${box('height:14px;border-radius:3px;background:#ededed')}</div></div>`,
  light: () => `<div style="height:100%;background-color:#fafafa;background-image:radial-gradient(#e4e4e4 1px,transparent 1px);background-size:12px 12px;display:flex;align-items:center;justify-content:center">
    <div style="width:120px;display:flex;flex-direction:column;align-items:center;gap:6px">${box('width:16px;height:16px;border:1.5px solid #000;border-radius:4px')}${box('width:50px;height:6px;border-radius:3px;background:#000;margin-top:4px')}${box('width:80px;height:10px;border-radius:999px;border:1px solid #e4e4e4;background:#fff')}${box('width:120px;height:14px;border-radius:3px;border:1px solid #eaeaea;background:#fff;margin-top:6px')}${box('width:120px;height:14px;border-radius:3px;border:1px solid #eaeaea;background:#fff')}${box('width:120px;height:14px;border-radius:3px;background:#000')}</div></div>`,
  dock: () => `<div style="height:100%;display:grid;grid-template-columns:110px minmax(0,1fr)">
    <div style="background:#0a0a0a;border-right:1px solid #1f1f1f;display:flex;flex-direction:column;justify-content:center;gap:6px;padding:0 14px">${box('width:44px;height:7px;border-radius:3px;background:#ededed')}${box('height:13px;border-radius:3px;border:1px solid #333;margin-top:6px')}${box('height:13px;border-radius:3px;border:1px solid #333')}${box('height:13px;border-radius:3px;background:#ededed')}</div>
    <div style="background-color:#000;background-image:radial-gradient(#1a1a1a 1px,transparent 1px);background-size:12px 12px;display:flex;flex-direction:column;justify-content:flex-end;padding:16px;gap:4px">${box('width:150px;height:18px;border-radius:3px;background:#ededed')}${box('width:180px;max-width:100%;height:18px;border-radius:3px;background:#262626')}</div></div>`,
  terminal: () => `<div style="height:100%;background:#000;display:flex;align-items:center;justify-content:center">
    <div style="width:190px;border:1px solid #262626;border-radius:6px;background:#0a0a0a;font-family:var(--mono);font-size:9px;color:#666">
      <div style="padding:5px 8px;border-bottom:1px solid #1f1f1f;display:flex;gap:3px">${box('width:5px;height:5px;border-radius:50%;background:#262626')}${box('width:5px;height:5px;border-radius:50%;background:#262626')}${box('width:5px;height:5px;border-radius:50%;background:#262626')}</div>
      <div style="padding:8px;display:flex;flex-direction:column;gap:5px"><div><span style="color:#50e3c2">$</span> sign in</div><div><span style="color:#50e3c2">→</span> <span style="color:#a1a1a1">app</span></div>${box('height:12px;border:1px solid #262626;border-radius:3px')}${box('height:12px;border:1px solid #ededed;border-radius:3px')}${box('width:60px;height:12px;border-radius:3px;background:#ededed')}</div></div></div>`,
  glass: () => `<div style="height:100%;position:relative;overflow:hidden;background:#000;display:flex;align-items:center;justify-content:center">
    ${box('position:absolute;width:220px;height:220px;left:20px;top:-90px;border-radius:50%;background:radial-gradient(circle,rgba(255,255,255,.16) 0%,rgba(255,255,255,0) 65%)')}
    ${box('position:absolute;width:240px;height:240px;right:0;bottom:-120px;border-radius:50%;background:radial-gradient(circle,rgba(80,227,194,.14) 0%,rgba(80,227,194,0) 65%)')}
    <div style="position:relative;width:130px;padding:12px;border:1px solid rgba(255,255,255,.1);border-radius:8px;background:rgba(20,20,20,.6);display:flex;flex-direction:column;gap:6px">${box('width:50px;height:7px;border-radius:3px;background:#ededed')}${box('height:13px;border-radius:4px;border:1px solid rgba(255,255,255,.12);margin-top:4px')}${box('height:13px;border-radius:4px;border:1px solid rgba(255,255,255,.12)')}${box('height:13px;border-radius:4px;background:#ededed')}</div></div>`,
};

async function renderTemplates() {
  const d = await api('GET', '/api/settings');
  const cur = d.settings.loginTemplate || 'centered';
  view.innerHTML = `<div class="page">
    <div class="page-head"><div><h1>${t('tpl.title')}</h1><p class="muted">${t('tpl.sub')}</p></div></div>
    <div class="tpl-grid">${TEMPLATES.map((id) => `<div class="tpl-card ${id === cur ? 'on' : ''}">
      <a class="tpl-thumb" href="/preview/${id}" target="_blank" rel="noopener" aria-label="${t('tpl.preview')}: ${t('tpl.' + id)}">${THUMBS[id]()}<span class="open">${t('tpl.preview')} ↗</span></a>
      <div class="tpl-meta"><div class="grow"><div class="strong">${t('tpl.' + id)}</div><div class="small muted">${t('tpl.' + id + '.desc')}</div></div>
        ${id === cur ? `<span class="badge ok"><i></i>${t('tpl.active')}</span>` : `<button class="btn sm" data-use-tpl="${id}">${t('tpl.use')}</button>`}</div>
    </div>`).join('')}</div>
    <div class="info" style="margin-top:20px">${t('tpl.note')}</div>
  </div>`;
}

async function useTemplate(id) {
  try {
    const d = await api('GET', '/api/settings');
    await api('PUT', '/api/settings', { ...d.settings, loginTemplate: id });
    toast(t('toast.tplSet', t('tpl.' + id)));
    refresh();
  } catch (ex) { toast(ex.message, 'err'); }
}

// ---------------------------------------------------------------- log

function eventRow(e) {
  const [icon, , label, cls] = ev(e.kind);
  const who = e.username || '–';
  const detail = detailText(e.detail);
  return `<div class="tr log-grid ${e.kind === 'locked' ? 'lock-row' : ''}">
    <div class="mono small muted">${stamp(e.at)}</div>
    <span class="ev-i">${I[icon]}</span>
    <div class="${cls}" title="${esc(detail)}">${esc(label)}${detail ? ` <span class="dim small">· ${esc(detail)}</span>` : ''}</div>
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
  const kinds = [['', t('log.kindAll')], ['ok', t('log.kindOk')], ['fail', t('log.kindFail')], ['lock', t('log.kindLock')], ['admin', t('log.kindAdmin')]];
  const ranges = [['24h', t('log.r24h')], ['7d', t('log.r7d')], ['30d', t('log.r30d')], ['90d', t('log.r90d')]];
  const from = (f.page - 1) * d.pageSize;
  view.innerHTML = `<div class="page">
    <div class="page-head">
      <div><h1>${t('log.title')}</h1><p class="muted">${t('log.sub')}</p></div>
      <div class="actions"><a class="btn" href="/api/events.csv?${qs}" download>${t('log.csv')}</a></div>
    </div>
    <div class="toolbar">
      <div class="seg">${kinds.map(([k, l]) => `<button type="button" class="${f.kind === k ? 'on' : ''}" data-log-kind="${k}">${l}</button>`).join('')}</div>
      <select class="select" data-log-range>${ranges.map(([k, l]) => `<option value="${k}" ${f.range === k ? 'selected' : ''}>${l}</option>`).join('')}</select>
      <select class="select" data-log-site><option value="">${t('log.allSites')}</option>${sites.map((s) => `<option value="${esc(s.domain)}" ${f.site === s.domain ? 'selected' : ''}>${esc(s.domain)}</option>`).join('')}</select>
      <span class="live right"><i></i>${t('ov.live')}</span>
    </div>
    <div class="table">
      <div class="tr th log-grid"><div>${t('col.time')}</div><div></div><div>${t('col.event')}</div><div>${t('col.site')}</div><div>${t('col.userDevice')}</div><div>${t('col.ip')}</div></div>
      ${d.items.length ? d.items.map(eventRow).join('') : `<div class="empty">${t('log.empty')}</div>`}
    </div>
    <div class="pager"><span>${d.items.length ? t('log.range', from + 1, from + d.items.length, d.total) : ''}</span>
      <div class="row gap8"><button class="btn sm" data-log-page="-1" ${f.page <= 1 ? 'disabled' : ''}>${t('common.back')}</button><button class="btn sm" data-log-page="1" ${f.page >= pages ? 'disabled' : ''}>${t('common.next')}</button></div></div>
  </div>`;
  $('[data-log-range]').addEventListener('change', (e) => { f.range = e.target.value; f.page = 1; refresh(); });
  $('[data-log-site]').addEventListener('change', (e) => { f.site = e.target.value; f.page = 1; refresh(); });
}

// ---------------------------------------------------------------- settings

const SECTIONS = ['general', 'sessions', 'security', 'login', 'caddy', 'account'];

async function renderSettings(sub) {
  const [d, me, oa] = await Promise.all([api('GET', '/api/settings'), api('GET', '/api/me'), api('GET', '/api/oauth')]);
  state.me = me;
  const s = d.settings;
  const c = d.caddy;
  const u = me.user;
  const caddyOk = c.enabled && c.writable && c.reachable && c.imported;
  const save = () => `<button type="submit" class="btn-light sm right">${t('common.save')}</button>`;
  const langs = ['auto', ...(d.languages || ['en', 'de', 'es'])];
  view.innerHTML = `<div class="page settings">
    <aside class="side-nav"><h1>${t('set.title')}</h1>${SECTIONS.map((id) => `<a href="#/settings/${id}" data-sec="${id}">${t('sec.' + id)}</a>`).join('')}</aside>
    <div class="cards">
      <form class="scard" id="general" data-settings>
        <div class="scard-body"><h2>${t('set.language')}</h2><p class="sub">${t('set.languageSub')}</p>
          <div class="fields"><label>${t('set.language')}<select class="input" name="language">${langs.map((l) => `<option value="${l}" ${s.language === l ? 'selected' : ''}>${t('lang.' + l)}</option>`).join('')}</select></label></div></div>
        <div class="scard-foot">${save()}</div>
      </form>
      <form class="scard" id="domain" data-settings>
        <div class="scard-body"><h2>${t('set.mainDomain')}</h2><p class="sub">${t('set.mainDomainSub')}</p>
          <div class="fields">
            <label>${t('set.mainDomain')}<input class="input mono" name="cookieDomain" value="${esc(s.cookieDomain.replace(/^\./, ''))}"></label>
            <label>${t('set.loginHost')}<input class="input mono" name="loginHost" value="${esc(s.loginHost)}"></label>
            <label>${t('set.adminHost')}<input class="input mono" name="adminHost" value="${esc(s.adminHost)}"></label>
          </div></div>
        <div class="scard-foot"><span class="small dim">${t('set.domainFoot')}</span>${save()}</div>
      </form>
      <form class="scard" id="sessions" data-settings>
        <div class="scard-body"><h2>${t('set.sessions')}</h2><p class="sub">${t('set.sessionsSub')}</p>
          <div class="inline-fields">${t('set.normal')}<input class="input mono" type="number" name="sessionHours" value="${s.sessionHours}" min="1" max="720">${t('set.hours')} ${t('set.remember')}<input class="input mono" type="number" name="rememberDays" value="${s.rememberDays}" min="1" max="365">${t('set.days')}</div></div>
        <div class="scard-foot"><span class="small dim">${t('set.newOnly')}</span>${save()}</div>
      </form>
      <form class="scard" id="security" data-settings>
        <div class="scard-body"><h2>${t('set.bf')}</h2><p class="sub">${t('set.bfSub')}</p>
          <div class="inline-fields"><input class="input mono" type="number" name="lockAttempts" value="${s.lockAttempts}" min="3" max="50">${t('set.attemptsIn')}<input class="input mono" type="number" name="lockWindowMin" value="${Math.round(s.lockWindowSec / 60)}" min="1" max="60">${t('set.minLockFor')}<input class="input mono" type="number" name="lockDurationMin" value="${Math.round(s.lockDurationSec / 60)}" min="1" max="1440">${t('set.min')}</div>
          <div class="toggle-line"><div class="grow"><div class="strong">${t('set.enforce')}</div><div class="small muted">${t('set.enforceSub')}</div></div>
            <button type="button" class="switch ${s.enforceAdmin2fa ? 'on' : ''}" data-sw-setting="enforceAdmin2fa" role="switch" aria-checked="${s.enforceAdmin2fa}"></button></div>
          <div class="toggle-line"><div class="grow"><div class="strong">${t('set.retention')}</div><div class="small muted">${t('set.retentionSub')}</div></div>
            <div class="inline-fields" style="margin:0"><input class="input mono" type="number" name="logRetentionDays" value="${s.logRetentionDays}" min="7" max="3650">${t('set.days')}</div></div>
        </div>
        <div class="scard-foot">${save()}</div>
      </form>
      ${oauthSection(oa.providers)}
      <div class="scard" id="caddy">
        <div class="scard-body"><h2>${t('set.caddy')} <span class="badge ${caddyOk ? 'ok' : 'warn'}"><i></i>${caddyOk ? t('badge.connected') : t('badge.incomplete')}</span></h2>
          <p class="sub">${t('set.caddySub')}</p>
          <div class="fields">
            <label>${t('set.adminApi')}<input class="input mono" value="${esc(c.admin)}" readonly></label>
            <label>${t('set.snippetDir')}<input class="input mono" value="${esc(c.dir)}" readonly></label>
          </div>
          <div class="inline-fields small" style="gap:18px">
            <span class="${c.enabled && c.writable ? 'ok-t' : 'warn-t'}">${c.enabled ? (c.writable ? t('chk.dirOk') : t('chk.dirRo')) : t('chk.dirMissing')}</span>
            <span class="${c.reachable ? 'ok-t' : 'warn-t'}">${c.reachable ? t('chk.apiOk') : t('chk.apiNo')}</span>
            <span class="${c.imported ? 'ok-t' : 'warn-t'}">${c.imported ? t('chk.importOk') : t('chk.importNo')}</span>
            <span class="${c.caddyfileWritable ? 'ok-t' : 'muted'}">${c.caddyfileWritable ? t('chk.cfOk') : t('chk.cfRo')}</span>
          </div>
          ${c.imported ? '' : `<div class="info" style="margin-top:16px">${t('set.addLine')}<br>${code(c.importLine)}</div>`}
          <div class="info" style="margin-top:16px">${t('set.ownBlocks', code('import wicket'), code(d.authAddr))}</div>
        </div>
      </div>
      <div class="scard" id="account">
        <div class="scard-body"><h2>${t('sec.account')}</h2><p class="sub">${t('set.signedInAs', `<b>${esc(u.username)}</b>`, esc(d.version))}</p>
          <div class="toggle-line"><div class="grow"><div class="strong">${t('set.password')}</div><div class="small muted">${t('set.pwSub')}</div></div><button type="button" class="btn sm" data-act="my-password">${t('common.change')}</button></div>
          <div class="toggle-line"><div class="grow"><div class="strong">${t('set.2fa')} <span class="badge ${u.totpEnabled ? 'ok' : 'warn'}" style="margin-left:6px"><i></i>${u.totpEnabled ? t('badge.on') : t('badge.off')}</span></div>
            <div class="small muted">${u.totpEnabled ? t('set.codesLeft', me.recoveryLeft) : t('set.2faSub')}</div></div>
            ${u.totpEnabled ? `<button type="button" class="btn sm" data-act="my-recovery">${t('set.newCodes')}</button>${me.enforce2fa ? '' : `<button type="button" class="btn sm danger" data-act="my-2fa-off">${t('set.disable')}</button>`}` : `<button type="button" class="btn-light sm" data-act="my-2fa">${t('set.setup')}</button>`}</div>
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
      language: all.language,
      cookieDomain: all.cookieDomain, loginHost: all.loginHost, adminHost: all.adminHost,
      sessionHours: Number(all.sessionHours), rememberDays: Number(all.rememberDays),
      lockAttempts: Number(all.lockAttempts), lockWindowSec: Number(all.lockWindowMin) * 60, lockDurationSec: Number(all.lockDurationMin) * 60,
      logRetentionDays: Number(all.logRetentionDays),
    };
    try {
      const r = await api('PUT', '/api/settings', body);
      if (r.relogin) { location.href = '/login'; return; }
      if (r.lang && r.lang !== LANG) { location.reload(); return; }
      toast(r.caddyError ? t('toast.savedCaddy', r.caddyError) : t('common.saved'), r.caddyError ? 'err' : '');
    } catch (ex) { toast(ex.message, 'err'); }
  }));
  wireOAuth();
  const secs = $$('[data-sec]');
  const mark = (id) => secs.forEach((a) => a.classList.toggle('on', a.dataset.sec === id));
  mark(sub || 'general');
  if (sub) { const el = document.getElementById(sub); if (el) el.scrollIntoView({ behavior: 'smooth' }); }
}

// ---------------------------------------------------------------- sign-in providers

const OAUTH_ICONS = {
  microsoft: '<svg width="18" height="18" viewBox="0 0 21 21"><rect x="1" y="1" width="9" height="9" fill="#F25022"/><rect x="11" y="1" width="9" height="9" fill="#7FBA00"/><rect x="1" y="11" width="9" height="9" fill="#00A4EF"/><rect x="11" y="11" width="9" height="9" fill="#FFB900"/></svg>',
  github: '<svg width="18" height="18" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>',
  google: '<svg width="18" height="18" viewBox="0 0 48 48"><path fill="#FFC107" d="M43.6 20.5H42V20H24v8h11.3C33.7 32.7 29.2 36 24 36c-6.6 0-12-5.4-12-12s5.4-12 12-12c3.1 0 5.8 1.2 7.9 3.1l5.7-5.7C34 6.1 29.3 4 24 4 12.9 4 4 12.9 4 24s8.9 20 20 20 20-8.9 20-20c0-1.3-.1-2.4-.4-3.5z"/><path fill="#FF3D00" d="M6.3 14.7l6.6 4.8C14.7 15.1 19 12 24 12c3.1 0 5.8 1.2 7.9 3.1l5.7-5.7C34 6.1 29.3 4 24 4 16.3 4 9.7 8.3 6.3 14.7z"/><path fill="#4CAF50" d="M24 44c5.2 0 9.9-2 13.4-5.2l-6.2-5.2C29.2 35.1 26.7 36 24 36c-5.2 0-9.6-3.3-11.3-7.9l-6.5 5C9.5 39.6 16.2 44 24 44z"/><path fill="#1976D2" d="M43.6 20.5H42V20H24v8h11.3c-.8 2.2-2.2 4.2-4.1 5.6l6.2 5.2C37 38.2 44 33 44 24c0-1.3-.1-2.4-.4-3.5z"/></svg>',
};

function oauthSection(list) {
  const status = (p) => p.enabled ? `<span class="badge ok"><i></i>${t('oauth.stActive')}</span>`
    : `<span class="badge"><i></i>${p.clientId ? t('oauth.stOff') : t('oauth.stNotConfigured')}</span>`;
  return `<div class="scard" id="login"><div class="scard-body"><h2>${t('oauth.title')}</h2><p class="sub">${t('oauth.sub')}</p>
      <div class="info" style="margin-top:16px">${t('oauth.note')}</div></div></div>
    ${list.map((p) => `<form class="scard" data-oauth="${p.id}">
      <div class="scard-body"><h2>${OAUTH_ICONS[p.id]}${esc(p.name)} ${status(p)}</h2>
        <p class="sub">${t('oauth.help.' + p.id)}</p>
        <div class="fields">
          <label>${t('oauth.clientId')}<input class="input mono" name="clientId" value="${esc(p.clientId)}" autocomplete="off" spellcheck="false"></label>
          <label>${t('oauth.clientSecret')}<input class="input mono" type="password" name="clientSecret" placeholder="${p.hasSecret ? t('oauth.secretSaved') : ''}" autocomplete="new-password"></label>
          ${p.id === 'microsoft' ? `<label>${t('oauth.tenant')}<input class="input mono" name="tenant" value="${esc(p.tenant)}" placeholder="${t('oauth.tenantPh')}" autocomplete="off" spellcheck="false"></label>` : ''}
        </div>
        <div class="fields"><label>${t('oauth.redirect')}<div class="row gap8"><input class="input mono" value="${esc(p.redirectUri)}" readonly><button type="button" class="btn sm" data-copy-text="${esc(p.redirectUri)}">${t('common.copy')}</button></div></label></div>
        <div class="small" data-oauth-result style="margin-top:12px"></div>
      </div>
      <div class="scard-foot">
        <label class="check"><input type="checkbox" name="enabled" ${p.enabled ? 'checked' : ''}><span class="box"></span>${t('oauth.enabled')}</label>
        <button type="button" class="btn sm right" data-oauth-test>${t('oauth.test')}</button>
        <button type="submit" class="btn-light sm">${t('common.save')}</button>
      </div>
    </form>`).join('')}`;
}

function wireOAuth() {
  $$('form[data-oauth]').forEach((form) => {
    const id = form.dataset.oauth;
    const out = $('[data-oauth-result]', form);
    const body = () => ({
      clientId: $('[name=clientId]', form).value.trim(), clientSecret: $('[name=clientSecret]', form).value,
      tenant: $('[name=tenant]', form)?.value.trim() || '', enabled: $('[name=enabled]', form).checked,
    });
    const show = (cls, msg) => { out.className = 'small ' + cls; out.textContent = msg; };
    $('[data-oauth-test]', form).addEventListener('click', async () => {
      show('muted', t('oauth.testing'));
      try { const r = await api('POST', `/api/oauth/${id}/test`, body()); show('ok-t', r.message); } catch (ex) { show('err-t', ex.message); }
    });
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      try { await api('PUT', `/api/oauth/${id}`, body()); toast(t('common.saved')); refresh(); }
      catch (ex) { show('err-t', ex.message); $('[name=enabled]', form).checked = false; }
    });
  });
}

async function twoFactorDialog() {
  const r = await api('POST', '/api/me/2fa');
  dialog({
    title: t('dlg2fa.title'),
    lead: t('dlg2fa.lead'),
    submit: t('dlg2fa.activate'),
    width: 640,
    body: `<div class="totp">
      <div class="qr"><img src="${esc(r.qr)}" alt="${t('dlg2fa.qrAlt')}" width="180" height="180"></div>
      <div class="totp-side">
        <div class="fg"><div class="lbl">${t('dlg2fa.manual')}</div><div class="secret"><span>${esc(r.secretGrouped)}</span><button type="button" class="mini" data-copy-text="${esc(r.secret)}">${t('common.copy')}</button></div></div>
        <div class="fg"><div class="lbl">${t('dlg2fa.code')}</div><input class="input mono" name="code" inputmode="numeric" autocomplete="one-time-code" maxlength="7" placeholder="123 456" style="font-size:18px;letter-spacing:4px"></div>
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
    body: `<div class="fg"><div class="lbl">${t('pwc.label')}</div><input class="input" type="password" name="password" autocomplete="current-password"></div>`,
    onSubmit: (form) => fn($('[name=password]', form).value),
  });
}

async function accountAction(act) {
  try {
    if (act === 'my-password') passwordDialog({ id: state.me.user.id });
    else if (act === 'my-2fa') await twoFactorDialog();
    else if (act === 'my-recovery') passwordConfirm(t('rec.title'), t('rec.lead'), t('rec.btn'), async (password) => {
      const r = await api('POST', '/api/me/recovery', { password });
      codesDialog(r.codes, state.me.user.username);
      refresh();
      return true;
    });
    else if (act === 'my-2fa-off') passwordConfirm(t('off.title'), t('off.lead'), t('off.btn'), async (password) => {
      await api('POST', '/api/me/2fa/disable', { password });
      toast(t('toast.2faOff'));
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

  const cp = e.target.closest('[data-copy-text]');
  if (cp) { await navigator.clipboard.writeText(cp.dataset.copyText); toast(t('toast.copied')); return; }
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
    try { await api('DELETE', `/api/sessions/${endS.dataset.endSession}`); toast(t('toast.sessionEnded')); drawUserDetail(); } catch (ex) { toast(ex.message, 'err'); }
    return;
  }
  const useTpl = e.target.closest('[data-use-tpl]');
  if (useTpl) { useTemplate(useTpl.dataset.useTpl); return; }
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
  LANG = state.me.lang && window.WICKET_I18N[state.me.lang] ? state.me.lang : 'en';
  applyStaticTexts();
  const u = state.me.user;
  $('#domainLabel').textContent = state.me.domain || '';
  $('#avatarBtn').textContent = u.username[0].toUpperCase();
  $('#menuHead').innerHTML = `<b>${esc(u.username)}</b><span>${u.role === 'admin' ? t('role.admin') : t('role.user')}</span>`;
  route();
})();
