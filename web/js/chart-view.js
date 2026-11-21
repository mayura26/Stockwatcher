import { crossLabel, formatPct, formatPrice, pctClass, priceFormat, priceTone, repeatLabel } from './format.js';

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
    this.repeat = 'once';
    this.mine = [];
    this.alertLines = [];
    this.previewPrice = null;
    this.previewLine = null;
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
          <div class="seg res" role="group" aria-label="Resolution">
            <button type="button" data-res="5m">5m</button>
            <button type="button" data-res="1H">1H</button>
            <button type="button" data-res="4H">4H</button>
            <button type="button" data-res="D" aria-pressed="true">D</button>
            <button type="button" data-res="W">W</button>
            <button type="button" data-res="M">M</button>
          </div>
        </div>
        <div class="chart-host"></div>
        <p class="chart-msg" data-msg></p>
        <form class="alert-form">
          <input name="level" inputmode="decimal" placeholder="Level" aria-label="Alert level">
          <div class="seg" role="group" aria-label="Repeat">
            <button type="button" data-repeat="once" aria-pressed="true">Once</button>
            <button type="button" data-repeat="continuous" aria-pressed="false">Continuous</button>
          </div>
          <div class="seg" role="group" aria-label="Direction">
            <button type="button" data-dir="above" aria-pressed="true">Cross up</button>
            <button type="button" data-dir="below" aria-pressed="false">Cross down</button>
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
        detail: { type: 'set-alert', symbolId: this.symbolId, direction: this.direction, repeat: this.repeat, level },
      }));
      event.target.reset();
      this.previewPrice = null;
      this.syncPreviewLine();
    });
    this.querySelector('input[name="level"]').addEventListener('input', (event) => {
      const level = Number(event.target.value);
      this.previewPrice = level > 0 ? level : null;
      this.syncPreviewLine();
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
      const repeat = event.target.closest('[data-repeat]');
      if (repeat) {
        this.repeat = repeat.dataset.repeat;
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
      this.repeat = 'once';
      this.previewPrice = null;
      this.mine = [];
      const level = this.querySelector('input[name="level"]');
      if (level) level.value = '';
      this.destroyChart();
      this.chartIntra = false;
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
      const intra = intradayRes(this.resolution);
      if (this.chart && this.chartIntra !== intra) this.destroyChart();
      this.chartIntra = intra;
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
    const px = this.querySelector('[data-price]');
    px.textContent = formatPrice(shown);
    px.className = 'px num ' + priceTone(change);
    const ch = this.querySelector('[data-change]');
    ch.textContent = formatPct(change);
    ch.className = 'chg ' + pctClass(change);
  }

  async loadAlerts() {
    try {
      const data = await this.api.get('/api/alerts');
      this.mine = (data.alerts || []).filter((a) => a.symbol_id === this.symbolId);
      const host = this.querySelector('[data-alerts]');
      host.innerHTML = this.mine.map((a) => `
        <li>
          <span>${crossLabel(a.direction)} ${formatPrice(a.level)} <span class="sub">${repeatLabel(a.repeat)} · ${a.status}</span></span>
          <span>
            ${a.status === 'triggered' && a.repeat !== 'continuous' ? `<button type="button" class="mini" data-rearm="${a.id}">Rearm</button>` : ''}
            <button type="button" class="mini danger" data-delete-alert="${a.id}">Delete</button>
          </span>
        </li>`).join('');
      this.drawAlertLines();
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
      this.lw = LW;
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
        timeScale: { borderColor: 'rgba(232,224,204,0.12)', rightOffset: 2, fixRightEdge: true },
        crosshair: {
          vertLine: { color: 'rgba(198,161,91,0.5)', labelBackgroundColor: '#1c1f17' },
          horzLine: { color: 'rgba(198,161,91,0.5)', labelBackgroundColor: '#1c1f17' },
        },
      });
      this.chart.subscribeCrosshairMove((param) => {
        const price = this.priceAt(param && param.point);
        if (price == null) return;
        this.rememberLevel(price, false);
      });
      this.chart.subscribeClick((param) => {
        const price = this.priceAt(param && param.point);
        if (price == null) return;
        this.rememberLevel(price, true);
      });
    }
    if (this.series) {
      this.chart.removeSeries(this.series);
      this.series = null;
      this.alertLines = [];
      this.previewLine = null;
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
    showWindow(this.chart, bars, this.resolution, this.payload?.symbol?.asset_class);
    this.drawAlertLines();
    this.syncPreviewLine();
  }

  priceAt(point) {
    if (!point || !this.series || typeof this.series.coordinateToPrice !== 'function') return null;
    const price = this.series.coordinateToPrice(point.y);
    if (price == null || !Number.isFinite(price) || price <= 0) return null;
    return price;
  }

  rememberLevel(price, force) {
    this.previewPrice = price;
    const input = this.querySelector('input[name="level"]');
    if (input && (force || document.activeElement !== input)) input.value = levelInput(price);
    this.syncPreviewLine();
  }

  drawAlertLines() {
    if (!this.series || !this.lw) return;
    for (const line of this.alertLines) this.series.removePriceLine(line);
    this.alertLines = [];
    const dashed = this.lw.LineStyle.Dashed;
    const solid = this.lw.LineStyle.Solid;
    for (const a of this.mine || []) {
      const up = a.direction !== 'below';
      this.alertLines.push(this.series.createPriceLine({
        price: a.level,
        color: up ? '#8eae8a' : '#c56b52',
        lineWidth: 1,
        lineStyle: a.repeat === 'continuous' ? dashed : solid,
        axisLabelVisible: true,
        title: up ? 'Up' : 'Down',
      }));
    }
  }

  syncPreviewLine() {
    if (!this.series || !this.lw) return;
    if (!(this.previewPrice > 0)) {
      if (this.previewLine) {
        this.series.removePriceLine(this.previewLine);
        this.previewLine = null;
      }
      return;
    }
    const opts = {
      price: this.previewPrice,
      color: '#c6a15b',
      lineWidth: 1,
      lineStyle: this.lw.LineStyle.Dashed,
      axisLabelVisible: true,
      title: 'Alert',
    };
    if (this.previewLine) this.previewLine.applyOptions(opts);
    else this.previewLine = this.series.createPriceLine(opts);
  }

  syncPressed() {
    for (const btn of this.querySelectorAll('[data-kind]')) btn.setAttribute('aria-pressed', btn.dataset.kind === this.kind ? 'true' : 'false');
    for (const btn of this.querySelectorAll('[data-res]')) btn.setAttribute('aria-pressed', btn.dataset.res === this.resolution ? 'true' : 'false');
    for (const btn of this.querySelectorAll('[data-dir]')) btn.setAttribute('aria-pressed', btn.dataset.dir === this.direction ? 'true' : 'false');
    for (const btn of this.querySelectorAll('[data-repeat]')) btn.setAttribute('aria-pressed', btn.dataset.repeat === this.repeat ? 'true' : 'false');
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
      this.alertLines = [];
      this.previewLine = null;
    }
  }

  disconnectedCallback() { this.destroyChart(); }
}

