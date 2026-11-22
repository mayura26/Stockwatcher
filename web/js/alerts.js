import { alertMarks, formatPrice } from './format.js';

class SwAlerts extends HTMLElement {
  connectedCallback() {
    this.openSymbols = new Set();
    this.innerHTML = `
      <div class="alerts-screen">
        <div class="sheet-pad">
          <div class="push-row">
            <span class="push-actions">
              <button type="button" class="btn" data-push>Enable alerts</button>
              <button type="button" class="btn" data-test>Test</button>
            </span>
            <span class="sub" data-push-state></span>
          </div>
          <p class="note">On iPhone, add Stockwatcher to the Home Screen first. Push needs iOS 16.4 or later, and the watcher only runs while the server’s scheduled task is on.</p>
          <form class="bracket-form">
            <select name="symbol" aria-label="Stock"></select>
            <input name="percent" inputmode="decimal" placeholder="%" aria-label="Bracket percent">
            <button type="submit" class="text-btn">Bracket</button>
          </form>
          <ul class="alert-list" data-list></ul>
        </div>
      </div>`;
    this.querySelector('.bracket-form').addEventListener('submit', (event) => {
      event.preventDefault();
      const percent = Number(new FormData(event.target).get('percent'));
      const symbolId = Number(new FormData(event.target).get('symbol'));
      if (!symbolId) return;
      if (!(percent > 0) || percent >= 100) {
        this.emit({ type: 'error', message: 'Enter a percent between 0 and 100.' });
        return;
      }
      this.emit({ type: 'set-bracket', symbolId, percent });
      event.target.querySelector('[name="percent"]').value = '';
    });
    this.querySelector('[data-push]').addEventListener('click', () => this.enable());
    this.querySelector('[data-test]').addEventListener('click', () => this.testPush());
    this.querySelector('[data-list]').addEventListener('toggle', (event) => {
      const details = event.target.closest('details[data-symbol]');
      if (!details || !this.openSymbols) return;
      const id = Number(details.dataset.symbol);
      if (details.open) this.openSymbols.add(id);
      else this.openSymbols.delete(id);
    }, true);
    this.addEventListener('click', (event) => {
      const rearm = event.target.closest('[data-rearm]');
      if (rearm) this.emit({ type: 'rearm-alert', id: Number(rearm.dataset.rearm) });
      const del = event.target.closest('[data-delete-alert]');
      if (del) this.emit({ type: 'delete-alert', id: Number(del.dataset.deleteAlert) });
    });
  }

  setSymbols(symbols) {
    const next = symbols || [];
    const select = this.querySelector('[name="symbol"]');
    if (!select) return;
    const current = select.value;
    const prev = this.symbols || [];
    const same = prev.length === next.length && prev.every((row, i) => row.id === next[i].id && row.display === next[i].display && row.price === next[i].price);
    this.symbols = next;
    if (same && select.options.length) return;
    if (!next.length) {
      select.innerHTML = '<option value="">No stocks</option>';
      select.disabled = true;
      return;
    }
    select.disabled = false;
    select.innerHTML = next.map((row) => `<option value="${Number(row.id)}"></option>`).join('');
    next.forEach((row, i) => {
      const price = row.price == null ? '' : ' · ' + formatPrice(row.price);
      select.options[i].textContent = row.display + price;
    });
    if (current && next.some((row) => String(row.id) === current)) select.value = current;
  }

  setAlerts(alerts) {
    this.alerts = alerts || [];
    const list = this.querySelector('[data-list]');
    if (!list) return;
    if (!this.openSymbols) this.openSymbols = new Set();
    list.querySelectorAll('details[data-symbol]').forEach((details) => {
      const id = Number(details.dataset.symbol);
      if (details.open) this.openSymbols.add(id);
      else this.openSymbols.delete(id);
    });
    if (!this.alerts.length) {
      this.openSymbols.clear();
      list.innerHTML = '<li class="alert-empty"><span class="sub">No alerts yet. Set one from a chart.</span></li>';
      return;
    }
    const groups = groupAlerts(this.alerts);
    const present = new Set(groups.map((group) => group.symbolId));
    for (const id of this.openSymbols) {
      if (!present.has(id)) this.openSymbols.delete(id);
    }
    list.innerHTML = groups.map((group) => `
      <li class="alert-group">
        <details data-symbol="${group.symbolId}"${this.openSymbols.has(group.symbolId) ? ' open' : ''}>
          <summary>
            <strong>${escapeHTML(group.display)}</strong>
            <span class="sub">${group.alerts.length} ${group.alerts.length === 1 ? 'alert' : 'alerts'}</span>
          </summary>
          <ul class="alert-rows">
            ${group.alerts.map((a) => `
              <li class="${a.status === 'triggered' ? 'is-triggered' : ''}">
                ${alertMarks(a)}
                <span>
                  ${a.status === 'triggered' && a.repeat !== 'continuous' ? `<button type="button" class="mini" data-rearm="${a.id}">Rearm</button>` : ''}
                  <button type="button" class="mini danger" data-delete-alert="${a.id}">Delete</button>
                </span>
              </li>`).join('')}
          </ul>
        </details>
      </li>`).join('');
  }

  async testPush() {
    const state = this.querySelector('[data-push-state]');
    const button = this.querySelector('[data-test]');
    button.disabled = true;
    try {
      await this.api.post('/api/push/test', {});
      state.textContent = 'Sent';
    } catch (err) {
      state.textContent = err.message;
    } finally {
      button.disabled = false;
    }
  }

  async enable() {
    const state = this.querySelector('[data-push-state]');
    try {
      if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
        throw new Error('This browser cannot receive push');
      }
      const perm = await Notification.requestPermission();
      if (perm !== 'granted') throw new Error('Notifications are blocked');
      const reg = await navigator.serviceWorker.ready;
      const key = await this.api.get('/api/push/key');
      if (!key.enabled || !key.public_key) throw new Error('Push is not configured on the server');
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: decodeKey(key.public_key),
      });
      await this.api.post('/api/push/subscribe', sub.toJSON());
      state.textContent = 'On';
    } catch (err) {
      state.textContent = err.message;
    }
  }

  emit(detail) {
    this.dispatchEvent(new CustomEvent('sw-action', { bubbles: true, detail }));
  }
}

function groupAlerts(alerts) {
  const groups = [];
  const byId = new Map();
  for (const alert of alerts) {
    let group = byId.get(alert.symbol_id);
    if (!group) {
      group = { symbolId: alert.symbol_id, display: alert.display, alerts: [] };
      byId.set(alert.symbol_id, group);
      groups.push(group);
    }
    group.alerts.push(alert);
  }
  return groups;
}

function decodeKey(value) {
  const padding = '='.repeat((4 - (value.length % 4)) % 4);
  const b64 = (value + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(b64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

customElements.define('sw-alerts', SwAlerts);
