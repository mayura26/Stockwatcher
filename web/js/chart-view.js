import { formatPct, formatPrice, pctClass, priceFormat } from './format.js';

let chartsPromise;

function loadCharts() {
  if (window.LightweightCharts) return Promise.resolve(window.LightweightCharts);
  if (!chartsPromise) {
    chartsPromise = new Promise((resolve, reject) => {
      const script = document.createElement('script');
      script.src = '/vendor/lightweight-charts.standalone.production.js';
      script.onload = () => resolve(window.LightweightCharts);
      script.onerror = () => reject(new Error('Chart library failed to load'));
      document.head.appendChild(script);
    });
  }
  return chartsPromise;
}

class SwChart extends HTMLElement {
  connectedCallback() {
    this.resolution = 'D';
    this.kind = 'candle';
    this.direction = 'above';
    this.innerHTML = `
      <div class="chart-screen">
        <header class="top">
          <button type="button" class="back" data-back>Back</button>
          <div>
            <div class="sym" data-display></div>
            <div class="sub" data-name></div>
          </div>
          <div class="px-block">
            <div class="px num" data-price></div>
            <div class="chg" data-change></div>
          </div>
        </header>
        <div class="toggles">
          <div class="seg" role="group" aria-label="Chart style">
            <button type="button" data-kind="line">Line</button>
            <button type="button" data-kind="candle" aria-pressed="true">Candle</button>
          </div>
          <div class="seg" role="group" aria-label="Resolution">
            <button type="button" data-res="D" aria-pressed="true">D</button>
            <button type="button" data-res="W">W</button>
            <button type="button" data-res="M">M</button>
          </div>
        </div>
        <div class="chart-host"></div>
        <p class="chart-msg" data-msg></p>
        <form class="alert-form">
          <input name="level" inputmode="decimal" placeholder="Level" aria-label="Alert level">
          <div class="seg" role="group" aria-label="Direction">
            <button type="button" data-dir="above" aria-pressed="true">Above</button>
            <button type="button" data-dir="below" aria-pressed="false">Below</button>
          </div>
          <button type="submit" class="text-btn">Set</button>
        </form>
        <ul class="alert-mini" data-alerts></ul>
      </div>`;
    this.querySelector('[data-back]').addEventListener('click', () => {
      location.hash = '#/';
    });
    this.querySelector('form').addEventListener('submit', (event) => {
      event.preventDefault();
      const level = Number(new FormData(event.target).get('level'));
      if (!level || level <= 0) {
        this.setMsg('Enter a price level.');
        return;
      }
      this.dispatchEvent(new CustomEvent('sw-action', {
        bubbles: true,
        detail: { type: 'set-alert', symbolId: this.symbolId, direction: this.direction, level },
      }));
      event.target.reset();
    });
    this.addEventListener('click', (event) => {
      const kind = event.target.closest('[data-kind]');
      if (kind && kind.dataset.kind !== this.kind) {
        this.kind = kind.dataset.kind;
        this.syncPressed();
        this.paint();
      }
      const res = event.target.closest('[data-res]');
      if (res && res.dataset.res !== this.resolution) {
        this.resolution = res.dataset.res;
        this.syncPressed();
        this.loadBars();
      }
      const dir = event.target.closest('[data-dir]');
      if (dir) {
        this.direction = dir.dataset.dir;
        this.syncPressed();
      }
      const rearm = event.target.closest('[data-rearm]');
      if (rearm) this.dispatchEvent(new CustomEvent('sw-action', { bubbles: true, detail: { type: 'rearm-alert', id: Number(rearm.dataset.rearm) } }));
      const del = event.target.closest('[data-delete-alert]');
      if (del) this.dispatchEvent(new CustomEvent('sw-action', { bubbles: true, detail: { type: 'delete-alert', id: Number(del.dataset.deleteAlert) } }));
    });
  }

  async open(id) {
    const changed = this.symbolId !== id;
    this.symbolId = id;
    if (changed) {
      this.payload = null;
      this.resolution = 'D';
      this.kind = 'candle';
      this.direction = 'above';
      this.destroyChart();
      this.syncPressed();
    }
    if (!this.payload || this.payload.resolution !== this.resolution) await this.loadBars();
    else {
      this.applyHeader();
      await this.paint();
    }
    await this.loadAlerts();
  }

  setLive(row) {
    this.live = row || null;
    if (this.payload) this.applyHeader();
  }

  async loadBars() {
    this.setMsg('Loading history…');
    try {
      this.payload = await this.api.get('/api/bars?id=' + this.symbolId + '&resolution=' + this.resolution);
      this.applyHeader();
      this.setMsg(this.payload.bars?.length ? '' : 'No history yet.');
      await this.paint();
    } catch (err) {
      this.setMsg(err.message);
    }
  }

