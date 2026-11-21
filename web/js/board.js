import { formatClock, formatPct, formatPrice, pctClass } from './format.js';

class SwBoard extends HTMLElement {
  connectedCallback() {
    this.innerHTML = `
      <header class="top">
        <div class="brand">Stockwatcher</div>
        <div class="top-actions">
          <button type="button" class="text-btn" data-act="edit">Edit</button>
          <button type="button" class="text-btn" data-act="alerts">Alerts</button>
          <button type="button" class="text-btn" data-act="search">Add</button>
        </div>
      </header>
      <div class="groups" role="tablist"></div>
      <div class="board">
        <div class="edit-slot"></div>
        <div class="cols"></div>
        <div class="rows"></div>
      </div>`;
    this.addEventListener('click', (event) => this.onClick(event));
    this.addEventListener('submit', (event) => this.onSubmit(event));
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
    const chips = groups.map((g) => `
      <button type="button" class="chip${g.id === this.group?.id ? ' is-on' : ''}" role="tab" data-group="${g.id}" aria-selected="${g.id === this.group?.id}">${escapeHTML(g.name)}</button>
    `).join('');
    const add = this.adding
      ? `<form data-new-group><input name="group" aria-label="New group name" maxlength="40" placeholder="Group" enterkeyhint="done"><button type="submit" class="chip">Add</button></form>`
      : '<button type="button" class="chip chip-add" data-act="new-group" aria-label="New group">+</button>';
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
        <form data-rename>
          <input name="name" value="${escapeAttr(group.name)}" aria-label="Group name" maxlength="40">
          <button type="submit" class="text-btn">Rename</button>
        </form>
        <div class="edit-moves">
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
    cols.innerHTML = `<span class="asof">${oldest ? escapeHTML(formatClock(oldest)) : ''}</span><span></span><span class="chg-h">D</span><span class="chg-h">W</span><span class="chg-h">M</span>`;
  }

  renderRows(group) {
    const host = this.querySelector('.rows');
    const rows = group?.rows || [];
    if (!rows.length) {
      host.innerHTML = '<p class="empty">Nothing in this group. <button type="button" data-act="search">Add a symbol</button></p>';
      return;
    }
    host.innerHTML = rows.map((row, i) => {
      const flash = this.flashes.get(row.id);
      const body = `
          <span class="ident">
            <span class="sym">${escapeHTML(row.display)}</span>
            <span class="sub">${escapeHTML(row.name || '')}</span>
          </span>
          <span class="px num">${formatPrice(row.price)}</span>
          ${chg(row.change_d)}
          ${chg(row.change_w)}
          ${chg(row.change_m)}
          ${this.editing ? `<span class="row-edit">
            <button type="button" class="mini" data-move-symbol="${row.id}" data-dir="-1" ${i === 0 ? 'disabled' : ''}>Up</button>
            <button type="button" class="mini" data-move-symbol="${row.id}" data-dir="1" ${i === rows.length - 1 ? 'disabled' : ''}>Down</button>
            <button type="button" class="mini danger" data-remove="${row.id}">Remove</button>
          </span>` : ''}`;
      if (this.editing) return `<div class="row${flash ? ' flash-' + flash : ''}">${body}</div>`;
      return `<button type="button" class="row${flash ? ' flash-' + flash : ''}" data-open="${row.id}">${body}</button>`;
    }).join('');
  }

  onClick(event) {
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
        this.adding = true;
        this.renderGroups(this.board);
        this.querySelector('[name="group"]')?.focus();
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
    const form = event.target.closest('[data-rename]');
    event.preventDefault();
    if (form) {
      this.emit({ type: 'rename-group', name: new FormData(form).get('name') });
      return;
    }
    const created = event.target.closest('[data-new-group]');
    if (!created) return;
    const name = new FormData(created).get('group');
    this.adding = false;
    if (name && String(name).trim()) this.emit({ type: 'create-group', name: String(name).trim() });
    else this.renderGroups(this.board);
  }

  emit(detail) {
    this.dispatchEvent(new CustomEvent('sw-action', { bubbles: true, detail }));
  }
}

function chg(n) {
  return `<span class="chg ${pctClass(n)}">${formatPct(n)}</span>`;
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
function escapeAttr(s) { return escapeHTML(s); }

customElements.define('sw-board', SwBoard);
