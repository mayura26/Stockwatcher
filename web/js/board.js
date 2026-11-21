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
    if (!this.editing || !group) {
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
      host.innerHTML = '<p class="empty">Nothing in this group.</p>';
      return;
    }
    host.innerHTML = rows.map((row, i) => {
      const flash = this.flashes.get(row.id);
      const open = this.openChanges.has(row.id);
      const main = `
          <span class="ident">
            <span class="sym">${escapeHTML(row.display)}</span>
            <span class="sub">${escapeHTML(row.name || '')}</span>
          </span>
          ${sparkline(row.sparkline, row.change_d)}
          <span class="px num ${priceTone(row.change_d)}">${formatPrice(row.price)}</span>`;
      const body = this.editing
        ? `<div class="row-main">${main}</div>`
        : `<button type="button" class="row-main" data-open="${row.id}">${main}</button>`;
      const more = open ? `<div class="more">
            <span class="more-cell"><span class="chg-h">1H</span>${chg(row.change_1h)}</span>
            <span class="more-cell"><span class="chg-h">4H</span>${chg(row.change_4h)}</span>
            <span class="more-cell"><span class="chg-h">W</span>${chg(row.change_w)}</span>
            <span class="more-cell"><span class="chg-h">M</span>${chg(row.change_m)}</span>
          </div>` : '';
      const moves = this.editing ? `<span class="row-edit">
            <button type="button" class="mini" data-move-symbol="${row.id}" data-dir="-1" ${i === 0 ? 'disabled' : ''}>Up</button>
            <button type="button" class="mini" data-move-symbol="${row.id}" data-dir="1" ${i === rows.length - 1 ? 'disabled' : ''}>Down</button>
          </span>` : '';
      return `<div class="row${flash ? ' flash-' + flash : ''}${open ? ' is-open' : ''}">${body}<button type="button" class="day" data-expand="${row.id}" aria-expanded="${open}"><span class="chg-h">D</span>${chg(row.change_d)}</button><button type="button" class="row-x" data-remove="${row.id}" aria-label="Remove ${escapeAttr(row.display)}">×</button>${more}${moves}</div>`;
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
    const expand = event.target.closest('[data-expand]');
    if (expand) {
      const id = Number(expand.dataset.expand);
      if (this.openChanges.has(id)) this.openChanges.delete(id);
      else this.openChanges.add(id);
      this.renderRows(this.group);
      return;
    }
    const open = event.target.closest('[data-open]');
    if (open && !this.editing) {
      this.emit({ type: 'open', id: Number(open.dataset.open) });
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
    if (gmove) this.emit({ type: 'move-group', dir: Number(gmove.dataset.moveGroup) });
    if (event.target.closest('[data-delete-group]')) this.emit({ type: 'delete-group' });
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