  applyHeader() {
    const sym = this.payload?.symbol || {};
    const price = this.live?.price ?? sym.price;
    const change = this.live?.change_d ?? sym.change_d;
    const bars = this.payload?.bars || [];
    const shown = price ?? (bars.length ? bars[bars.length - 1].close : null);
    this.querySelector('[data-display]').textContent = sym.display || '';
    this.querySelector('[data-name]').textContent = sym.name || '';
    this.querySelector('[data-price]').textContent = formatPrice(shown);
    const ch = this.querySelector('[data-change]');
    ch.textContent = formatPct(change);
    ch.className = 'chg ' + pctClass(change);
  }

  async loadAlerts() {
    try {
      const data = await this.api.get('/api/alerts');
      const mine = (data.alerts || []).filter((a) => a.symbol_id === this.symbolId);
      const host = this.querySelector('[data-alerts]');
      host.innerHTML = mine.map((a) => `
        <li>
          <span>${a.direction} ${formatPrice(a.level)} <span class="sub">${a.status}</span></span>
          <span>
            ${a.status === 'triggered' ? `<button type="button" class="mini" data-rearm="${a.id}">Rearm</button>` : ''}
            <button type="button" class="mini danger" data-delete-alert="${a.id}">Delete</button>
          </span>
        </li>`).join('');
    } catch (err) {
      /* the board banner reports this if the app action failed; a quiet chart still stands */
    }
  }

  async paint() {
    const bars = this.payload?.bars || [];
    if (!bars.length || this.hidden) return;
    let LW;
    try {
      LW = await loadCharts();
    } catch (err) {
      this.setMsg(err.message);
      return;
    }
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    if (this.hidden || !this.isConnected) return;
    try {
      this.draw(LW);
    } catch (err) {
      this.setMsg(err.message || 'Chart failed');
    }
  }

  draw(LW) {
    const host = this.querySelector('.chart-host');
    if (!this.chart) {
      this.chart = LW.createChart(host, {
        autoSize: true,
        layout: {
          background: { type: LW.ColorType.Solid, color: '#14160f' },
          textColor: '#9a937f',
          fontFamily: 'IBM Plex Sans, sans-serif',
        },
        grid: {
          vertLines: { color: 'rgba(232,224,204,0.06)' },
          horzLines: { color: 'rgba(232,224,204,0.06)' },
        },
        rightPriceScale: { borderColor: 'rgba(232,224,204,0.12)' },
        timeScale: { borderColor: 'rgba(232,224,204,0.12)', fixLeftEdge: true, fixRightEdge: true },
        crosshair: {
          vertLine: { color: 'rgba(198,161,91,0.5)', labelBackgroundColor: '#1c1f17' },
          horzLine: { color: 'rgba(198,161,91,0.5)', labelBackgroundColor: '#1c1f17' },
        },
      });
    }
    if (this.series) {
      this.chart.removeSeries(this.series);
      this.series = null;
    }
    const bars = this.payload?.bars || [];
    const last = bars[bars.length - 1].close;
    const format = priceFormat(last);
    if (this.kind === 'line') {
      this.series = this.chart.addSeries(LW.LineSeries, {
        color: '#c6a15b',
        lineWidth: 2,
        priceLineVisible: false,
        priceFormat: format,
      });
      this.series.setData(bars.map((b) => ({ time: b.time, value: b.close })));
    } else {
      this.series = this.chart.addSeries(LW.CandlestickSeries, {
        upColor: '#8eae8a',
        downColor: '#c56b52',
        borderVisible: false,
        wickUpColor: '#8eae8a',
        wickDownColor: '#c56b52',
        priceLineVisible: false,
        priceFormat: format,
      });
      this.series.setData(bars);
    }
    this.chart.timeScale().fitContent();
  }

  syncPressed() {
    for (const btn of this.querySelectorAll('[data-kind]')) btn.setAttribute('aria-pressed', btn.dataset.kind === this.kind ? 'true' : 'false');
    for (const btn of this.querySelectorAll('[data-res]')) btn.setAttribute('aria-pressed', btn.dataset.res === this.resolution ? 'true' : 'false');
    for (const btn of this.querySelectorAll('[data-dir]')) btn.setAttribute('aria-pressed', btn.dataset.dir === this.direction ? 'true' : 'false');
  }

  setMsg(text) {
    const el = this.querySelector('[data-msg]');
    if (el) el.textContent = text || '';
  }

  close() { this.destroyChart(); }

  destroyChart() {
    if (this.chart) {
      this.chart.remove();
      this.chart = null;
      this.series = null;
    }
  }

  disconnectedCallback() { this.destroyChart(); }
}

customElements.define('sw-chart', SwChart);
