import { formatClock, formatPct, formatPrice, pctClass, priceTone } from './format.js';

const HOLD_MS = 400;
const HOLD_SLOP = 10;
const SCROLL_EDGE = 72;

const COLORS = [
  { value: '#c6a15b', label: 'Brass' },
  { value: '#8eae8a', label: 'Sage' },
  { value: '#c56b52', label: 'Clay' },
  { value: '#e8e0cc', label: 'Sand' },
  { value: '#8aa0ae', label: 'Slate' },
  { value: '#a35d6a', label: 'Wine' },
  { value: '#d4895c', label: 'Copper' },
  { value: '#a3ae62', label: 'Olive' },
  { value: '#5eaea4', label: 'Tide' },
  { value: '#6e92d4', label: 'Dusk' },
  { value: '#b196cc', label: 'Iris' },
  { value: '#d08a9c', label: 'Rose' },
];

const ICONS = [
  { id: 'mark', label: 'Mark', svg: svg('<circle cx="8" cy="8" r="2.2" fill="currentColor" stroke="none"></circle>') },
  { id: 'eye', label: 'Eye', svg: svg('<path d="M1.5 8S4 4.2 8 4.2 14.5 8 14.5 8 12 11.8 8 11.8 1.5 8 1.5 8Z"></path><circle cx="8" cy="8" r="1.5"></circle>') },
  { id: 'chart', label: 'Chart', svg: svg('<path d="M2 13h12"></path><path d="M3.5 10.5 6.2 7.4l2.3 1.7 3.9-4.8"></path>') },
  { id: 'coin', label: 'Coin', svg: svg('<circle cx="8" cy="8" r="5"></circle><path d="M8 4.8v6.4"></path>') },
  { id: 'bolt', label: 'Bolt', svg: svg('<path d="M9.2 1.8 4.2 8.6h3.1L6.4 14.2 12 7.2H8.8L9.2 1.8Z"></path>') },
  { id: 'layers', label: 'Layers', svg: svg('<path d="M8 2.4 2.4 5.8 8 9.2l5.6-3.4L8 2.4Z"></path><path d="M2.4 8.6 8 12l5.6-3.4"></path>') },
  { id: 'crypto', label: 'Crypto', svg: svg('<path fill="currentColor" fill-rule="evenodd" stroke="none" d="M3.8 1.3H5.2V3.2H8.2A2.2 2.2 0 0 1 8.2 7.6H5.2V8.4H8.8A2.4 2.4 0 0 1 8.8 13.2H5.2V14.7H3.8ZM5.9 1.3H7.3V14.7H5.9ZM7.6 4.5H8.2A1.05 1.05 0 0 1 8.2 6.6H7.6ZM7.6 9.6H8.6A1.2 1.2 0 0 1 8.6 12H7.6Z"></path>') },
  { id: 'bank', label: 'Bank', svg: svg('<path d="M1.6 6.4 8 2.2 14.4 6.4"></path><path d="M3.2 6.4h9.6"></path><path d="M4.2 6.4V12"></path><path d="M8 6.4V12"></path><path d="M11.8 6.4V12"></path><path d="M2.4 12.4h11.2"></path>') },
  { id: 'chip', label: 'Chip', svg: svg('<rect x="4.6" y="4.6" width="6.8" height="6.8"></rect><path d="M6.6 4.6V2"></path><path d="M9.4 4.6V2"></path><path d="M6.6 14v-2.6"></path><path d="M9.4 14v-2.6"></path><path d="M4.6 6.6H2"></path><path d="M4.6 9.4H2"></path><path d="M14 6.6h-2.6"></path><path d="M14 9.4h-2.6"></path>') },
  { id: 'memory', label: 'Memory', svg: svg('<path d="M2.2 4h11.6v7.2H2.2Z"></path><path d="M4.8 4v2.4"></path><path d="M8 4v2.4"></path><path d="M11.2 4v2.4"></path><path d="M5 11.2v1.8"></path><path d="M8 11.2v1.8"></path><path d="M11 11.2v1.8"></path>') },
  { id: 'ai', label: 'AI', svg: svg('<circle cx="8" cy="8" r="1.4" fill="currentColor" stroke="none"></circle><circle cx="8" cy="2.6" r="1.15" fill="currentColor" stroke="none"></circle><circle cx="2.8" cy="12.6" r="1.15" fill="currentColor" stroke="none"></circle><circle cx="13.2" cy="12.6" r="1.15" fill="currentColor" stroke="none"></circle><path d="M8 6.6V3.8"></path><path d="M6.8 8.7 3.7 11.6"></path><path d="M9.2 8.7 12.3 11.6"></path>') },
  { id: 'energy', label: 'Energy', svg: svg('<path d="M8 2.2c1.6 2.2 3.4 4 3.4 6.3a3.4 3.4 0 0 1-6.8 0c0-2.3 1.8-4.1 3.4-6.3Z"></path>') },
  { id: 'pulse', label: 'Pulse', svg: svg('<path d="M1.2 8h3l1.7-4.2 2.6 7.6L10.2 8H14.8"></path>') },
  { id: 'cloud', label: 'Cloud', svg: svg('<path d="M4.2 12.2h7a2.3 2.3 0 0 0 0-4.6 3.1 3.1 0 0 0-5.8-.4 2.6 2.6 0 0 0-1.2 5Z"></path>') },
  { id: 'factory', label: 'Factory', svg: svg('<path d="M1.8 13.4V6.6l3.2 2.8V6.6l3.2 2.8V3.2H14v10.2"></path><path d="M1.8 13.4H14"></path>') },
  { id: 'house', label: 'House', svg: svg('<path d="M2 7.4 8 2.2l6 5.2"></path><path d="M3.6 7.2v6.2h8.8V7.2"></path><path d="M6.9 13.4V9.6h2.2v3.8"></path>') },
  { id: 'shield', label: 'Shield', svg: svg('<path d="M8 1.8 13.2 4v4.4c0 3.2-2.2 5-5.2 5.8-3-.8-5.2-2.6-5.2-5.8V4L8 1.8Z"></path>') },
];

