'use strict';

// 6 single-digit boxes -> hidden "code" field; auto-advance, paste, auto-submit when complete
document.querySelectorAll('[data-otp]').forEach((box) => {
  const inputs = [...box.querySelectorAll('input.digit')];
  const hidden = box.querySelector('input[type=hidden]');
  const form = box.closest('form');
  let sent = false;
  const sync = () => {
    hidden.value = inputs.map((i) => i.value).join('');
    if (hidden.value.length === 6 && !sent) {
      sent = true;
      form.requestSubmit();
    }
  };
  inputs.forEach((inp, i) => {
    inp.addEventListener('input', () => {
      inp.value = inp.value.replace(/\D/g, '').slice(-1);
      if (inp.value && inputs[i + 1]) inputs[i + 1].focus();
      sync();
    });
    inp.addEventListener('keydown', (e) => {
      if (e.key === 'Backspace' && !inp.value && inputs[i - 1]) { inputs[i - 1].focus(); inputs[i - 1].value = ''; sent = false; }
      if (e.key === 'ArrowLeft' && inputs[i - 1]) inputs[i - 1].focus();
      if (e.key === 'ArrowRight' && inputs[i + 1]) inputs[i + 1].focus();
    });
    inp.addEventListener('paste', (e) => {
      const digits = (e.clipboardData.getData('text') || '').replace(/\D/g, '').slice(0, 6);
      if (!digits) return;
      e.preventDefault();
      digits.split('').forEach((d, j) => { if (inputs[j]) inputs[j].value = d; });
      (inputs[digits.length] || inputs[5]).focus();
      sync();
    });
  });
  form.addEventListener('submit', () => { hidden.value = inputs.map((i) => i.value).join(''); });
  if (!document.querySelector('[autofocus]')) inputs[0].focus();
});

// submit buttons: no double submits
document.querySelectorAll('form[data-once]').forEach((form) => {
  form.addEventListener('submit', () => {
    const btn = form.querySelector('[type=submit]');
    if (btn) setTimeout(() => { btn.disabled = true; btn.classList.add('busy'); }, 0);
  });
});

// lock countdown
document.querySelectorAll('[data-countdown]').forEach((el) => {
  let s = Number(el.dataset.countdown) || 0;
  const tick = () => {
    if (s <= 0) { location.reload(); return; }
    el.textContent = String(Math.floor(s / 60)).padStart(2, '0') + ':' + String(s % 60).padStart(2, '0');
    s -= 1;
  };
  tick();
  setInterval(tick, 1000);
});

document.querySelectorAll('[data-copy]').forEach((b) => b.addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText(b.dataset.copy);
    const t = b.textContent;
    b.textContent = b.dataset.copied || 'Copied';
    setTimeout(() => { b.textContent = t; }, 1500);
  } catch { /* clipboard blocked */ }
}));

document.querySelectorAll('[data-back]').forEach((b) => b.addEventListener('click', () => {
  if (history.length > 1) history.back(); else location.href = '/';
}));

// "codes saved" unlocks the continue button
document.querySelectorAll('[data-gate]').forEach((cb) => {
  const target = document.getElementById(cb.dataset.gate);
  const update = () => target.classList.toggle('is-disabled', !cb.checked);
  cb.addEventListener('change', update);
  target.addEventListener('click', (e) => { if (!cb.checked) e.preventDefault(); });
  update();
});

// password strength meter; labels come translated from the page: "min|weak|weak|okay|good|strong"
document.querySelectorAll('[data-strength]').forEach((inp) => {
  const bars = [...document.querySelectorAll('.meter i')];
  const label = document.querySelector('[data-strength-label]');
  const labels = (label.dataset.labels || '').split('|');
  label.textContent = labels[0] || '';
  inp.addEventListener('input', () => {
    const v = inp.value;
    let score = 0;
    if (v.length >= 10) score++;
    if (v.length >= 14) score++;
    if (/[A-Z]/.test(v) && /[a-z]/.test(v)) score++;
    if (/\d/.test(v) && /[^A-Za-z0-9]/.test(v)) score++;
    if (v.length < 10) score = Math.min(score, 1);
    bars.forEach((b, i) => b.classList.toggle('on', i < score));
    label.textContent = v.length < 10 ? labels[0] : labels[score + 1] || '';
  });
});
