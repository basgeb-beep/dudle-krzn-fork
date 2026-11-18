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
      let i = sel.selectedIndex;
      do { i = (i + 1) % sel.options.length; } while (sel.options[i].disabled && i !== sel.selectedIndex);
      sel.selectedIndex = i;
      render();
    });
    sel.hidden = true; // stays in the form, so its value is still submitted
    sel.after(btn);
    render();
  });

  // ---- small screens: answer as a list instead of a wide table row ---------------
  const voteRow = document.querySelector('#vote tr.new, #vote tr.editing');
  if (voteRow) {
    const wrap = document.querySelector('#vote .tablewrap');
    const list = el('ul', { className: 'vote-list' });
    list.setAttribute('aria-label', document.querySelector('#vote').dataset.youranswer || '');
    const moved = []; // [node, original parent]
    const toList = () => {
      const nameCell = voteRow.querySelector('td.name');
      const nameLi = el('li', { className: 'vl-name' });
      [...nameCell.childNodes].forEach((n) => { moved.push([n, nameCell]); nameLi.append(n); });
      list.append(nameLi);
      voteRow.querySelectorAll('td.cell').forEach((td) => {
        const sel = td.querySelector('select.vote');
        const li = el('li', {}, el('span', { className: 'vl-label', textContent: sel ? sel.dataset.label : '' }));
        [...td.childNodes].forEach((n) => { moved.push([n, td]); li.append(n); });
        list.append(li);
      });
      wrap.before(list);
      voteRow.hidden = true;
    };
    const toTable = () => {
      moved.splice(0).forEach(([n, parent]) => parent.append(n));
      list.replaceChildren(); list.remove();
      voteRow.hidden = false;
    };
    const mq = window.matchMedia('(max-width: 640px)');
    const apply = () => { if (mq.matches && !list.isConnected) toList(); else if (!mq.matches && list.isConnected) toTable(); };
    mq.addEventListener('change', apply);
    apply();
  }

  // ---- date picker -----------------------------------------------------------
  document.querySelectorAll('.datepicker').forEach((dp) => {
    const cal = dp.querySelector('.cal');
    const list = dp.querySelector('.picked');
    dp.querySelector('.nojs-dates')?.remove(); // avoid submitting empty fallback rows
    cal.hidden = false;

    const L = dp.dataset;
    const picked = new Map(); // 'YYYY-MM-DD' -> ['HH:MM', ...]; [] = whole day; '' = empty custom field
    (L.initial || '').split(',').filter(Boolean).forEach((s) => {
      const [d, t] = s.split('|');
      if (!/^\d{4}-\d{2}-\d{2}$/.test(d)) return;
      const times = picked.get(d) || [];
      if (t && !times.includes(t)) times.push(t);
      picked.set(d, times);
    });
    const GRID = []; // quick-pick times 08:00 .. 16:00
    for (let h = 8; h <= 16; h++) GRID.push(String(h).padStart(2, '0') + ':00');

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
        b.setAttribute('aria-label', fmtDay.format(new Date(y, m, d)));
        if (k === todayIso) b.classList.add('today');
        if (picked.has(k)) b.classList.add('on');
        b.setAttribute('aria-pressed', picked.has(k));
        b.disabled = k < todayIso && !picked.has(k);
        b.addEventListener('click', () => {
          picked.has(k) ? picked.delete(k) : picked.set(k, []);
          drawCal(); drawList();
        });
        grid.append(b);
      }
      cal.replaceChildren(head, grid);
    }

    const status = el('p', { className: 'sr' });
    status.setAttribute('role', 'status');
    dp.append(status);

    function drawList() {
      status.textContent = (L.count || '').replace('{n}', picked.size);
      list.replaceChildren();
      if (!picked.size) { list.append(el('li', { className: 'none', textContent: L.none })); return; }
      [...picked.keys()].sort().forEach((k) => {
        const times = picked.get(k);
        const pair = (t, visible) => {
          const time = el('input', { type: visible ? 'time' : 'hidden', name: 'time', value: t });
          return { time, slot: el('span', { className: visible ? 'slot' : 'slot-h' }, el('input', { type: 'hidden', name: 'date', value: k }), time) };
        };
        const chips = el('div', { className: 'chips', role: 'group' });
        chips.setAttribute('aria-label', L.time);
        const out = el('div', { className: 'slots' });
        GRID.forEach((t) => {
          const on = times.includes(t);
          const c = el('button', { type: 'button', className: 'chip' + (on ? ' on' : ''), textContent: t });
          c.setAttribute('aria-pressed', on);
          c.setAttribute('aria-label', t + ', ' + fmtDay.format(parse(k)));
          c.addEventListener('click', () => {
            const i = times.indexOf(t);
            if (i >= 0) times.splice(i, 1); else times.push(t);
            drawList();
          });
          chips.append(c);
          if (on) out.append(pair(t, false).slot);
        });
        let focusLast = null;
        times.forEach((t, i) => {
          if (GRID.includes(t)) return;
          const p = pair(t, true);
          p.time.setAttribute('aria-label', L.time);
          p.time.addEventListener('input', () => { times[i] = p.time.value; });
          const rmt = el('button', { type: 'button', className: 'rm', title: L.remove }, el('span', { textContent: '×', ariaHidden: 'true' }));
          rmt.setAttribute('aria-label', L.remove + ': ' + (t || L.time));
          rmt.addEventListener('click', () => { times.splice(i, 1); drawList(); });
          p.slot.append(rmt);
          out.append(p.slot);
          focusLast = p.time;
        });
        if (!times.length) out.append(pair('', false).slot); // whole day
        const add = el('button', { type: 'button', className: 'add-time', textContent: '+ ' + L.addtime });
        add.addEventListener('click', () => {
          times.push(''); drawList();
          const row = [...list.querySelectorAll('li[data-day]')].find((x) => x.dataset.day === k);
          const ins = row && row.querySelectorAll('.slot input[type=time]');
          if (ins && ins.length) ins[ins.length - 1].focus();
        });
        const extra = el('div', { className: 'dayactions' }, add);
        if (picked.size > 1 && times.some(Boolean)) {
          const copy = el('button', { type: 'button', className: 'add-time', textContent: '⇉ ' + L.copyall });
          copy.addEventListener('click', () => {
            const src = times.filter(Boolean);
            picked.forEach((_, d) => { if (d !== k) picked.set(d, [...src]); });
            drawList();
            status.textContent = L.copyall + ' ✓';
          });
          extra.append(copy);
        }
        const rm = el('button', { type: 'button', className: 'rm', title: L.remove }, el('span', { textContent: '×', ariaHidden: 'true' }));
        rm.setAttribute('aria-label', L.remove + ': ' + fmtDay.format(parse(k)));
        rm.addEventListener('click', () => { picked.delete(k); drawCal(); drawList(); });
        const li = el('li', {}, el('span', { className: 'd', textContent: fmtDay.format(parse(k)) }),
          el('div', { className: 'times' }, chips, out, extra), rm);
        li.dataset.day = k;
        list.append(li);
      });
    }

    // Drop empty slots when the same day also has concrete times.
    const form = dp.closest('form');
    if (form) form.addEventListener('submit', () => {
      list.querySelectorAll('li[data-day]').forEach((li) => {
        const slots = [...li.querySelectorAll('.slot, .slot-h')];
        if (!slots.some((sl) => sl.querySelector('input[name=time]').value)) return;
        slots.forEach((sl) => {
          const t = sl.querySelector('input[name=time]');
          if (!t.value) { t.disabled = true; sl.querySelector('input[name=date]').disabled = true; }
        });
      });
    });
    drawCal(); drawList();
  });

  // ---- duration chips: free entry only for "custom", hide times for "all day"
  document.querySelectorAll('.dur').forEach((box) => {
    const custom = box.querySelector('.dur-custom');
    const dp = box.closest('form')?.querySelector('.datepicker');
    const sync = () => {
      const v = box.querySelector('input[name=duration]:checked')?.value;
      if (custom) custom.hidden = v !== 'custom';
      if (dp) dp.classList.toggle('allday', v === '1440');
    };
    box.addEventListener('change', sync);
    sync();
  });

  // ---- create form: reveal one step after the other ----------------------------
  const create = document.querySelector('form.create');
  if (create) {
    const title = create.querySelector('#title');
    const choices = create.querySelector('#choices');
    const show = (name) => create.querySelectorAll(`[data-step="${name}"].step-hidden`).forEach((e) => {
      e.classList.remove('step-hidden');
      e.classList.add('step-in');
    });
    if (!title.value.trim()) { // fresh form: start with title and description only
      create.querySelectorAll('input[name=kind]').forEach((k) => { k.checked = false; });
      create.querySelectorAll('[data-step]').forEach((e) => e.classList.add('step-hidden'));
    }
    const update = () => {
      if (title.value.trim()) show('kind');
      const kind = create.querySelector('input[name=kind]:checked')?.value;
      if (kind === 'date') {
        show('dates');
        if (create.querySelector('.picked li[data-day]')) { show('duration'); show('finish'); }
      } else if (kind === 'text') {
        show('choices');
        if (choices.value.trim()) show('finish');
      }
    };
    create.addEventListener('input', update);
    create.addEventListener('change', update);
    create.addEventListener('click', () => setTimeout(update));
    create.addEventListener('submit', (e) => {
      if (create.querySelector('[data-step="finish"].step-hidden')) { e.preventDefault(); update(); }
    });
    update();
  }

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
