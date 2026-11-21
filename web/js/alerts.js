import { crossLabel, formatPrice, repeatLabel } from './format.js';

class SwAlerts extends HTMLElement {
  connectedCallback() {
    this.innerHTML = `
      <div class="alerts-screen">
        <header class="top">
          <button type="button" class="back" data-back>Back</button>
          <div class="brand">Alerts</div>
          <span></span>
        </header>
        <div class="sheet-pad">
          <div class="push-row">
            <button type="button" class="btn" data-push style="margin-top:0">Enable alerts</button>
            <span class="sub" data-push-state></span>
          </div>
          <p class="note">On iPhone, add Stockwatcher to the Home Screen first. Push needs iOS 16.4 or later, and the watcher only runs while the server’s scheduled task is on.</p>
          <ul class="alert-list" data-list></ul>
        </div>
      </div>`;
    this.querySelector('[data-back]').addEventListener('click', () => { location.hash = '#/'; });
    this.querySelector('[data-push]').addEventListener('click', () => this.enable());
    this.addEventListener('click', (event) => {
      const rearm = event.target.closest('[data-rearm]');
      if (rearm) this.emit({ type: 'rearm-alert', id: Number(rearm.dataset.rearm) });
      const del = event.target.closest('[data-delete-alert]');
      if (del) this.emit({ type: 'delete-alert', id: Number(del.dataset.deleteAlert) });
    });
  }

  setAlerts(alerts) {
    this.alerts = alerts || [];
    const list = this.querySelector('[data-list]');
    if (!list) return;
    if (!this.alerts.length) {
      list.innerHTML = '<li><span class="sub">No alerts yet. Set one from a chart.</span></li>';
      return;
    }
    list.innerHTML = this.alerts.map((a) => `
      <li>
        <span><strong>${escapeHTML(a.display)}</strong> ${crossLabel(a.direction)} ${formatPrice(a.level)} <span class="sub">${repeatLabel(a.repeat)} · ${escapeHTML(a.status)}</span></span>
        <span>
          ${a.status === 'triggered' && a.repeat !== 'continuous' ? `<button type="button" class="mini" data-rearm="${a.id}">Rearm</button>` : ''}
          <button type="button" class="mini danger" data-delete-alert="${a.id}">Delete</button>
        </span>
      </li>`).join('');
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
