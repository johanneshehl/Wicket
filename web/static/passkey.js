'use strict';

// Passkeys: sign-in without a password (login pages) and adding a passkey (signed-in pages, admin).
(() => {
  const dec = (s) => {
    const b = atob(s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4));
    return Uint8Array.from(b, (c) => c.charCodeAt(0)).buffer;
  };
  const enc = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');

  const post = async (url, body) => {
    const r = await fetch(url, {
      method: 'POST', credentials: 'same-origin',
      headers: { 'X-Wicket': '1', 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const d = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(d.error || r.statusText);
    return d;
  };

  const credJSON = (c) => {
    const r = c.response;
    const out = {
      id: c.id, rawId: enc(c.rawId), type: c.type,
      clientExtensionResults: c.getClientExtensionResults ? c.getClientExtensionResults() : {},
      response: { clientDataJSON: enc(r.clientDataJSON) },
    };
    if (c.authenticatorAttachment) out.authenticatorAttachment = c.authenticatorAttachment;
    if (r.attestationObject) {
      out.response.attestationObject = enc(r.attestationObject);
      if (r.getTransports) out.response.transports = r.getTransports();
    }
    if (r.authenticatorData) {
      out.response.authenticatorData = enc(r.authenticatorData);
      out.response.signature = enc(r.signature);
      out.response.userHandle = r.userHandle ? enc(r.userHandle) : null;
    }
    return out;
  };

  const api = {
    supported: () => Boolean(window.PublicKeyCredential && navigator.credentials && window.isSecureContext),
    async register(name) {
      const opts = await post('/passkey/register/begin');
      const pk = opts.publicKey;
      pk.challenge = dec(pk.challenge);
      pk.user.id = dec(pk.user.id);
      (pk.excludeCredentials || []).forEach((c) => { c.id = dec(c.id); });
      const cred = await navigator.credentials.create({ publicKey: pk });
      return post('/passkey/register/finish?name=' + encodeURIComponent(name || ''), credJSON(cred));
    },
    async login(rd) {
      const opts = await post('/passkey/login/begin');
      const pk = opts.publicKey;
      pk.challenge = dec(pk.challenge);
      (pk.allowCredentials || []).forEach((c) => { c.id = dec(c.id); });
      const cred = await navigator.credentials.get({ publicKey: pk });
      return post('/passkey/login/finish?rd=' + encodeURIComponent(rd || ''), credJSON(cred));
    },
  };
  window.WicketPasskey = api;

  const message = (el, e) => (e && e.name === 'NotAllowedError' ? el.dataset.cancelled : (e && e.message) || el.dataset.failed);

  // the markup stays neutral; the provider-button look is only applied where passkeys work
  document.querySelectorAll('[data-pk-wrap]').forEach((wrap) => {
    if (!api.supported()) return;
    wrap.hidden = false;
    if (wrap.classList.contains('pk-wrap')) wrap.classList.add('oauth');
  });

  document.querySelectorAll('[data-passkey-login]').forEach((btn) => {
    btn.classList.add('btn-oauth');
    let err = null;
    btn.addEventListener('click', async () => {
      btn.disabled = true;
      if (err) err.hidden = true;
      try {
        const r = await api.login(btn.dataset.rd);
        location.href = r.redirect || '/';
      } catch (e) {
        if (!err) {
          err = document.createElement('div');
          err.className = 'alert';
          err.setAttribute('role', 'alert');
          btn.after(err);
        }
        err.textContent = message(btn, e);
        err.hidden = false;
        btn.disabled = false;
      }
    });
  });

  document.querySelectorAll('[data-passkey-register]').forEach((btn) => {
    const out = document.querySelector('[data-passkey-status]');
    btn.addEventListener('click', async () => {
      btn.disabled = true;
      try {
        const r = await api.register('');
        if (out) { out.textContent = r.message || btn.dataset.done; out.className = 'info'; out.hidden = false; }
      } catch (e) {
        if (out) { out.textContent = message(btn, e); out.className = 'alert'; out.hidden = false; }
      }
      btn.disabled = false;
    });
  });
})();