class SwBoard extends HTMLElement {
  connectedCallback() {
    this.sheetMode = 'create';
    this.draft = { color: COLORS[0].value, icon: 'mark' };
    this.sheetOpen = false;
    this.openChanges = new Set();
    this.closedFolds = loadFolds();
    this.innerHTML = `
      <header class="top">
        <div class="top-line">
          <div class="brand">Stockwatcher</div>
          <div class="top-actions">
            <button type="button" class="bar-btn" data-act="edit" aria-pressed="false">Edit</button>
            <button type="button" class="bar-btn" data-act="alerts">Alerts</button>
          </div>
        </div>
        <div class="specials" role="tablist"></div>
      </header>
      <div class="groups" role="tablist"></div>
      <div class="board">
        <div class="edit-slot"></div>
        <div class="cols"></div>
        <div class="rows"></div>
        <button type="button" class="add-row" data-act="search" aria-label="Add a symbol">+</button>
      </div>
      ${groupSheet()}`;
    this.ateClick = false;
    this.onPointerMove = (event) => this.trackDrag(event);
    this.onPointerUp = (event) => this.finishDrag(true, event);
    this.onPointerCancel = (event) => this.finishDrag(false, event);
    this.onContextMenu = (event) => {
      if (this.drag) event.preventDefault();
    };
    this.blockTouch = (event) => {
      if (this.drag?.active) event.preventDefault();
    };
    this.addEventListener('pointerdown', () => {
      if (!this.drag?.active) this.ateClick = false;
    });
    this.addEventListener('click', (event) => this.onClick(event));
    this.addEventListener('submit', (event) => this.onSubmit(event));
    this.querySelector('.rows').addEventListener('pointerdown', (event) => this.armDrag(event));
    this.onKey = (event) => {
      if (event.key === 'Escape' && this.sheetOpen) this.closeGroupSheet();
    };
    window.addEventListener('keydown', this.onKey);
  }

  disconnectedCallback() {
    window.removeEventListener('keydown', this.onKey);
    this.finishDrag(false);
  }

  setState({ board, groupId, flashes, editing, armed }) {
    if (this.drag?.active) return;
    if (this.drag) this.finishDrag(false);
    const y = window.scrollY;
    this.board = board;
    this.groupId = groupId;
    this.view = groupId === 'all' ? 'all' : '';
    this.editing = editing;
    this.flashes = flashes || new Map();
    const groups = board?.groups || [];
    const group = this.view === 'all'
      ? null
      : groups.find((g) => g.id === groupId) || groups.find((g) => !g.synthetic) || null;
    this.group = group;
    this.renderSpecials(board);
    this.renderGroups(board);
    this.renderEdit(group);
    this.renderBody();
    const alerts = this.querySelector('[data-act="alerts"]');
    alerts.innerHTML = armed ? 'Alerts<span class="dot" aria-label="armed alerts"></span>' : 'Alerts';
    const edit = this.querySelector('[data-act="edit"]');
    edit.classList.toggle('is-on', editing);
    edit.setAttribute('aria-pressed', editing ? 'true' : 'false');
    this.querySelector('.add-row').hidden = this.view === 'all' || !!group?.synthetic;
    window.scrollTo(0, y);
  }

