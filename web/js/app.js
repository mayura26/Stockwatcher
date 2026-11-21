import { API } from './api.js';
import './board.js';
import './search.js';
import './chart-view.js';
import './alerts.js';

const TOKEN_KEY = 'sw.token';
const GROUP_KEY = 'sw.group';

function parseRoute() {
  const hash = location.hash || '#/';
  const chart = hash.match(/^#\/chart\/(\d+)/);
  if (chart) return { name: 'chart', id: Number(chart[1]) };
  if (hash.startsWith('#/alerts')) return { name: 'alerts' };
  return { name: 'board' };
}

function findRow(board, id) {
  for (const group of board?.groups || []) {
    for (const row of group.rows || []) {
      if (row.id === id) return row;
    }
  }
  return null;
}

class SwApp extends HTMLElement {
  connectedCallback() {
    if (this.ready) return;
    this.ready = true;
    this.token = localStorage.getItem(TOKEN_KEY) || '';
    this.api = API(() => this.token);
    this.board = null;
    this.alerts = [];
    this.prices = new Map();
    this.flashes = new Map();
    this.searchOpen = false;
    this.editing = false;
    this.dragging = false;
    this.error = '';
    this.shell = false;
    this.poll = 0;
    window.addEventListener('hashchange', () => this.showRoute());
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden && !this.editing && !this.searchOpen && !this.dragging) this.refreshData().catch(() => {});
      this.syncPoll();
    });
    window.addEventListener('sw-unauthorized', () => this.lock());
    window.addEventListener('keydown', (event) => {
      if (event.key !== 'Escape') return;
      if (this.searchOpen) {
        this.searchOpen = false;
        this.showRoute();
        return;
      }
      if (parseRoute().name !== 'board') location.hash = '#/';
    });
    navigator.serviceWorker?.addEventListener('message', (event) => {
      if (event.data?.type === 'navigate' && event.data.url) location.href = event.data.url;
    });
    this.addEventListener('sw-action', (event) => this.onAction(event.detail));
    if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {});
    if (!this.token) {
      this.renderGate();
      return;
    }
    this.boot();
  }

  lock() {
    this.token = '';
    localStorage.removeItem(TOKEN_KEY);
    clearInterval(this.poll);
    this.shell = false;
    this.renderGate('Token was rejected');
  }

  renderGate(message) {
    this.innerHTML = `
      <form class="gate">
        <h1>Stockwatcher</h1>
        <hr class="rule">
        <p class="lede">A private blotter for stocks, CME futures, and crypto.</p>
        <label class="field">Access token
          <input type="password" name="token" autocomplete="current-password" required>
        </label>
        <button class="btn" type="submit">Unlock</button>
        <p class="err">${message ? escapeHTML(message) : ''}</p>
      </form>`;
    this.querySelector('form').addEventListener('submit', async (event) => {
      event.preventDefault();
      this.token = new FormData(event.target).get('token').trim();
      localStorage.setItem(TOKEN_KEY, this.token);
      this.shell = false;
      try {
        await this.boot();
      } catch (err) {
        if (err.message !== 'unauthorized') this.renderGate(err.message);
      }
    });
  }

  async boot() {
    this.renderShell();
    await this.refreshData();
    this.showRoute();
    this.syncPoll();
  }

  renderShell() {
    if (this.shell) return;
    this.shell = true;
    this.innerHTML = `
      <p class="banner" hidden></p>
      <sw-board></sw-board>
      <sw-search hidden></sw-search>
      <sw-chart hidden></sw-chart>
      <sw-alerts hidden></sw-alerts>`;
    const api = this.api;
    this.querySelector('sw-search').api = api;
    this.querySelector('sw-chart').api = api;
    this.querySelector('sw-alerts').api = api;
  }

  groupId() {
    const groups = this.board?.groups || [];
    if (!groups.length) return 0;
    const raw = localStorage.getItem(GROUP_KEY);
    if (raw === 'all') return 'all';
    if (raw != null && raw !== '') {
      const saved = Number(raw);
      if (groups.some((g) => g.id === saved)) return saved;
    }
    const real = groups.find((g) => !g.synthetic);
    return (real || groups[0]).id;
  }

  diff(board) {
    const flashes = new Map();
    for (const group of board.groups || []) {
      for (const row of group.rows || []) {
        if (row.price == null) continue;
        const prev = this.prices.get(row.id);
        if (prev != null && prev !== row.price) flashes.set(row.id, row.price > prev ? 'up' : 'down');
        this.prices.set(row.id, row.price);
      }
    }
    return flashes;
  }

  async refreshData() {
    const board = await this.api.get('/api/board');
    if (this.dragging) return;
    this.flashes = this.diff(board);
    this.board = board;
    const route = parseRoute();
    if (route.name === 'alerts' || route.name === 'chart') {
      const data = await this.api.get('/api/alerts');
      this.alerts = data.alerts || [];
    }
    if (!this.shell) return;
    if (route.name === 'board') this.paintBoard();
    else if (route.name === 'chart') {
      this.querySelector('sw-chart').setLive(findRow(board, route.id));
      if (this.querySelector('sw-chart').symbolId === route.id) this.querySelector('sw-chart').loadAlerts();
    } else if (route.name === 'alerts') {
      this.querySelector('sw-alerts').setAlerts(this.alerts);
    }
  }

  paintBoard() {
    if (this.dragging) return;
    this.querySelector('sw-board').setState({
      board: this.board,
      groupId: this.groupId(),
      flashes: this.flashes,
      editing: this.editing,
      armed: this.board?.armed_alerts || 0,
    });
    this.flashes = new Map();
  }

  showRoute() {
    if (!this.token) return;
    if (!this.shell) this.renderShell();
    const route = parseRoute();
    if (route.name !== 'board') this.searchOpen = false;
    const board = this.querySelector('sw-board');
    const chart = this.querySelector('sw-chart');
    const alerts = this.querySelector('sw-alerts');
    const search = this.querySelector('sw-search');
    board.hidden = route.name !== 'board';
    chart.hidden = route.name !== 'chart';
    alerts.hidden = route.name !== 'alerts';
    search.hidden = !(route.name === 'board' && this.searchOpen);
    this.toggleBanner();
    if (route.name !== 'chart') chart.close();
    if (route.name === 'board') this.paintBoard();
    if (route.name === 'chart') chart.open(route.id);
    if (route.name === 'alerts') {
      alerts.api = this.api;
      alerts.setAlerts(this.alerts);
      if (!this.alerts.length) this.api.get('/api/alerts').then((data) => {
        this.alerts = data.alerts || [];
        alerts.setAlerts(this.alerts);
      }).catch((err) => this.fail(err));
    }
    if (this.searchOpen && route.name === 'board') search.open();
    this.syncPoll();
  }

  syncPoll() {
    clearInterval(this.poll);
    this.poll = 0;
    if (!this.token || document.hidden || this.editing || this.searchOpen || this.dragging) return;
    this.poll = setInterval(() => this.refreshData().catch((err) => this.fail(err)), 20000);
  }

  toggleBanner() {
    const banner = this.querySelector('.banner');
    if (!banner) return;
    banner.hidden = !this.error;
    banner.textContent = this.error || '';
  }

  fail(err) {
    if (!err || err.message === 'unauthorized') return;
    this.error = err.message || 'Something went wrong';
    this.toggleBanner();
  }

  async onAction(detail) {
    try {
      this.error = '';
      this.toggleBanner();
      await this.handle(detail);
    } catch (err) {
      this.fail(err);
    }
  }

  async handle(detail) {
    switch (detail.type) {
      case 'search':
        this.searchOpen = true;
        this.querySelector('sw-search').hidden = false;
        this.querySelector('sw-search').open();
        this.syncPoll();
        return;
      case 'close-search':
        this.searchOpen = false;
        this.querySelector('sw-search').hidden = true;
        this.syncPoll();
        return;
      case 'alerts':
        location.hash = '#/alerts';
        return;
      case 'open':
        location.hash = '#/chart/' + detail.id;
        return;
      case 'edit':
        this.editing = !this.editing;
        this.paintBoard();
        this.syncPoll();
        return;
      case 'create-group': {
        const group = await this.api.post('/api/groups', {
          name: detail.name,
          color: detail.color,
          icon: detail.icon,
        });
        localStorage.setItem(GROUP_KEY, String(group.id));
        this.querySelector('sw-board').closeGroupSheet();
        await this.refreshData();
        return;
      }
      case 'group':
        localStorage.setItem(GROUP_KEY, String(detail.id));
        this.paintBoard();
        return;
      case 'rename-group':
        await this.api.patch('/api/groups/' + this.groupId(), {
          name: detail.name,
          color: detail.color,
          icon: detail.icon,
        });
        this.querySelector('sw-board').closeGroupSheet();
        await this.refreshData();
        return;
      case 'delete-group':
        await this.api.del('/api/groups/' + this.groupId());
        localStorage.removeItem(GROUP_KEY);
        await this.refreshData();
        return;
      case 'move-group': {
        const ids = (this.board?.groups || []).filter((g) => !g.synthetic).map((g) => g.id);
        const index = ids.indexOf(this.groupId());
        const next = index + detail.dir;
        if (index < 0 || next < 0 || next >= ids.length) return;
        const swap = ids[next];
        ids[next] = ids[index];
        ids[index] = swap;
        await this.api.put('/api/groups/order', { ids });
        await this.refreshData();
        return;
      }
      case 'remove-symbol':
        await this.api.del('/api/groups/' + this.groupId() + '/symbols/' + detail.symbolId);
        await this.refreshData();
        return;
      case 'drag-lock':
        this.dragging = !!detail.on;
        this.syncPoll();
        return;
      case 'reorder-symbols': {
        const group = (this.board?.groups || []).find((g) => g.id === this.groupId());
        const rows = group?.rows || [];
        const byId = new Map(rows.map((row) => [row.id, row]));
        const next = (detail.ids || []).map((id) => byId.get(id));
        if (!group || next.some((row) => !row) || next.length !== rows.length) return;
        const previous = rows.slice();
        group.rows = next;
        try {
          await this.api.put('/api/groups/' + this.groupId() + '/symbols/order', { ids: detail.ids });
        } catch (err) {
          group.rows = previous;
          await this.refreshData();
          throw err;
        }
        await this.refreshData();
        return;
      }
      case 'move-symbol': {
        const group = (this.board?.groups || []).find((g) => g.id === this.groupId());
        const ids = (group?.rows || []).map((row) => row.id);
        const index = ids.indexOf(detail.symbolId);
        const next = index + detail.dir;
        if (index < 0 || next < 0 || next >= ids.length) return;
        const swap = ids[next];
        ids[next] = ids[index];
        ids[index] = swap;
        await this.api.put('/api/groups/' + this.groupId() + '/symbols/order', { ids });
        await this.refreshData();
        return;
      }
      case 'toggle-owned':
        if (detail.owned) await this.api.put('/api/owned/' + detail.symbolId, {});
        else await this.api.del('/api/owned/' + detail.symbolId);
        await this.refreshData();
        return;
      case 'add':
        await this.api.post('/api/groups/' + this.groupId() + '/symbols', {
          asset_class: detail.hit.asset_class,
          display: detail.hit.display,
          provider_symbol: detail.hit.provider_symbol,
          name: detail.hit.name,
        });
        this.searchOpen = false;
        this.querySelector('sw-search').hidden = true;
        await this.refreshData();
        setTimeout(() => this.refreshData().catch(() => {}), 2500);
        this.syncPoll();
        return;
      case 'set-alert':
        await this.api.post('/api/alerts', {
          symbol_id: detail.symbolId,
          direction: detail.direction,
          repeat: detail.repeat || 'once',
          level: detail.level,
        });
        await this.querySelector('sw-chart').loadAlerts();
        await this.refreshData();
        return;
      case 'delete-alert':
        await this.api.del('/api/alerts/' + detail.id);
        await this.afterAlertChange();
        return;
      case 'rearm-alert':
        await this.api.post('/api/alerts/' + detail.id + '/rearm', {});
        await this.afterAlertChange();
        return;
      case 'error':
        throw new Error(detail.message);
      default:
        return;
    }
  }

  async afterAlertChange() {
    const data = await this.api.get('/api/alerts');
    this.alerts = data.alerts || [];
    const route = parseRoute();
    if (route.name === 'alerts') this.querySelector('sw-alerts').setAlerts(this.alerts);
    if (route.name === 'chart') await this.querySelector('sw-chart').loadAlerts();
    await this.refreshData();
  }
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

customElements.define('sw-app', SwApp);