function intradayRes(res) {
  return res === '5m' || res === '1H' || res === '4H';
}

// Days of history shown when a resolution opens. Older bars stay loaded and can be scrolled in.
const WINDOW_DAYS = { '5m': 1, '1H': 7, '4H': 31, D: 365, W: 365 * 5, M: 365 * 10 };

function showWindow(chart, bars, resolution, assetClass) {
  const width = chart.timeScale().width();
  if (!width) {
    requestAnimationFrame(() => showWindow(chart, bars, resolution, assetClass));
    return;
  }
  const count = Math.min(bars.length, windowCount(bars, resolution, assetClass));
  const spacing = Math.min(14, Math.max(1, (width - 4) / count));
  chart.timeScale().applyOptions({ barSpacing: spacing });
  const last = bars.length - 1;
  chart.timeScale().setVisibleLogicalRange({ from: last - count + 1, to: last });
}

function windowCount(bars, resolution, assetClass) {
  const last = bars[bars.length - 1].time;
  if (resolution === '5m' && assetClass !== 'crypto' && typeof last === 'number') {
    const day = nyDay(last);
    let n = 0;
    for (let i = bars.length - 1; i >= 0; i--) {
      if (nyDay(bars[i].time) !== day) break;
      n++;
    }
    return Math.max(n, 1);
  }
  const days = WINDOW_DAYS[resolution] || 365;
  const cutoff = typeof last === 'number' ? last - days * 86400 : shiftDay(last, -days);
  let n = 0;
  for (let i = bars.length - 1; i >= 0; i--) {
    if (bars[i].time < cutoff) break;
    n++;
  }
  return Math.max(n, 1);
}

function nyDay(unix) {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'America/New_York',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date(unix * 1000));
}

function shiftDay(day, delta) {
  const t = new Date(day + 'T00:00:00Z');
  t.setUTCDate(t.getUTCDate() + delta);
  return t.toISOString().slice(0, 10);
}

function levelInput(n) {
  const v = Number(n);
  if (!Number.isFinite(v) || v <= 0) return '';
  const abs = Math.abs(v);
  let digits = 2;
  if (abs < 1) digits = abs < 0.01 ? 6 : 4;
  return String(Number(v.toFixed(digits)));
}

customElements.define('sw-chart', SwChart);