  renderSpecials(board) {
    const host = this.querySelector('.specials');
    const items = [{ id: 'all', name: 'All' }];
    for (const group of board?.groups || []) {
      if (group.kind === 'notable' || group.kind === 'owned') items.push({ id: String(group.id), name: group.name });
    }
    const current = String(this.groupId);
    host.innerHTML = items.map((item) => {
      const on = current === item.id;
      return `<button type="button" class="text-btn${on ? ' is-on' : ''}" role="tab" data-view="${item.id}" aria-selected="${on}">${escapeHTML(item.name)}</button>`;
    }).join('');
  }

  renderGroups(board) {
    const host = this.querySelector('.groups');
    const groups = (board?.groups || []).filter((g) => !g.synthetic);
    const chips = groups.map((g) => {
      const on = g.id === this.group?.id;
      const color = chipColor(g.color);
      return `<button type="button" class="chip${on ? ' is-on' : ''}" role="tab" data-group="${g.id}" aria-selected="${on}" style="--chip:${color}"><span class="chip-icon">${iconSVG(g.icon)}</span><span class="chip-name">${escapeHTML(g.name)}</span></button>`;
    }).join('');
    const add = '<button type="button" class="chip chip-add" data-act="new-group" aria-label="New group">+</button>';
    host.innerHTML = chips + add;
  }

  renderEdit(group) {
    const slot = this.querySelector('.edit-slot');
    if (!this.editing || !group || group.synthetic) {
      slot.innerHTML = '';
      return;
    }
    slot.innerHTML = `
      <div class="edit-bar">
        <div class="edit-moves">
          <button type="button" class="mini" data-act="edit-group">Edit group</button>
          <button type="button" class="mini" data-move-group="-1">Left</button>
          <button type="button" class="mini" data-move-group="1">Right</button>
          <button type="button" class="mini danger" data-delete-group>Delete group</button>
        </div>
      </div>`;
  }

  renderCols(group) {
    const cols = this.querySelector('.cols');
    const rows = group?.rows || [];
    if (!rows.length) {
      cols.innerHTML = '';
      return;
    }
    const times = rows.map((r) => r.as_of).filter(Boolean);
    const oldest = times.length ? Math.min(...times) : 0;
    cols.innerHTML = oldest ? `<span class="asof">${escapeHTML(formatClock(oldest))}</span>` : '';
  }

  renderBody() {
    if (this.view === 'all') {
      this.querySelector('.cols').innerHTML = '';
      this.renderAll(this.board);
      return;
    }
    this.renderCols(this.group);
    this.renderRows(this.group);
  }

  renderAll(board) {
    const host = this.querySelector('.rows');
    const groups = (board?.groups || []).filter((g) => !g.synthetic);
    if (!groups.length) {
      host.innerHTML = '<p class="empty">Nothing in this group.</p>';
      return;
    }
    host.innerHTML = groups.map((group) => {
      const shut = this.closedFolds.has(group.id);
      const rows = group.rows || [];
      let body = '';
      if (!shut) {
        body = rows.length
          ? rows.map((row, i) => this.cardHTML(row, i, rows, true)).join('')
          : '<p class="empty fold-empty">Nothing in this group.</p>';
      }
      return `<section class="fold${shut ? ' is-shut' : ''}" style="--chip:${chipColor(group.color)}">
        <button type="button" class="fold-h" data-fold="${group.id}" aria-expanded="${shut ? 'false' : 'true'}">
          <span class="chip-icon">${iconSVG(group.icon)}</span>
          <span class="fold-name">${escapeHTML(group.name)}</span>
          <span class="fold-mark" aria-hidden="true"></span>
        </button>
        ${body}
      </section>`;
    }).join('');
  }

