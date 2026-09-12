'use strict';

// Admin UI for the 1.3 features: groups, network rules and session limits per site, Docker targets,
// user groups, invitations and passkeys, mail server, notifications, OIDC apps, integrations and
// branding. admin.js calls into window.WX from its site and user dialogs.
(() => {
  const lines = (s, sep = /[\n,]+/) => s.split(sep).map((x) => x.trim()).filter(Boolean);
  const node = (html) => { const d = document.createElement('div'); d.innerHTML = html.trim(); return d.firstElementChild; };
  const fg = (label, inner, sub = '') => `<div class="fg"><div class="lbl">${label}${sub ? ` <span>${sub}</span>` : ''}</div>${inner}</div>`;
  const checks = (list, sel, attr) => list.map((x) => `<label class="check"><input type="checkbox" ${attr}="${x.id}" ${sel.includes(x.id) ? 'checked' : ''}><span class="box"></span>${esc(x.name || x.username)}</label>`).join('');
  const checklist = (list, sel, attr, empty) => `<div class="checklist">${list.length ? checks(list, sel, attr) : `<span class="dim small">${empty}</span>`}</div>`;
  const picked = (root, attr) => $$(`[${attr}]:checked`, root).map((i) => Number(i.getAttribute(attr)));
  const copyRow = (v) => `<div class="row gap8"><input class="input mono" value="${esc(v)}" readonly><button type="button" class="btn sm" data-copy-text="${esc(v)}">${t('common.copy')}</button></div>`;
  const getGroups = async () => (await api('GET', '/api/groups')).groups;

  // the global copy handler ignores clicks inside dialogs
  function wireCopy(root) {
    root.addEventListener('click', async (e) => {
      const b = e.target.closest('[data-copy-text]');
      if (!b) return;
      e.preventDefault();
      await navigator.clipboard.writeText(b.dataset.copyText);
      toast(t('toast.copied'));
    });
  }

  Object.assign(EV, {
    passkey_fail: ['fail', 'err-t'], passkey_added: ['key', ''], passkey_removed: ['key', ''],
    oidc_login: ['ok', ''], password_reset_requested: ['key', 'warn-t'],
  });

  // ---------------------------------------------------------------- site dialog

  async function siteMount(form, d) {
    d.groups = d.groups || [];
    d.allowIps = d.allowIps || [];
    d.denyIps = d.denyIps || [];
    d.maxSessionHours = d.maxSessionHours || 0;
    const [groups, ct] = await Promise.all([
      getGroups().catch(() => []),
      api('GET', '/api/containers').catch(() => ({ containers: [] })),
    ]);
    if (!form.isConnected) return;

    const gfg = node(fg(t('wx.siteGroups'), checklist(groups, d.groups, 'data-wx-group', t('wx.noGroups'))));
    $('[data-userlist]', form).after(gfg);
    const sync = () => { gfg.hidden = d.access !== 'users'; };
    form.addEventListener('input', sync);
    form.addEventListener('change', sync);
    sync();

    const opts = ct.containers.flatMap((c) => c.targets.map((tg) => `<option value="${esc(tg)}">${esc(c.name)} · ${esc(tg)}${c.state !== 'running' ? ` (${esc(c.state)})` : ''}</option>`));
    if (opts.length) {
      const sel = node(`<select class="input" data-wx-container><option value="">${t('wx.container')}</option>${opts.join('')}</select>`);
      $('[data-target] input', form).after(sel);
      sel.addEventListener('change', () => {
        if (!sel.value) return;
        const input = $('[name=target]', form);
        input.value = sel.value;
        input.dispatchEvent(new Event('input', { bubbles: true }));
        sel.value = '';
      });
    }

    const sec = node(`<div class="fg" data-wx-sec><div class="lbl">${t('wx.network')}</div>
      <div class="grid2">
        <div class="fg"><div class="small muted">${t('wx.allowIps')}</div><textarea class="input" data-wx-allow rows="2" placeholder="192.168.1.0/24" spellcheck="false">${esc(d.allowIps.join('\n'))}</textarea></div>
        <div class="fg"><div class="small muted">${t('wx.denyIps')}</div><textarea class="input" data-wx-deny rows="2" placeholder="203.0.113.7" spellcheck="false">${esc(d.denyIps.join('\n'))}</textarea></div>
      </div>
      <div class="dim small">${t('wx.ipHint')}</div>
      <div class="inline-fields" style="margin-top:4px">${t('wx.maxSession')}<input class="input mono" type="number" min="0" max="720" data-wx-hours value="${d.maxSessionHours}">${t('wx.maxSessionUnit')}</div></div>`);
    $('[data-sw="enabled"]', form).closest('.fg').before(sec);
  }

  function siteCollect(d, form) {
    if (!$('[data-wx-sec]', form)) return; // extras not loaded yet: keep the values as they are
    d.groups = picked(form, 'data-wx-group');
    d.allowIps = lines($('[data-wx-allow]', form).value);
    d.denyIps = lines($('[data-wx-deny]', form).value);
    d.maxSessionHours = Number($('[data-wx-hours]', form).value) || 0;
  }

  // ---------------------------------------------------------------- user dialog and detail

  function userMount(form, u) {
    const edit = Boolean(u);
    $('.grid2', form).insertAdjacentHTML('afterend', `<div class="dim small">${t('wx.auditorHint')}</div>`);
    (async () => {
      const [groups, mail] = await Promise.all([getGroups().catch(() => []), edit ? null : api('GET', '/api/mail').catch(() => null)]);
      if (!form.isConnected) return;
      const box = node(fg(t('wx.userGroups'), `<div data-wx-ugroups>${checklist(groups, u?.groups || [], 'data-wx-group', t('wx.noGroups'))}</div>`));
      $('.grid2', form).after(box);
      const pw = $('[data-pw]', form);
      if (!edit && pw && mail?.smtp?.host) {
        const inv = node(`<label class="check"><input type="checkbox" data-wx-invite><span class="box"></span>${t('wx.invite')}</label>`);
        pw.before(inv);
        $('input', inv).addEventListener('change', (e) => { pw.hidden = e.target.checked; });
      }
    })();
  }

  function userCollect(form) {
    const out = { invite: Boolean($('[data-wx-invite]', form)?.checked) };
    if ($('[data-wx-ugroups]', form)) out.groups = picked(form, 'data-wx-group');
    return out;
  }

  async function userDetail(box, u, self) {
    const [groups, pk] = await Promise.all([
      getGroups().catch(() => []),
      api('GET', `/api/users/${u.id}/passkeys`).catch(() => ({ passkeys: [] })),
    ]);
    if (!box.isConnected || state.userSel !== u.id || $('[data-wx-detail]', box)) return;
    const names = groups.filter((g) => (u.groups || []).includes(g.id)).map((g) => esc(g.name));
    $('.kv', box)?.parentElement.insertAdjacentHTML('beforeend', `<div class="kv"><span>${t('wx.userGroups')}</span><span>${names.length ? names.join(', ') : '–'}</span></div>`);
    const list = pk.passkeys || [];
    const acts = $('.detail-actions', box);
    acts.before(node(`<div data-wx-detail><div class="strong small" style="margin-bottom:12px">${t('wx.passkeys')}</div>
      ${list.length ? list.map((p) => `<div class="sess">${I.key}<div class="grow"><div class="small">${esc(p.name || t('pk.unnamed'))}</div>
        <div class="mono dim" style="font-size:11px">${t('pk.used', ago(p.lastUsed))}</div></div><button class="link" data-wx-pk="${p.id}">${t('common.remove')}</button></div>`).join('')
        : `<div class="dim small">${t('wx.noPasskeys')}</div>`}</div>`));
    if (u.email && !self) {
      acts.insertAdjacentHTML('afterbegin', `${u.lastSeen ? '' : `<button class="btn sm" data-wx-mail="invite">${t('wx.sendInvite')}</button>`}<button class="btn sm" data-wx-mail="reset-link">${t('wx.sendReset')}</button>`);
    }
    box.onclick = async (e) => {
      const m = e.target.closest('[data-wx-mail]');
      const p = e.target.closest('[data-wx-pk]');
      if (!m && !p) return;
      try {
        if (m) {
          m.disabled = true;
          const r = await api('POST', `/api/users/${u.id}/${m.dataset.wxMail}`, {});
          toast(r.message);
        } else if (await confirmDialog(t('pk.removeTitle'), t('pk.removeText', esc(u.username)), t('common.remove'), true)) {
          await api('DELETE', `/api/users/${u.id}/passkeys/${p.dataset.wxPk}`);
          toast(t('pk.removed'));
          drawUserDetail();
        }
      } catch (ex) { toast(ex.message, 'err'); }
      if (m) m.disabled = false;
    };
  }

  // ---------------------------------------------------------------- groups page

  async function renderGroups() {
    const [groups, ud] = await Promise.all([getGroups(), api('GET', '/api/users')]);
    const uname = (id) => ud.users.find((u) => u.id === id)?.username;
    const newBtn = (cls) => `<button class="${cls}" data-wx-newgroup>${I.plus}${t('grp.create')}</button>`;
    view.innerHTML = `<div class="page">
      <div class="page-head"><div><h1>${t('grp.title')}</h1><p class="muted">${t('grp.sub')}</p></div>
        <div class="actions">${newBtn('btn-light')}</div></div>
      ${groups.length ? `<div class="table"><div class="tr th wx-groups-grid"><div>${t('grp.name')}</div><div>${t('grp.members')}</div><div></div></div>
        ${groups.map((g) => `<div class="tr wx-groups-grid clickable" data-wx-grouprow="${g.id}">
          <div><div class="strong">${esc(g.name)}</div><div class="small dim">${esc(g.description || '')}</div></div>
          <div class="muted small">${esc(g.members.map(uname).filter(Boolean).join(', ') || '–')}</div>
          <div class="tr-end">${I.edit}</div></div>`).join('')}</div>`
        : `<div class="panel"><div class="empty">${t('grp.empty')}${newBtn('btn-light sm')}</div></div>`}
      <div class="info" style="margin-top:20px">${t('grp.note')}</div></div>`;
    view.onclick = (e) => {
      if (e.target.closest('[data-wx-newgroup]')) { groupDialog(null, ud.users); return; }
      const row = e.target.closest('[data-wx-grouprow]');
      if (row) groupDialog(groups.find((g) => g.id === Number(row.dataset.wxGrouprow)), ud.users);
    };
  }

  function groupDialog(g, users) {
    const edit = Boolean(g);
    dialog({
      title: edit ? t('grp.edit', esc(g.name)) : t('grp.new'),
      submit: edit ? t('common.save') : t('grp.createBtn'),
      width: 520,
      footLeft: edit ? `<button type="button" class="btn danger" data-wx-delgroup>${t('common.delete')}</button>` : '',
      body: `${fg(t('grp.name'), `<input class="input" name="name" value="${esc(g?.name || '')}" placeholder="${t('grp.namePh')}" autocomplete="off" spellcheck="false">`)}
        ${fg(t('grp.desc'), `<input class="input" name="description" value="${esc(g?.description || '')}" autocomplete="off">`, t('common.optional'))}
        ${fg(t('grp.members'), checklist(users, g?.members || [], 'data-wx-member', t('f.noUsers')))}`,
      onMount(form) {
        $('[data-wx-delgroup]', form)?.addEventListener('click', async () => {
          closeDialog();
          if (!(await confirmDialog(t('grp.deleteTitle'), t('grp.deleteText', esc(g.name)), t('common.delete'), true))) return;
          try { await api('DELETE', `/api/groups/${g.id}`); toast(t('grp.deleted')); refresh(); } catch (ex) { toast(ex.message, 'err'); }
        });
      },
      async onSubmit(form) {
        const body = { name: $('[name=name]', form).value.trim(), description: $('[name=description]', form).value.trim(), members: picked(form, 'data-wx-member') };
        if (edit) await api('PUT', `/api/groups/${g.id}`, body);
        else await api('POST', '/api/groups', body);
        toast(t('common.saved'));
        refresh();
      },
    });
  }

  // ---------------------------------------------------------------- settings: mail server

  function mailCard(m) {
    const s = m.smtp;
    const on = Boolean(s.host);
    const narrow = 'style="flex:0 1 160px;min-width:120px"';
    return `<form class="scard" id="mail" data-wx-mailform><div class="scard-body">
      <h2>${t('mail.title')} <span class="badge ${on ? 'ok' : ''}"><i></i>${on ? t('wx.configured') : t('wx.notConfigured')}</span></h2>
      <p class="sub">${t('mail.sub')}</p>
      <div class="fields">
        <label>${t('mail.host')}<input class="input mono" name="host" value="${esc(s.host)}" placeholder="smtp.example.com" autocomplete="off" spellcheck="false"></label>
        <label ${narrow}>${t('mail.port')}<input class="input mono" type="number" name="port" value="${s.port || ''}" placeholder="587"></label>
        <label ${narrow}>${t('mail.security')}<select class="input" name="security">${['starttls', 'tls', 'none'].map((x) => `<option value="${x}" ${s.security === x ? 'selected' : ''}>${t('mail.sec.' + x)}</option>`).join('')}</select></label>
      </div>
      <div class="fields">
        <label>${t('mail.user')}<input class="input mono" name="username" value="${esc(s.username)}" autocomplete="off" spellcheck="false"></label>
        <label>${t('mail.pass')}<input class="input mono" type="password" name="password" placeholder="${m.hasPassword ? t('oauth.secretSaved') : ''}" autocomplete="new-password"></label>
      </div>
      <div class="fields">
        <label>${t('mail.from')}<input class="input mono" name="from" value="${esc(s.from)}" placeholder="wicket@example.com" autocomplete="off" spellcheck="false"></label>
        <label>${t('mail.fromName')}<input class="input" name="fromName" value="${esc(s.fromName)}" placeholder="Wicket"></label>
      </div>
      <div class="small" data-wx-result style="margin-top:12px"></div></div>
      <div class="scard-foot">
        <input class="input" type="email" name="testTo" style="max-width:240px;height:32px" placeholder="${esc(state.me.user.email || t('mail.testTo'))}">
        <button type="button" class="btn sm" data-wx-mailtest>${t('mail.test')}</button>
        <button type="submit" class="btn-light sm right">${t('common.save')}</button></div></form>`;
  }

  function wireMail() {
    const form = $('[data-wx-mailform]');
    const out = $('[data-wx-result]', form);
    const show = (cls, msg) => { out.className = 'small ' + cls; out.textContent = msg; };
    const v = (n) => $(`[name=${n}]`, form).value.trim();
    const body = () => ({ host: v('host'), port: Number(v('port')) || 0, security: v('security'), username: v('username'),
      password: $('[name=password]', form).value, from: v('from'), fromName: v('fromName') });
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      try { await api('PUT', '/api/mail', body()); toast(t('common.saved')); refresh(); } catch (ex) { show('err-t', ex.message); }
    });
    $('[data-wx-mailtest]', form).addEventListener('click', async () => {
      show('muted', t('mail.testing'));
      try {
        await api('PUT', '/api/mail', body());
        const r = await api('POST', '/api/mail/test', { to: v('testTo') });
        show('ok-t', r.message);
      } catch (ex) { show('err-t', ex.message); }
    });
  }

  // ---------------------------------------------------------------- settings: notifications

  function channelHTML(c, i, events) {
    return `<div class="wx-item" data-ch="${i}">
      <div class="fields" style="margin-top:0">
        <label>${t('ntf.name')}<input class="input" data-k="name" value="${esc(c.name)}" placeholder="${t('ntf.namePh')}"></label>
        <label style="flex:0 1 170px;min-width:140px">${t('ntf.type')}<select class="input" data-k="type">${['email', 'webhook', 'ntfy'].map((x) => `<option value="${x}" ${c.type === x ? 'selected' : ''}>${t('ntf.type.' + x)}</option>`).join('')}</select></label>
      </div>
      <div class="fields">
        <label>${t('ntf.target.' + c.type)}<input class="input mono" data-k="target" value="${esc(c.target)}" placeholder="${t('ntf.targetPh.' + c.type)}" spellcheck="false"></label>
        ${c.type === 'email' ? '' : `<label style="flex:0 1 240px">${t('ntf.token')}<input class="input mono" type="password" data-k="token" value="${esc(c.token || '')}" placeholder="${c.hasToken ? t('oauth.secretSaved') : t('common.optional')}" autocomplete="new-password"></label>`}
      </div>
      <div class="wx-events">${events.map((ev) => `<label class="check"><input type="checkbox" data-ev="${ev}" ${c.events.includes(ev) ? 'checked' : ''}><span class="box"></span>${t('ntf.ev.' + ev)}</label>`).join('')}</div>
      <div class="row gap8" style="margin-top:14px">
        <label class="check"><input type="checkbox" data-k="enabled" ${c.enabled ? 'checked' : ''}><span class="box"></span>${t('ntf.enabled')}</label>
        <button type="button" class="btn sm right" data-ch-test ${c.id ? '' : 'disabled'} title="${c.id ? '' : t('ntf.saveFirst')}">${t('ntf.test')}</button>
        <button type="button" class="btn sm danger" data-ch-del>${t('common.remove')}</button>
      </div></div>`;
  }

  function notifyCard(n) {
    return `<form class="scard" id="notify" data-wx-notifyform><div class="scard-body">
      <h2>${t('ntf.title')}</h2><p class="sub">${t('ntf.sub')}</p>
      ${n.mailConfigured ? '' : `<div class="info" style="margin-top:16px">${t('ntf.mailMissing')}</div>`}
      <div class="wx-list" data-ch-list></div>
      <div class="small err-t" data-wx-result style="margin-top:12px"></div></div>
      <div class="scard-foot"><button type="button" class="btn sm" data-ch-add>${I.plus}${t('ntf.add')}</button>
        <button type="submit" class="btn-light sm right">${t('common.save')}</button></div></form>`;
  }

  function wireNotify(n) {
    const form = $('[data-wx-notifyform]');
    const list = $('[data-ch-list]', form);
    const chs = n.channels.map((c) => ({ ...c, token: '' }));
    const draw = () => {
      list.innerHTML = chs.length ? chs.map((c, i) => channelHTML(c, i, n.events)).join('') : `<div class="dim small">${t('ntf.empty')}</div>`;
    };
    const read = () => $$('[data-ch]', list).forEach((el) => {
      const c = chs[Number(el.dataset.ch)];
      $$('[data-k]', el).forEach((i) => { c[i.dataset.k] = i.type === 'checkbox' ? i.checked : i.value.trim(); });
      c.events = $$('[data-ev]:checked', el).map((i) => i.dataset.ev);
    });
    list.addEventListener('change', (e) => { if (e.target.dataset.k === 'type') { read(); draw(); } });
    list.addEventListener('click', async (e) => {
      const el = e.target.closest('[data-ch]');
      if (!el) return;
      const idx = Number(el.dataset.ch);
      if (e.target.closest('[data-ch-del]')) { read(); chs.splice(idx, 1); draw(); }
      if (e.target.closest('[data-ch-test]')) {
        try { const r = await api('POST', '/api/notify/test', { id: chs[idx].id }); toast(r.message); } catch (ex) { toast(ex.message, 'err'); }
      }
    });
    $('[data-ch-add]', form).addEventListener('click', () => {
      read();
      chs.push({ id: '', name: '', type: n.mailConfigured ? 'email' : 'ntfy', target: '', token: '', events: [...n.events], enabled: true });
      draw();
    });
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      read();
      try {
        await api('PUT', '/api/notify', { channels: chs.map(({ hasToken, ...c }) => c) });
        toast(t('common.saved'));
        refresh();
      } catch (ex) { $('[data-wx-result]', form).textContent = ex.message; }
    });
    draw();
  }

  // ---------------------------------------------------------------- settings: OIDC apps

  const oidcAccess = (c) => c.access !== 'users' ? t('access.' + c.access)
    : [c.users.length ? t('n.users', c.users.length) : '', c.groups.length ? t('n.groups', c.groups.length) : ''].filter(Boolean).join(', ') || t('n.users', 0);

  function oidcCard(o) {
    return `<div class="scard" id="oidc"><div class="scard-body">
      <h2>${t('oidc.title')}</h2><p class="sub">${t('oidc.sub')}</p>
      <div class="fields"><label>${t('oidc.issuer')}${copyRow(o.issuer)}</label><label>${t('oidc.discovery')}${copyRow(o.discovery)}</label></div>
      <div class="wx-list">${o.clients.length ? o.clients.map((c) => `<div class="wx-item row gap8">
        <div class="grow"><div class="strong">${esc(c.name)} ${c.public ? `<span class="tag">${t('oidc.publicTag')}</span>` : ''}</div>
          <div class="small dim mono">${esc(c.id)} · ${esc(c.redirectUris.join(', '))}</div></div>
        <span class="muted small nowrap">${oidcAccess(c)}</span>
        <button type="button" class="btn sm" data-oidc-edit="${esc(c.id)}">${t('common.edit')}</button></div>`).join('')
        : `<div class="dim small">${t('oidc.empty')}</div>`}</div></div>
      <div class="scard-foot"><button type="button" class="btn-light sm right" data-oidc-new>${I.plus}${t('oidc.add')}</button></div></div>`;
  }

  function secretDialog(c, secret, discovery) {
    dialog({
      title: t('oidc.readyTitle', esc(c.name)),
      lead: secret ? t('oidc.secretLead') : t('oidc.publicLead'),
      width: 560,
      body: `${fg(t('oidc.clientId'), copyRow(c.id))}${secret ? fg(t('oidc.secret'), copyRow(secret)) : ''}${fg(t('oidc.discovery'), copyRow(discovery))}`,
      onMount: wireCopy,
    });
  }

  function oidcDialog(c, users, groups, discovery) {
    const edit = Boolean(c);
    const d = c ? { ...c } : { name: '', redirectUris: [], public: false, access: 'all', users: [], groups: [] };
    const cid = edit ? encodeURIComponent(c.id) : '';
    dialog({
      title: edit ? t('oidc.edit', esc(c.name)) : t('oidc.new'),
      lead: edit ? '' : t('oidc.newLead'),
      submit: edit ? t('common.save') : t('oidc.createBtn'),
      width: 580,
      footLeft: edit ? `<button type="button" class="btn danger" data-oidc-del>${t('common.delete')}</button>${d.public ? '' : `<button type="button" class="btn" data-oidc-rotate>${t('oidc.rotate')}</button>`}` : '',
      body: `${edit ? fg(t('oidc.clientId'), copyRow(c.id)) : ''}
        ${fg(t('oidc.name'), `<input class="input" name="name" value="${esc(d.name)}" placeholder="Grafana" autocomplete="off">`)}
        ${fg(t('oidc.redirects'), `<textarea class="input" name="redirects" rows="3" spellcheck="false" placeholder="https://grafana.example.com/login/generic_oauth">${esc(d.redirectUris.join('\n'))}</textarea>`, t('oidc.redirectsSub'))}
        <div class="grid2">
          ${fg(t('f.access'), `<select class="input" name="access">${['all', 'admins', 'users'].map((x) => `<option value="${x}" ${d.access === x ? 'selected' : ''}>${x === 'users' ? t('f.usersWithAccess') : t('access.' + x)}</option>`).join('')}</select>`)}
          ${fg(t('oidc.type'), `<select class="input" name="public"><option value="0">${t('oidc.confidential')}</option><option value="1" ${d.public ? 'selected' : ''}>${t('oidc.public')}</option></select>`)}
        </div>
        <div class="grid2" data-oidc-who ${d.access === 'users' ? '' : 'hidden'}>
          ${fg(t('nav.users'), checklist(users, d.users, 'data-wx-member', t('f.noUsers')))}
          ${fg(t('nav.groups'), checklist(groups, d.groups, 'data-wx-group', t('wx.noGroups')))}
        </div>
        <div class="dim small">${t('oidc.publicHint')}</div>`,
      onMount(form) {
        wireCopy(form);
        $('[name=access]', form).addEventListener('change', (e) => { $('[data-oidc-who]', form).hidden = e.target.value !== 'users'; });
        $('[data-oidc-del]', form)?.addEventListener('click', async () => {
          closeDialog();
          if (!(await confirmDialog(t('oidc.deleteTitle'), t('oidc.deleteText', esc(c.name)), t('common.delete'), true))) return;
          try { await api('DELETE', `/api/oidc/${cid}`); toast(t('oidc.deleted')); refresh(); } catch (ex) { toast(ex.message, 'err'); }
        });
        $('[data-oidc-rotate]', form)?.addEventListener('click', async () => {
          closeDialog();
          if (!(await confirmDialog(t('oidc.rotate'), t('oidc.rotateText', esc(c.name)), t('oidc.rotate'), true))) return;
          try { const r = await api('POST', `/api/oidc/${cid}/secret`); secretDialog(c, r.secret, discovery); } catch (ex) { toast(ex.message, 'err'); }
        });
      },
      async onSubmit(form) {
        const body = {
          name: $('[name=name]', form).value.trim(),
          redirectUris: lines($('[name=redirects]', form).value, /\n+/),
          public: $('[name=public]', form).value === '1',
          access: $('[name=access]', form).value,
          users: picked(form, 'data-wx-member'),
          groups: picked(form, 'data-wx-group'),
        };
        const r = edit ? await api('PUT', `/api/oidc/${cid}`, body) : await api('POST', '/api/oidc', body);
        refresh();
        if (!edit || r.secret) { secretDialog(r.client, r.secret, discovery); return true; }
        toast(t('common.saved'));
      },
    });
  }

  // ---------------------------------------------------------------- settings: integrations

  function integrationsCard(x) {
    const snippet = (title, hint, text) => `<div class="fg" style="margin-top:20px"><div class="lbl">${title} <span>${hint}</span></div>
      <div class="code-box"><div class="code-head"><span>${title}</span><button type="button" class="link right" data-copy-text="${esc(text)}">${t('common.copy')}</button></div><pre>${esc(text)}</pre></div></div>`;
    const line = (title, text, on) => `<div class="toggle-line"><div class="grow"><div class="strong">${title}</div><div class="small muted">${text}</div></div>
      <span class="badge ${on ? 'ok' : ''}"><i></i>${on ? t('badge.on') : t('badge.off')}</span></div>`;
    return `<div class="scard" id="integrations"><div class="scard-body">
      <h2>${t('int.title')}</h2><p class="sub">${t('int.sub')}</p>
      ${snippet('Traefik', t('int.traefikHint'), x.traefik)}
      ${snippet('nginx', t('int.nginxHint'), x.nginx)}
      ${line(t('int.metrics'), x.metrics.tokenSet ? t('int.metricsOn', code(x.metrics.path)) : t('int.metricsOff', code('WICKET_METRICS_TOKEN')), x.metrics.tokenSet)}
      ${line(t('int.docker'), x.docker ? t('int.dockerOn') : t('int.dockerOff', code('WICKET_DOCKER_HOST')), x.docker)}
    </div></div>`;
  }

  // ---------------------------------------------------------------- settings: my passkeys

  function accountPasskeys(list) {
    const body = $('#account .scard-body');
    if (!body) return;
    const ok = window.WicketPasskey && window.WicketPasskey.supported();
    const el = node(`<div class="toggle-line" style="align-items:flex-start"><div class="grow">
      <div class="strong">${t('wx.passkeys')} <span class="badge ${list.length ? 'ok' : ''}" style="margin-left:6px"><i></i>${list.length}</span></div>
      <div class="small muted">${t('pk.sub')}</div>
      ${list.map((p) => `<div class="sess" style="margin-top:10px">${I.key}<div class="grow"><div class="small">${esc(p.name || t('pk.unnamed'))}</div>
        <div class="mono dim" style="font-size:11px">${t('pk.added', ago(p.createdAt))} · ${t('pk.used', ago(p.lastUsed))}</div></div>
        <button type="button" class="link" data-mypk="${p.id}">${t('common.remove')}</button></div>`).join('')}</div>
      ${ok ? `<button type="button" class="btn-light sm" data-mypk-add>${t('pk.add')}</button>` : `<span class="small dim">${t('pk.unsupported')}</span>`}</div>`);
    body.append(el);
    el.addEventListener('click', async (e) => {
      const add = e.target.closest('[data-mypk-add]');
      const rm = e.target.closest('[data-mypk]');
      try {
        if (add) {
          add.disabled = true;
          await window.WicketPasskey.register(device(navigator.userAgent));
          toast(t('pk.addedToast'));
          refresh();
        } else if (rm && await confirmDialog(t('pk.removeTitle'), t('pk.removeSelf'), t('common.remove'), true)) {
          await api('DELETE', `/api/me/passkeys/${rm.dataset.mypk}`);
          toast(t('pk.removed'));
          refresh();
        }
      } catch (ex) {
        toast(ex && ex.name === 'NotAllowedError' ? t('pk.cancelled') : ex.message, 'err');
        if (add) add.disabled = false;
      }
    });
  }

  // ---------------------------------------------------------------- settings route

  const EXT_SECTIONS = ['mail', 'notify', 'oidc', 'integrations'];
  SECTIONS.splice(SECTIONS.indexOf('caddy'), 0, ...EXT_SECTIONS);

  routes.settings = async (sub) => {
    await renderSettings(sub);
    const [mail, ntf, oidc, integ, groups, ud, pk] = await Promise.all([
      api('GET', '/api/mail'), api('GET', '/api/notify'), api('GET', '/api/oidc'), api('GET', '/api/integrations'),
      getGroups(), api('GET', '/api/users'), api('GET', '/api/me/passkeys'),
    ]);
    const anchor = document.getElementById('caddy');
    if (!anchor) return;
    anchor.insertAdjacentHTML('beforebegin', mailCard(mail) + notifyCard(ntf) + oidcCard(oidc) + integrationsCard(integ));
    wireMail();
    wireNotify(ntf);
    $('#oidc').addEventListener('click', (e) => {
      if (e.target.closest('[data-oidc-new]')) { oidcDialog(null, ud.users, groups, oidc.discovery); return; }
      const ed = e.target.closest('[data-oidc-edit]');
      if (ed) oidcDialog(oidc.clients.find((c) => c.id === ed.dataset.oidcEdit), ud.users, groups, oidc.discovery);
    });
    accountPasskeys(pk.passkeys || []);
    if (EXT_SECTIONS.includes(sub)) document.getElementById(sub)?.scrollIntoView({ behavior: 'smooth' });
  };

  // ---------------------------------------------------------------- templates route: branding

  function brandingCard(b, preview) {
    return `<form class="scard" id="branding" style="margin-top:28px" data-wx-brand><div class="scard-body">
      <h2>${t('br.title')}</h2><p class="sub">${t('br.sub')}</p>
      <div class="fields">
        <label>${t('br.name')}<input class="input" name="name" value="${esc(b.name)}" maxlength="40" placeholder="wicket"></label>
        <label>${t('br.accent')}<div class="row gap8"><input type="color" class="input wx-color" data-color value="${esc(b.accent || '#ededed')}">
          <input class="input mono" name="accent" value="${esc(b.accent)}" placeholder="#0070f3" maxlength="7" spellcheck="false"></div></label>
      </div>
      <div class="fields"><label>${t('br.footer')}<input class="input" name="footer" value="${esc(b.footer)}" maxlength="120" placeholder="${t('br.footerPh')}"></label></div>
      <div class="toggle-line"><div class="grow"><div class="strong">${t('br.logo')}</div><div class="small muted">${t('br.logoHint')}</div></div>
        <div class="wx-logo-box" data-logo-prev>${b.logo ? `<img class="wx-logo" src="${esc(b.logo)}" alt="">` : `<span class="dim small">${t('br.noLogo')}</span>`}</div>
        <label class="btn sm">${t('br.logoPick')}<input type="file" accept="image/png,image/jpeg,image/webp,image/gif,image/svg+xml" data-logo hidden></label>
        <button type="button" class="btn sm danger" data-logo-rm ${b.logo ? '' : 'hidden'}>${t('common.remove')}</button></div>
      <div class="toggle-line"><div class="grow"><div class="strong">${t('br.hideFooter')}</div><div class="small muted">${t('br.hideFooterSub')}</div></div>
        <button type="button" class="switch ${b.hideFooter ? 'on' : ''}" data-hide role="switch" aria-checked="${b.hideFooter}"></button></div>
      </div>
      <div class="scard-foot">${preview ? `<a class="btn sm" href="${esc(preview)}" target="_blank" rel="noopener">${t('tpl.preview')} ↗</a>` : ''}
        <button type="submit" class="btn-light sm right">${t('common.save')}</button></div></form>`;
  }

  function wireBranding(b) {
    const form = $('[data-wx-brand]');
    let logo; // undefined = keep, '' = remove, data: URI = replace
    let hide = b.hideFooter;
    const prev = $('[data-logo-prev]', form);
    const rm = $('[data-logo-rm]', form);
    const color = $('[data-color]', form);
    const accent = $('[name=accent]', form);
    color.addEventListener('input', () => { accent.value = color.value; });
    accent.addEventListener('input', () => { if (/^#[0-9a-f]{6}$/i.test(accent.value)) color.value = accent.value; });
    $('[data-hide]', form).addEventListener('click', (e) => {
      hide = !hide;
      e.currentTarget.classList.toggle('on', hide);
      e.currentTarget.setAttribute('aria-checked', hide);
    });
    $('[data-logo]', form).addEventListener('change', (e) => {
      const f = e.target.files[0];
      e.target.value = '';
      if (!f) return;
      if (f.size > 256 * 1024) { toast(t('br.tooBig'), 'err'); return; }
      const r = new FileReader();
      r.onload = () => { logo = r.result; prev.innerHTML = `<img class="wx-logo" src="${esc(logo)}" alt="">`; rm.hidden = false; };
      r.readAsDataURL(f);
    });
    rm.addEventListener('click', () => { logo = ''; prev.innerHTML = `<span class="dim small">${t('br.noLogo')}</span>`; rm.hidden = true; });
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const body = { name: $('[name=name]', form).value.trim(), accent: accent.value.trim(), footer: $('[name=footer]', form).value.trim(), hideFooter: hide };
      if (logo !== undefined) body.logo = logo;
      try { await api('PUT', '/api/branding', body); toast(t('common.saved')); refresh(); } catch (ex) { toast(ex.message, 'err'); }
    });
  }

  routes.templates = async () => {
    await renderTemplates();
    const b = await api('GET', '/api/branding');
    const page = $('.page', view);
    if (!page) return;
    page.insertAdjacentHTML('beforeend', brandingCard(b, $('.tpl-card.on .tpl-thumb', page)?.getAttribute('href')));
    wireBranding(b);
  };

  routes.groups = renderGroups;

  window.WX = { siteMount, siteCollect, userMount, userCollect, userDetail };
})();
