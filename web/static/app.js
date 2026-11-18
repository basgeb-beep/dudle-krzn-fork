// Progressive enhancement only: every page works without this file.
(() => {
  'use strict';
  const lang = document.documentElement.lang || 'en';
  document.documentElement.classList.add('js');

  const el = (tag, props = {}, ...kids) => {
    const n = Object.assign(document.createElement(tag), props);
    kids.forEach((k) => n.append(k));
    return n;
  };

  // ---- vote cells: click to cycle no -> yes -> if need be -------------------
  const symbols = { 0: '✕', 1: '✓', 2: '?' };
  document.querySelectorAll('select.vote').forEach((sel) => {
    const btn = el('button', { type: 'button' });
    const render = () => {
      const opt = sel.options[sel.selectedIndex];
      btn.className = 'vote-btn v' + sel.value;
      btn.textContent = symbols[sel.value] || '';
      btn.title = opt.textContent;
      btn.setAttribute('aria-label', (sel.dataset.label ? sel.dataset.label + ': ' : '') + opt.textContent);
    };
    btn.addEventListener('click', () => {
      sel.selectedIndex = (sel.selectedIndex + 1) % sel.options.length;
      render();
    });
    sel.hidden = true; // stays in the form, so its value is still submitted
    sel.after(btn);
    render();
  });

  // ---- date picker -----------------------------------------------------------
  document.querySelectorAll('.datepicker').forEach((dp) => {
    const cal = dp.querySelector('.cal');
    const list = dp.querySelector('.picked');
    dp.querySelector('.nojs-dates')?.remove(); // avoid submitting empty fallback rows
    cal.hidden = false;

    const L = dp.dataset;
    const picked = new Map(); // 'YYYY-MM-DD' -> 'HH:MM' | ''
    (L.initial || '').split(',').filter(Boolean).forEach((s) => {
      const [d, t] = s.split('|');
      if (/^\d{4}-\d{2}-\d{2}$/.test(d)) picked.set(d, t || '');
    });

    const pad = (n) => String(n).padStart(2, '0');
    const iso = (y, m, d) => `${y}-${pad(m + 1)}-${pad(d)}`;
    const parse = (k) => { const [y, m, d] = k.split('-').map(Number); return new Date(y, m - 1, d); };
    const now = new Date();
    const todayIso = iso(now.getFullYear(), now.getMonth(), now.getDate());
    const first = [...picked.keys()].sort()[0];
    let [y, m] = first ? first.split('-').map((n, i) => (i === 1 ? n - 1 : +n)) : [now.getFullYear(), now.getMonth()];

    const fmtMonth = new Intl.DateTimeFormat(lang, { month: 'long', year: 'numeric' });
    const fmtDay = new Intl.DateTimeFormat(lang, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' });
    const fmtDow = new Intl.DateTimeFormat(lang, { weekday: 'short' });

    function drawCal() {
      const nav = (label, glyph, delta) => {
        const b = el('button', { type: 'button', className: 'cal-nav', textContent: glyph, title: label });
        b.setAttribute('aria-label', label);
        b.addEventListener('click', () => { m += delta; if (m < 0) { m = 11; y--; } if (m > 11) { m = 0; y++; } drawCal(); });
        return b;
      };
      const head = el('div', { className: 'cal-head' }, nav(L.prev, '‹', -1),
        el('strong', { textContent: fmtMonth.format(new Date(y, m, 1)) }), nav(L.next, '›', 1));
      const grid = el('div', { className: 'cal-grid' });
      for (let i = 0; i < 7; i++) grid.append(el('span', { className: 'dow', textContent: fmtDow.format(new Date(2024, 0, 1 + i)) })); // 2024-01-01 is a Monday
      const lead = (new Date(y, m, 1).getDay() + 6) % 7;
      for (let i = 0; i < lead; i++) grid.append(el('span'));
      const days = new Date(y, m + 1, 0).getDate();
      for (let d = 1; d <= days; d++) {
        const k = iso(y, m, d);
        const b = el('button', { type: 'button', className: 'day', textContent: d });
        if (k === todayIso) b.classList.add('today');
        if (picked.has(k)) b.classList.add('on');
        b.setAttribute('aria-pressed', picked.has(k));
        b.disabled = k < todayIso && !picked.has(k);
        b.addEventListener('click', () => {
          picked.has(k) ? picked.delete(k) : picked.set(k, '');
          drawCal(); drawList();
        });
        grid.append(b);
      }
      cal.replaceChildren(head, grid);
    }

    function drawList() {
      list.replaceChildren();
      if (!picked.size) { list.append(el('li', { className: 'none', textContent: L.none })); return; }
      [...picked.keys()].sort().forEach((k) => {
        const time = el('input', { type: 'time', name: 'time', value: picked.get(k), title: L.time });
        time.setAttribute('aria-label', L.time);
        time.addEventListener('input', () => picked.set(k, time.value));
        const rm = el('button', { type: 'button', className: 'rm', textContent: '×', title: L.remove });
        rm.setAttribute('aria-label', L.remove + ': ' + fmtDay.format(parse(k)));
        rm.addEventListener('click', () => { picked.delete(k); drawCal(); drawList(); });
        list.append(el('li', {}, el('input', { type: 'hidden', name: 'date', value: k }),
          el('span', { className: 'd', textContent: fmtDay.format(parse(k)) }), time, rm));
      });
    }
    drawCal(); drawList();
  });

  // ---- copy buttons ------------------------------------------------------------
  document.querySelectorAll('[data-copy]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const input = document.querySelector(btn.dataset.copy);
      if (!input) return;
      try { await navigator.clipboard.writeText(input.value); }
      catch { input.select(); document.execCommand('copy'); }
      const old = btn.textContent;
      btn.textContent = btn.dataset.copied || old;
      setTimeout(() => { btn.textContent = old; }, 1600);
    });
  });
  document.querySelectorAll('input[readonly]').forEach((i) => i.addEventListener('focus', () => i.select()));

  // ---- confirm destructive forms -------------------------------------------------
  document.querySelectorAll('form[data-confirm]').forEach((f) => {
    f.addEventListener('submit', (e) => { if (!confirm(f.dataset.confirm)) e.preventDefault(); });
  });

  // ---- remember created polls on this device (localStorage only) ---------------------
  const KEY = 'dudle.polls';
  const load = () => { try { return JSON.parse(localStorage.getItem(KEY)) || []; } catch { return []; } };
  const save = (v) => { try { localStorage.setItem(KEY, JSON.stringify(v.slice(0, 30))); } catch { /* ignore */ } };

  const admin = document.querySelector('[data-admin-url]');
  if (admin) {
    const entry = { id: admin.dataset.adminId, title: admin.dataset.adminTitle, url: new URL(admin.dataset.adminUrl).pathname };
    save([entry, ...load().filter((p) => p.id !== entry.id)]);
  }
  const box = document.getElementById('mypolls');
  if (box) {
    const polls = load();
    if (polls.length) {
      const ul = box.querySelector('ul');
      polls.forEach((p) => ul.append(el('li', {}, el('a', { href: p.url, textContent: p.title }))));
      box.hidden = false;
    }
  }
})();