  renderRows(group) {
    const host = this.querySelector('.rows');
    const rows = group?.rows || [];
    if (!rows.length) {
      host.innerHTML = `<p class="empty">${escapeHTML(emptyCopy(group))}</p>`;
      return;
    }
    const fixed = !!group?.synthetic;
    host.innerHTML = rows.map((row, i) => this.cardHTML(row, i, rows, fixed)).join('');
  }

  cardHTML(row, i, rows, fixed) {
    const flash = this.flashes.get(row.id);
    const open = this.openChanges.has(row.id);
    const expanded = open ? 'true' : 'false';
    const mark = row.owned ? `<span class="owned-mark" role="img" aria-label="Owned">${starSVG()}</span>` : '';
    const name = `<button type="button" class="card-name" data-expand="${row.id}" aria-expanded="${expanded}">
          <span class="ident">
            <span class="sym">${escapeHTML(row.display)}${mark}</span>
            <span class="sub">${escapeHTML(row.name || '')}</span>
          </span>
        </button>`;
    const price = `<button type="button" class="card-px" data-expand="${row.id}" aria-expanded="${expanded}">
          <span class="px num ${priceTone(row.change_d)}">${formatPrice(row.price)}</span>
          <span class="day">${extLine(row)}${chg(row.change_d)}</span>
        </button>`;
    const more = open ? `<div class="more">${ownButton(row)}${rangeMeter(row)}<div class="more-chgs">
          <span class="more-cell"><span class="chg-h">1H</span>${chg(row.change_1h)}</span>
          <span class="more-cell"><span class="chg-h">4H</span>${chg(row.change_4h)}</span>
          <span class="more-cell"><span class="chg-h">W</span>${chg(row.change_w)}</span>
          <span class="more-cell"><span class="chg-h">M</span>${chg(row.change_m)}</span>
        </div></div>` : '';
    const moves = this.editing && !fixed ? `<span class="row-edit">
          <button type="button" class="mini" data-move-symbol="${row.id}" data-dir="-1" ${i === 0 ? 'disabled' : ''}>Up</button>
          <button type="button" class="mini" data-move-symbol="${row.id}" data-dir="1" ${i === rows.length - 1 ? 'disabled' : ''}>Down</button>
        </span>` : '';
    const remove = fixed ? '' : `<button type="button" class="row-x" data-remove="${row.id}" aria-label="Remove ${escapeAttr(row.display)}">×</button>`;
    return `<div class="row${flash ? ' flash-' + flash : ''}${open ? ' is-open' : ''}${fixed ? ' is-auto' : ''}" data-symbol="${row.id}">${name}${sparkHit(row, this.editing)}${price}${remove}${more}${moves}</div>`;
  }

  openGroupSheet(mode) {
    this.sheetMode = mode;
    const group = this.group;
    const editing = mode === 'edit' && group;
    this.draft = {
      color: chipColor(editing ? group.color : nextColor((this.board?.groups || []).filter((g) => !g.synthetic))),
      icon: iconId(editing ? group.icon : 'mark'),
    };
    this.querySelector('#group-sheet-title').textContent = editing ? 'Edit group' : 'New group';
    this.querySelector('[data-sheet-submit]').textContent = editing ? 'Save' : 'Create';
    const input = this.querySelector('[name="name"]');
    input.value = editing ? group.name : '';
    this.syncPicks();
    this.sheetOpen = true;
    this.querySelector('.group-mask').hidden = false;
    input.focus();
  }

  closeGroupSheet() {
    this.sheetOpen = false;
    const mask = this.querySelector('.group-mask');
    if (mask) mask.hidden = true;
  }

  syncPicks() {
    for (const btn of this.querySelectorAll('[data-color]')) {
      btn.setAttribute('aria-pressed', btn.dataset.color === this.draft.color ? 'true' : 'false');
    }
    for (const btn of this.querySelectorAll('[data-icon]')) {
      btn.setAttribute('aria-pressed', btn.dataset.icon === this.draft.icon ? 'true' : 'false');
    }
  }

  armDrag(event) {
    if (this.drag || this.view === 'all' || this.group?.synthetic) return;
    if (event.button > 0) return;
    const host = this.querySelector('.rows');
    const row = event.target.closest('.row');
    if (!row || !host?.contains(row)) return;
    if (event.target.closest('.row-x, .row-edit, .own')) return;
    this.ateClick = false;
    this.drag = {
      pointerId: event.pointerId,
      row,
      x: event.clientX,
      y: event.clientY,
      active: false,
      timer: setTimeout(() => this.beginDrag(), HOLD_MS),
    };
    window.addEventListener('pointermove', this.onPointerMove);
    window.addEventListener('pointerup', this.onPointerUp);
    window.addEventListener('pointercancel', this.onPointerCancel);
    document.addEventListener('contextmenu', this.onContextMenu, true);
  }

