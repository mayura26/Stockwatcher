import { formatClock, formatPct, formatPrice, pctClass, priceTone } from './format.js';

const COLORS = [
  { value: '#c6a15b', label: 'Brass' },
  { value: '#8eae8a', label: 'Sage' },
  { value: '#c56b52', label: 'Clay' },
  { value: '#e8e0cc', label: 'Sand' },
  { value: '#8aa0ae', label: 'Slate' },
  { value: '#a35d6a', label: 'Wine' },
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
    this.innerHTML = `
      <header class="top">
        <div class="brand">Stockwatcher</div>
        <div class="top-actions">
          <button type="button" class="text-btn" data-act="edit">Edit</button>
          <button type="button" class="text-btn" data-act="alerts">Alerts</button>
        </div>
      </header>
      <div class="groups" role="tablist"></div>
      <div class="board">
        <div class="edit-slot"></div>
        <div class="cols"></div>
        <div class="rows"></div>
        <button type="button" class="add-row" data-act="search" aria-label="Add a symbol">+</button>
      </div>
      ${groupSheet()}`;
    this.addEventListener('click', (event) => this.onClick(event));
    this.addEventListener('submit', (event) => this.onSubmit(event));
    this.onKey = (event) => {
      if (event.key === 'Escape' && this.sheetOpen) this.closeGroupSheet();
    };
    window.addEventListener('keydown', this.onKey);
  }

  disconnectedCallback() {
    window.removeEventListener('keydown', this.onKey);
  }

  setState({ board, groupId, flashes, editing, armed }) {
    const y = window.scrollY;
    this.board = board;
    this.groupId = groupId;
    this.editing = editing;
    this.flashes = flashes || new Map();
    const group = (board?.groups || []).find((g) => g.id === groupId) || board?.groups?.[0];
    this.group = group;
    this.renderGroups(board);
    this.renderEdit(group);
    this.renderCols(group);
    this.renderRows(group);
    const alerts = this.querySelector('[data-act="alerts"]');
    alerts.innerHTML = armed ? 'Alerts<span class="dot" aria-label="armed alerts"></span>' : 'Alerts';
    this.querySelector('[data-act="edit"]').classList.toggle('is-on', editing);
    this.querySelector('[data-act="edit"]').setAttribute('aria-pressed', editing ? 'true' : 'false');
    this.querySelector('.add-row').hidden = !!group?.synthetic;
    window.scrollTo(0, y);
  }

  renderGroups(board) {
    const host = this.querySelector('.groups');
    const groups = board?.groups || [];
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

  renderRows(group) {
    const host = this.querySelector('.rows');
    const rows = group?.rows || [];
    if (!rows.length) {
      host.innerHTML = group?.synthetic
        ? '<p class="empty">No unusual moves.</p>'
        : '<p class="empty">Nothing in this group.</p>';
      return;
    }
    const fixed = !!group?.synthetic;
    host.innerHTML = rows.map((row, i) => {
      const flash = this.flashes.get(row.id);
      const open = this.openChanges.has(row.id);
      const expanded = open ? 'true' : 'false';
      const name = `<button type="button" class="card-name" data-expand="${row.id}" aria-expanded="${expanded}">
            <span class="ident">
              <span class="sym">${escapeHTML(row.display)}</span>
              <span class="sub">${escapeHTML(row.name || '')}</span>
            </span>
          </button>`;
      const price = `<button type="button" class="card-px" data-expand="${row.id}" aria-expanded="${expanded}">
            <span class="px num ${priceTone(row.change_d)}">${formatPrice(row.price)}</span>
            <span class="day"><span class="chg-h">D</span>${chg(row.change_d)}</span>
          </button>`;
      const more = open ? `<div class="more">${rangeMeter(row)}<div class="more-chgs">
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
      return `<div class="row${flash ? ' flash-' + flash : ''}${open ? ' is-open' : ''}${fixed ? ' is-auto' : ''}">${name}${sparkHit(row, this.editing)}${price}${remove}${more}${moves}</div>`;
    }).join('');
  }

  openGroupSheet(mode) {
    this.sheetMode = mode;
    const group = this.group;
    const editing = mode === 'edit' && group;
    this.draft = {
      color: chipColor(editing ? group.color : COLORS[0].value),
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

  onClick(event) {
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
      this.renderRows(this.group);
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

function chg(n) {
  return `<span class="chg ${pctClass(n)}">${formatPct(n)}</span>`;
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
  const width = high - low;
  const clamp = (v) => Math.max(0, Math.min(100, ((v - low) / width) * 100));
  let travel = '';
  const spark = row.sparkline || [];
  if (spark.length) {
    let smin = spark[0];
    let smax = spark[0];
    for (const v of spark) {
      if (v < smin) smin = v;
      if (v > smax) smax = v;
    }
    const a = clamp(smin);
    const b = clamp(smax);
    travel = `<span class="range-span" style="left:${a}%;width:${Math.max(b - a, 0.8)}%"></span>`;
  }
  let mark = '';
  if (row.price != null && !Number.isNaN(Number(row.price))) {
    const price = Number(row.price);
    const tone = price > high ? 'up' : price < low ? 'down' : '';
    mark = `<span class="range-mark ${tone}" style="left:${clamp(price)}%"></span>`;
  }
  return `<div class="range"><div class="range-scale num"><span>${formatPrice(low)}</span><span>${formatPrice(high)}</span></div><div class="range-track">${travel}${mark}</div></div>`;
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

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
function escapeAttr(s) { return escapeHTML(s); }

customElements.define('sw-board', SwBoard);