  trackDrag(event) {
    const drag = this.drag;
    if (!drag || event.pointerId !== drag.pointerId) return;
    if (!drag.active) {
      const dx = event.clientX - drag.x;
      const dy = event.clientY - drag.y;
      if (dx * dx + dy * dy > HOLD_SLOP * HOLD_SLOP) this.finishDrag(false, event);
      return;
    }
    if (event.cancelable) event.preventDefault();
    this.placeDrag(event.clientY);
  }

  beginDrag() {
    const drag = this.drag;
    if (!drag || drag.active) return;
    const row = drag.row;
    const host = this.querySelector('.rows');
    if (!row.isConnected || !host) {
      this.finishDrag(false);
      return;
    }
    const rows = [...host.querySelectorAll(':scope > .row')];
    if (!rows.includes(row) || rows.length < 2) {
      this.finishDrag(false);
      return;
    }
    const symbolId = Number(row.dataset.symbol);
    this.openChanges.delete(symbolId);
    row.classList.remove('is-open');
    row.querySelector('.more')?.remove();
    for (const button of row.querySelectorAll('[aria-expanded="true"]')) {
      button.setAttribute('aria-expanded', 'false');
    }
    drag.active = true;
    this.ateClick = true;
    drag.rows = rows;
    drag.from = rows.indexOf(row);
    drag.target = drag.from;
    drag.heights = rows.map((item) => item.getBoundingClientRect().height);
    drag.viewportTops = rows.map((item) => item.getBoundingClientRect().top);
    drag.scrollY = window.scrollY;
    const rect = row.getBoundingClientRect();
    drag.grab = Math.min(Math.max(drag.y - rect.top, 0), rect.height);
    drag.pointerY = drag.y;
    host.classList.add('is-sorting');
    row.classList.add('is-lifted');
    document.body.classList.add('sw-dragging');
    try { row.setPointerCapture(drag.pointerId); } catch { /* pointer already released */ }
    document.addEventListener('touchmove', this.blockTouch, { passive: false });
    navigator.vibrate?.(10);
    this.emit({ type: 'drag-lock', on: true });
    this.placeDrag(drag.y);
    this.watchScroll();
  }

  placeDrag(clientY) {
    const drag = this.drag;
    if (!drag?.active) return;
    drag.pointerY = clientY;
    const scrollDelta = window.scrollY - drag.scrollY;
    const count = drag.rows.length;
    drag.target = dropIndex(drag, clientY, scrollDelta);
    const order = movedOrder(drag.from, drag.target, count);
    const pos = new Array(count);
    let y = drag.viewportTops[0] - scrollDelta;
    for (const index of order) {
      pos[index] = y;
      y += drag.heights[index];
    }
    for (let index = 0; index < count; index++) {
      const item = drag.rows[index];
      const layoutTop = drag.viewportTops[index] - scrollDelta;
      if (index === drag.from) {
        item.style.transform = `translateY(${Math.round(clientY - drag.grab - layoutTop)}px)`;
      } else {
        const shift = Math.round(pos[index] - layoutTop);
        item.style.transform = shift ? `translateY(${shift}px)` : '';
      }
    }
  }

  watchScroll() {
    const step = () => {
      const drag = this.drag;
      if (!drag?.active) return;
      const y = drag.pointerY;
      const edge = SCROLL_EDGE;
      const max = 16;
      let delta = 0;
      if (y < edge) delta = -Math.ceil((1 - y / edge) * max);
      else if (y > window.innerHeight - edge) delta = Math.ceil((1 - (window.innerHeight - y) / edge) * max);
      if (delta) {
        window.scrollBy(0, delta);
        this.placeDrag(y);
      }
      drag.raf = requestAnimationFrame(step);
    };
    this.drag.raf = requestAnimationFrame(step);
  }

  finishDrag(commit, event) {
    const drag = this.drag;
    if (!drag) return;
    if (event && event.pointerId !== drag.pointerId) return;
    clearTimeout(drag.timer);
    if (drag.raf) cancelAnimationFrame(drag.raf);
    window.removeEventListener('pointermove', this.onPointerMove);
    window.removeEventListener('pointerup', this.onPointerUp);
    window.removeEventListener('pointercancel', this.onPointerCancel);
    document.removeEventListener('touchmove', this.blockTouch);
    document.removeEventListener('contextmenu', this.onContextMenu, true);
    document.body.classList.remove('sw-dragging');
    let changed = false;
    let ids = null;
    if (drag.active) {
      try { drag.row.releasePointerCapture(drag.pointerId); } catch { /* pointer already released */ }
      const settled = this.settleRows(commit);
      changed = settled.changed;
      ids = settled.ids;
      this.emit({ type: 'drag-lock', on: false });
      this.ateClick = true;
      clearTimeout(this.clickTimer);
      this.clickTimer = setTimeout(() => { this.ateClick = false; }, 500);
    }
    this.drag = null;
    if (changed && ids) this.emit({ type: 'reorder-symbols', ids });
  }

  settleRows(commit) {
    const drag = this.drag;
    const host = this.querySelector('.rows');
    host?.classList.remove('is-sorting');
    const rows = drag.rows || [];
    const changed = !!(commit && drag.target != null && drag.target !== drag.from);
    const order = changed ? movedOrder(drag.from, drag.target, rows.length) : rows.map((_, index) => index);
    const ids = [];
    for (const index of order) {
      const row = rows[index];
      if (!row) continue;
      row.style.transform = '';
      row.classList.remove('is-lifted');
      if (host && row.isConnected) host.appendChild(row);
      const id = Number(row.dataset.symbol);
      if (Number.isFinite(id)) ids.push(id);
    }
    if (this.editing && host) {
      const ordered = [...host.querySelectorAll(':scope > .row')];
      ordered.forEach((row, index) => {
        const up = row.querySelector('[data-dir="-1"]');
        const down = row.querySelector('[data-dir="1"]');
        if (up) up.disabled = index === 0;
        if (down) down.disabled = index === ordered.length - 1;
      });
    }
    return { ids, changed: changed && ids.length === rows.length };
  }

  onClick(event) {
    if (this.ateClick) {
      this.ateClick = false;
      event.preventDefault();
      event.stopPropagation();
      return;
    }
    if (event.target === this.querySelector('.group-mask')) {
      this.closeGroupSheet();
      return;
    }
    const color = event.target.closest('[data-color]');
    if (color) {
      this.draft.color = color.dataset.color;
      this.syncPicks();
      return;
    }
    const icon = event.target.closest('[data-icon]');
    if (icon) {
      this.draft.icon = icon.dataset.icon;
      this.syncPicks();
      return;
    }
    const view = event.target.closest('[data-view]');
    if (view) {
      const raw = view.dataset.view;
      this.emit({ type: 'group', id: raw === 'all' ? 'all' : Number(raw) });
      return;
    }
    const group = event.target.closest('[data-group]');
    if (group) {
      this.emit({ type: 'group', id: Number(group.dataset.group) });
      return;
    }
    const act = event.target.closest('[data-act]');
    if (act) {
      if (act.dataset.act === 'new-group') {
        this.openGroupSheet('create');
        return;
      }
      if (act.dataset.act === 'edit-group') {
        this.openGroupSheet('edit');
        return;
      }
      if (act.dataset.act === 'close-group') {
        this.closeGroupSheet();
        return;
      }
      this.emit({ type: act.dataset.act });
      return;
    }
    const move = event.target.closest('[data-move-symbol]');
    if (move) {
      this.emit({ type: 'move-symbol', symbolId: Number(move.dataset.moveSymbol), dir: Number(move.dataset.dir) });
      return;
    }
    const remove = event.target.closest('[data-remove]');
    if (remove) {
      this.emit({ type: 'remove-symbol', symbolId: Number(remove.dataset.remove) });
      return;
    }
    const gmove = event.target.closest('[data-move-group]');
    if (gmove) {
      this.emit({ type: 'move-group', dir: Number(gmove.dataset.moveGroup) });
      return;
    }
    if (event.target.closest('[data-delete-group]')) {
      this.emit({ type: 'delete-group' });
      return;
    }
    const own = event.target.closest('[data-own]');
    if (own) {
      this.emit({
        type: 'toggle-owned',
        symbolId: Number(own.dataset.own),
        owned: own.getAttribute('aria-pressed') !== 'true',
      });
      return;
    }
    const fold = event.target.closest('[data-fold]');
    if (fold) {
      const id = Number(fold.dataset.fold);
      if (this.closedFolds.has(id)) this.closedFolds.delete(id);
      else this.closedFolds.add(id);
      saveFolds(this.closedFolds);
      this.renderAll(this.board);
      return;
    }
    const open = event.target.closest('[data-open]');
    if (open && !this.editing) {
      this.emit({ type: 'open', id: Number(open.dataset.open) });
      return;
    }
    const expand = event.target.closest('[data-expand]');
    if (expand) {
      const id = Number(expand.dataset.expand);
      if (this.openChanges.has(id)) this.openChanges.delete(id);
      else this.openChanges.add(id);
      this.renderBody();
    }
  }

  onSubmit(event) {
    const form = event.target.closest('[data-group-form]');
    if (!form) return;
    event.preventDefault();
    const name = String(new FormData(form).get('name') || '').trim();
    if (!name) return;
    this.emit({
      type: this.sheetMode === 'edit' ? 'rename-group' : 'create-group',
      name,
      color: this.draft.color,
      icon: this.draft.icon,
    });
  }

  emit(detail) {
    this.dispatchEvent(new CustomEvent('sw-action', { bubbles: true, detail }));
  }
}

function groupSheet() {
  const swatches = COLORS.map((c) => `<button type="button" class="swatch" data-color="${c.value}" style="background:${c.value}" aria-label="${c.label}" aria-pressed="false"></button>`).join('');
  const icons = ICONS.map((ic) => `<button type="button" class="icon-btn" data-icon="${ic.id}" aria-label="${ic.label}" aria-pressed="false">${ic.svg}</button>`).join('');
  return `
    <div class="group-mask" hidden>
      <div class="search-sheet" role="dialog" aria-labelledby="group-sheet-title">
        <div class="search-head">
          <div class="sheet-title" id="group-sheet-title">New group</div>
          <button type="button" class="text-btn" data-act="close-group">Close</button>
        </div>
        <form data-group-form class="group-form">
          <label class="field">Name
            <input name="name" maxlength="40" placeholder="Group" autocomplete="off" enterkeyhint="done" required>
          </label>
          <div>
            <div class="pick-label" id="group-colors">Color</div>
            <div class="swatches" role="group" aria-labelledby="group-colors">${swatches}</div>
          </div>
          <div>
            <div class="pick-label" id="group-icons">Icon</div>
            <div class="icon-row" role="group" aria-labelledby="group-icons">${icons}</div>
          </div>
          <button type="submit" class="btn" data-sheet-submit>Create</button>
        </form>
      </div>
    </div>`;
}

const FOLD_KEY = 'sw.all.closed';

function loadFolds() {
  try {
    const raw = JSON.parse(localStorage.getItem(FOLD_KEY) || '[]');
    if (!Array.isArray(raw)) return new Set();
    return new Set(raw.map(Number).filter((n) => Number.isFinite(n)));
  } catch {
    return new Set();
  }
}

function saveFolds(closed) {
  localStorage.setItem(FOLD_KEY, JSON.stringify([...closed]));
}

function emptyCopy(group) {
  if (group?.kind === 'owned') return 'Nothing owned.';
  if (group?.synthetic) return 'No unusual moves.';
  return 'Nothing in this group.';
}

function starSVG() {
  return '<svg viewBox="0 0 16 16" aria-hidden="true"><path fill="currentColor" stroke="none" d="M8 1.6 9.7 6.1h4.7L11.1 8.9l1.2 4.5L8 10.8 3.7 13.4 4.9 8.9 1.6 6.1h4.7Z"></path></svg>';
}

function ownButton(row) {
  const on = !!row.owned;
  return `<button type="button" class="own${on ? ' is-on' : ''}" data-own="${row.id}" aria-pressed="${on ? 'true' : 'false'}">${starSVG()}<span>Owned</span></button>`;
}

function svg(body) {
  return `<svg viewBox="0 0 16 16" aria-hidden="true">${body}</svg>`;
}

function iconId(id) {
  return ICONS.some((ic) => ic.id === id) ? id : 'mark';
}

function iconSVG(id) {
  return ICONS.find((ic) => ic.id === iconId(id)).svg;
}

function chipColor(value) {
  return COLORS.some((c) => c.value === String(value || '').toLowerCase()) ? String(value).toLowerCase() : COLORS[0].value;
}

function nextColor(groups) {
  const counts = new Map(COLORS.map((c) => [c.value, 0]));
  for (const group of groups || []) {
    const color = chipColor(group.color);
    counts.set(color, (counts.get(color) || 0) + 1);
  }
  let pick = COLORS[0].value;
  let least = Infinity;
  for (const color of COLORS) {
    const n = counts.get(color.value) || 0;
    if (n < least) {
      least = n;
      pick = color.value;
    }
  }
  return pick;
}

function chg(n) {
  return `<span class="chg ${pctClass(n)}">${formatPct(n)}</span>`;
}

function extLine(row) {
  const label = extLabel(row.ext_session);
  if (!label || row.ext_pct == null) return '';
  return `<span class="day-ext"><span class="chg-h">${label}</span>${chg(row.ext_pct)}</span>`;
}

function extLabel(session) {
  if (session === 'pre') return 'Pre';
  if (session === 'post') return 'After';
  return '';
}

function sparkHit(row, editing) {
  const inner = sparkline(row.sparkline, row.change_d);
  if (editing) return `<span class="spark-hit">${inner}</span>`;
  return `<button type="button" class="spark-hit" data-open="${row.id}" aria-label="Chart for ${escapeAttr(row.display)}">${inner}</button>`;
}

function rangeMeter(row) {
  const low = row.range_low;
  const high = row.range_high;
  if (low == null || high == null || !(high > low)) return '';
  const mid = (Number(low) + Number(high)) / 2;
  if (!(mid > 0)) return '';
  const expected = (Number(high) / mid - 1) * 100;
  if (!(expected > 0)) return '';
  const day = Number(row.change_d);
  const price = Number(row.price);
  const today = Number.isFinite(day) ? day : (Number.isFinite(price) ? (price / mid - 1) * 100 : NaN);
  if (!Number.isFinite(today)) return '';
  const outside = Math.abs(today) > expected;
  // A tenth past the day's move keeps the pointer off the cap. A -2% day draws ±2.2%.
  const scale = outside ? Math.abs(today) * 1.1 : expected;
  const at = (v) => ((v + scale) / (2 * scale)) * 100;
  const bounds = outside
    ? `<span class="range-tick" style="left:${at(-expected)}%"></span><span class="range-tick" style="left:${at(expected)}%"></span>`
    : '';
  const ticks = `${bounds}<span class="range-zero" style="left:${at(0)}%"></span>`;
  const tone = today > expected ? 'up' : today < -expected ? 'down' : '';
  return `<div class="range"><div class="range-scale num"><span>${formatPct(-scale)}</span><span>${formatPct(scale)}</span></div><div class="range-track">${ticks}<span class="range-mark ${tone}" style="left:${at(today)}%"></span></div></div>`;
}

function sparkline(values, change) {
  if (!values || values.length < 2) return '<span class="spark"></span>';
  const w = 68;
  const h = 28;
  const pad = 2;
  let min = values[0];
  let max = values[0];
  for (const v of values) {
    if (v < min) min = v;
    if (v > max) max = v;
  }
  const span = max - min || 1;
  const d = values.map((v, i) => {
    const x = pad + (i / (values.length - 1)) * (w - pad * 2);
    const y = pad + (1 - (v - min) / span) * (h - pad * 2);
    return (i ? 'L' : 'M') + x.toFixed(1) + ' ' + y.toFixed(1);
  }).join(' ');
  return `<span class="spark ${pctClass(change)}" aria-hidden="true"><svg viewBox="0 0 ${w} ${h}"><path d="${d}"></path></svg></span>`;
}

function movedOrder(from, target, count) {
  const order = [];
  for (let index = 0; index < count; index++) if (index !== from) order.push(index);
  order.splice(target, 0, from);
  return order;
}

function dropIndex(drag, clientY, scrollDelta) {
  const topOf = (index) => drag.viewportTops[index] - scrollDelta;
  const center = clientY - drag.grab + drag.heights[drag.from] / 2;
  let target = drag.from;
  if (center >= topOf(drag.from) + drag.heights[drag.from] / 2) {
    for (let index = drag.from + 1; index < drag.heights.length; index++) {
      if (center > topOf(index) + drag.heights[index] / 2) target = index;
    }
  } else {
    for (let index = drag.from - 1; index >= 0; index--) {
      if (center < topOf(index) + drag.heights[index] / 2) target = index;
    }
  }
  return target;
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
function escapeAttr(s) { return escapeHTML(s); }

customElements.define('sw-board', SwBoard);
