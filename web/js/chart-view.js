import { alertMarks, formatPct, formatPrice, pctClass, priceFormat, priceTone, yahooQuoted } from './format.js';

const RANGE_KEY = 'sw.range';
const MA_KEY = 'sw.ma.visible';
const RANGES = ['D', '3D', '1W', '1M', '3M', '6M', 'YTD', '1Y', '5Y'];

function savedMA() {
  const raw = localStorage.getItem(MA_KEY);
  if (raw == null) return new Set([1, 2]);
  const on = new Set();
  for (const part of raw.split(',')) {
    const n = Number(part);
    if (n === 1 || n === 2 || n === 3) on.add(n);
  }
  return on;
}

function storeMA(on) {
  localStorage.setItem(MA_KEY, [...on].sort((a, b) => a - b).join(','));
}

function savedRange() {
  const saved = localStorage.getItem(RANGE_KEY) || localStorage.getItem('sw.resolution');
  if (saved === 'W') return '1W';
  if (saved === 'M') return '1M';
  return RANGES.includes(saved) ? saved : 'D';
}

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
    this.range = savedRange();
    this.kind = 'candle';
    this.direction = 'above';
    this.repeat = 'once';
    this.mine = [];
    this.alertLines = [];
    this.maSeries = [];
    this.maOn = savedMA();
    this.previewPrice = null;
    this.previewLine = null;
    this.userRanged = false;
    this.applyingWindow = false;
    this.innerHTML = `
      <div class="chart-screen">
        <div class="chart-id">
          <div class="ident">
            <div class="sym-line">
              <div class="sym"><span data-display></span><span class="live-dot" data-feed hidden role="img"></span><span class="yahoo-mark" data-yahoo hidden>Yahoo</span></div>
              <div class="seg chart-kind" role="group" aria-label="Chart style">
                <button type="button" data-kind="line">Line</button>
                <button type="button" data-kind="candle" aria-pressed="true">Candle</button>
              </div>
              <button type="button" class="text-btn chart-alert-btn" data-alert-open>Alerts</button>
            </div>
            <div class="sub" data-name></div>
            <div class="ma-toggles"></div>
          </div>
          <div class="px-block">
            <div class="px num" data-price></div>
            <div class="day-ext" data-ext hidden></div>
            <div class="px-change">
              <div class="chg" data-change></div>
              <span class="chg-h" data-span></span>
            </div>
          </div>
        </div>
        <div class="toggles">
          <div class="range-scroll">
            <div class="seg res" role="group" aria-label="Range">
              ${RANGES.map((range) => `<button type="button" data-range="${range}"${range === 'D' ? ' aria-pressed="true"' : ''}>${range}</button>`).join('')}
            </div>
          </div>
        </div>
        <div class="chart-stage">
          <div class="chart-host"></div>
          <button type="button" class="chart-add" data-chart-add hidden aria-label="Add alert">+</button>
          <div class="chart-load" data-load hidden><span></span></div>
        </div>
        <p class="chart-msg" data-msg></p>
        <div class="chart-alert-mask" data-alert-sheet hidden>
          <div class="search-sheet" role="dialog" aria-label="Alerts">
            <div class="search-head">
              <div class="sheet-title">Alerts</div>
              <button type="button" class="text-btn" data-alert-close>Close</button>
            </div>
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
            <form class="alert-form bracket-form" data-bracket>
              <input name="percent" inputmode="decimal" placeholder="%" aria-label="Bracket percent">
              <button type="submit" class="text-btn">Bracket</button>
            </form>
            <ul class="alert-rows alert-mini" data-alerts></ul>
          </div>
        </div>
      </div>`;
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
    this.querySelector('[data-bracket]').addEventListener('submit', (event) => {
      event.preventDefault();
      const percent = Number(new FormData(event.target).get('percent'));
      if (!(percent > 0) || percent >= 100) {
        this.setMsg('Enter a percent between 0 and 100.');
        return;
      }
      this.setMsg('');
      this.dispatchEvent(new CustomEvent('sw-action', {
        bubbles: true,
        detail: { type: 'set-bracket', symbolId: this.symbolId, percent },
      }));
      event.target.reset();
    });
    this.querySelector('input[name="level"]').addEventListener('input', (event) => {
      const level = Number(event.target.value);
      this.previewPrice = level > 0 ? level : null;
      this.syncPreviewLine();
    });
    this.addEventListener('click', (event) => {
      if (event.target.closest('[data-chart-add]')) {
        this.addFromChart();
        return;
      }
      if (event.target.closest('[data-alert-open]')) {
        this.openAlertSheet();
        return;
      }
      if (event.target.closest('[data-alert-close]') || event.target.matches('[data-alert-sheet]')) {
        this.closeAlertSheet();
        return;
      }
      const ma = event.target.closest('[data-ma]');
      if (ma) {
        this.toggleMA(Number(ma.dataset.ma));
        return;
      }
      const kind = event.target.closest('[data-kind]');
      if (kind && kind.dataset.kind !== this.kind) {
        this.kind = kind.dataset.kind;
        this.syncPressed();
        this.paint();
      }
      const res = event.target.closest('[data-range]');
      if (res && res.dataset.range !== this.range) {
        this.range = res.dataset.range;
        localStorage.setItem(RANGE_KEY, this.range);
        this.userRanged = false;
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
    this.syncPressed();
    this.observePlot();
  }

  async open(id) {
    const changed = this.symbolId !== id;
    this.symbolId = id;
    if (changed) {
      this.payload = null;
      this.kind = 'candle';
      this.direction = 'above';
      this.repeat = 'once';
      this.previewPrice = null;
      this.mine = [];
      const level = this.querySelector('input[name="level"]');
      if (level) level.value = '';
      const percent = this.querySelector('[data-bracket] input');
      if (percent) percent.value = '';
      this.destroyChart();
      this.chartIntra = false;
      this.userRanged = false;
      this.syncPressed();
    }
    if (!this.payload || this.payload.range !== this.range) await this.loadBars();
    else if (this.foldStale()) await this.loadBars(true);
    else {
      this.applyHeader();
      await this.paint();
    }
    await this.loadAlerts();
  }

  setLive(row) {
    this.live = row || null;
    if (this.payload) this.applyHeader();
    this.placeAddButton();
  }

  async loadBars(quiet) {
    const width = this.plotWidth();
    const token = (this.loadToken = (this.loadToken || 0) + 1);
    if (!quiet) {
      this.setLoading(true);
      this.setMsg('Loading history…');
    }
    try {
      let path = '/api/bars?id=' + this.symbolId + '&range=' + this.range;
      if (width > 0) path += '&w=' + width;
      const payload = await this.api.get(path);
      if (token !== this.loadToken || !this.isConnected) return;
      if (quiet && this.payload && payload.range === this.range && payload.candle === this.payload.candle) {
        this.sentWidth = width;
        return;
      }
      this.sentWidth = width;
      if (quiet) this.userRanged = false;
      this.payload = payload;
      const intra = intradayRange(this.range);
      if (this.chart && this.chartIntra !== intra) this.destroyChart();
      this.chartIntra = intra;
      this.applyHeader();
      this.setMsg(this.payload.bars?.length ? '' : 'No history yet.');
      await this.paint();
      this.scheduleFoldCheck();
    } catch (err) {
      if (token === this.loadToken) this.setMsg(err.message);
    } finally {
      if (token === this.loadToken) this.setLoading(false);
    }
  }

  applyHeader() {
    const sym = this.payload?.symbol || {};
    const source = this.live || sym;
    const price = this.live?.price ?? sym.price;
    const change = this.periodChange();
    const bars = this.payload?.bars || [];
    const shown = price ?? (bars.length ? bars[bars.length - 1].close : null);
    this.querySelector('[data-display]').textContent = sym.display || '';
    const feed = this.live ? (this.live.feed || '') : (sym.feed || '');
    const dot = this.querySelector('[data-feed]');
    if (dot) {
      const on = feed === 'live' || feed === 'delayed';
      dot.hidden = !on;
      dot.className = 'live-dot' + (on ? ' ' + feed : '');
      if (on) dot.setAttribute('aria-label', feed === 'live' ? 'Live' : 'Delayed');
      else dot.removeAttribute('aria-label');
    }
    const yahoo = this.querySelector('[data-yahoo]');
    if (yahoo) yahoo.hidden = !yahooQuoted(sym);
    this.querySelector('[data-name]').textContent = sym.name || '';
    this.paintMA(this.payload?.averages || []);
    const px = this.querySelector('[data-price]');
    px.textContent = formatPrice(shown);
    px.className = 'px num ' + priceTone(change);
    const ch = this.querySelector('[data-change]');
    ch.textContent = formatPct(change);
    ch.className = 'chg ' + pctClass(change);
    const span = this.querySelector('[data-span]');
    if (span) span.textContent = spanLabel(this.range, this.payload?.candle);
    const ext = this.querySelector('[data-ext]');
    const label = source.ext_session === 'pre' ? 'Pre' : source.ext_session === 'post' ? 'After' : '';
    if (label && source.ext_pct != null) {
      ext.hidden = false;
      ext.innerHTML = `<span class="chg-h">${label}</span><span class="chg ${pctClass(source.ext_pct)}">${formatPct(source.ext_pct)}</span>`;
    } else {
      ext.hidden = true;
      ext.textContent = '';
    }
  }

  periodChange() {
    const sym = this.payload?.symbol || {};
    const price = this.live?.price ?? sym.price;
    if (this.range === 'D') {
      const day = this.live?.change_d ?? sym.change_d;
      if (day != null) return day;
    }
    const base = Number(this.payload?.change_base);
    if (base > 0 && price > 0) return (price / base - 1) * 100;
    return this.payload?.change ?? null;
  }

  async loadAlerts() {
    try {
      const data = await this.api.get('/api/alerts');
      this.mine = (data.alerts || []).filter((a) => a.symbol_id === this.symbolId);
      const host = this.querySelector('[data-alerts]');
      host.innerHTML = this.mine.length ? this.mine.map((a) => `
        <li class="${a.status === 'triggered' ? 'is-triggered' : ''}">
          ${alertMarks(a)}
          <span>
            ${a.status === 'triggered' && a.repeat !== 'continuous' ? `<button type="button" class="mini" data-rearm="${a.id}">Rearm</button>` : ''}
            <button type="button" class="mini danger" data-delete-alert="${a.id}">Delete</button>
          </span>
        </li>`).join('') : '<li class="alert-empty"><span class="sub">No alerts on this symbol.</span></li>';
      const open = this.querySelector('[data-alert-open]');
      if (open) open.innerHTML = this.mine.length ? 'Alerts<span class="dot" aria-label="alerts on this symbol"></span>' : 'Alerts';
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
        localization: { timeFormatter: (time) => crosshairLabel(time) },
        timeScale: {
          borderColor: 'rgba(232,224,204,0.12)',
          rightOffset: 2,
          fixRightEdge: true,
          secondsVisible: false,
          timeVisible: intradayRange(this.range),
          tickMarkFormatter: (time, type) => tickLabel(time, type, LW.TickMarkType),
        },
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
      this.bindTimeScale();
    }
    const kept = this.userRanged ? this.chart.timeScale().getVisibleLogicalRange() : null;
    const bars = this.payload?.bars || [];
    const last = bars[bars.length - 1].close;
    const format = priceFormat(last);
    this.withWindow(() => {
      this.clearAverages();
      if (this.series) {
        this.chart.removeSeries(this.series);
        this.series = null;
        this.alertLines = [];
        this.previewLine = null;
      }
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
      this.chart.timeScale().applyOptions({
        timeVisible: intradayRange(this.range),
        secondsVisible: false,
      });
      if (kept) this.chart.timeScale().setVisibleLogicalRange(kept);
      this.addAverages(LW, format);
    });
    if (!kept) this.fitWindow();
    this.drawAlertLines();
    this.syncPreviewLine();
  }

  paintMA(lines) {
    const host = this.querySelector('.ma-toggles');
    if (!host) return;
    const next = lines || [];
    const key = next.map((line) => `${line.slot}:${line.kind}:${line.period}:${line.color}`).join('|');
    if (host.dataset.key !== key) {
      host.dataset.key = key;
      host.innerHTML = next.map((line) => {
        const label = `${line.period} ${String(line.kind || '').toUpperCase()}`;
        return `<button type="button" class="ma-chip" data-ma="${line.slot}"><span class="ma-swatch" style="background:${line.color}"></span>${label}</button>`;
      }).join('');
    }
    this.syncPressed();
  }

  toggleMA(slot) {
    if (slot !== 1 && slot !== 2 && slot !== 3) return;
    if (this.maOn.has(slot)) this.maOn.delete(slot);
    else this.maOn.add(slot);
    storeMA(this.maOn);
    this.syncPressed();
    this.redrawAverages();
  }

  redrawAverages() {
    if (!this.chart || !this.lw) return;
    const bars = this.payload?.bars || [];
    if (!bars.length) return;
    const format = priceFormat(bars[bars.length - 1].close);
    this.withWindow(() => {
      this.clearAverages();
      this.addAverages(this.lw, format);
    });
  }

  clearAverages() {
    if (this.chart) {
      for (const series of this.maSeries || []) this.chart.removeSeries(series);
    }
    this.maSeries = [];
  }

  addAverages(LW, format) {
    if (!this.chart) return;
    for (const line of this.payload?.averages || []) {
      if (!this.maOn.has(line.slot)) continue;
      const data = (line.points || []).filter((point) => point && point.value != null && point.time != null);
      if (!data.length) continue;
      const series = this.chart.addSeries(LW.LineSeries, {
        color: line.color,
        lineWidth: 1,
        priceLineVisible: false,
        lastValueVisible: false,
        crosshairMarkerVisible: false,
        priceFormat: format,
      });
      series.setData(data.map((point) => ({ time: point.time, value: point.value })));
      this.maSeries.push(series);
    }
  }

  openAlertSheet() {
    const mask = this.querySelector('[data-alert-sheet]');
    if (mask) mask.hidden = false;
  }

  closeAlertSheet() {
    const mask = this.querySelector('[data-alert-sheet]');
    if (mask) mask.hidden = true;
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
    for (const a of this.mine || []) {
      const up = a.direction !== 'below';
      this.alertLines.push(this.series.createPriceLine({
        price: a.level,
        color: up ? 'rgba(142, 174, 138, 0.45)' : 'rgba(197, 107, 82, 0.45)',
        lineWidth: 1,
        lineStyle: dashed,
        axisLabelVisible: true,
        title: '',
      }));
    }
  }

  currentPrice() {
    const sym = this.payload?.symbol || {};
    const price = this.live?.price ?? sym.price;
    if (price > 0) return Number(price);
    const bars = this.payload?.bars || [];
    const close = bars.length ? bars[bars.length - 1].close : null;
    return close > 0 ? Number(close) : null;
  }

  addFromChart() {
    const level = Number(levelInput(this.previewPrice));
    if (!(level > 0)) return;
    const now = Number(levelInput(this.currentPrice()));
    if (!(now > 0)) {
      this.setMsg('No current price yet.');
      return;
    }
    if (level === now) {
      this.setMsg('That level is the current price.');
      return;
    }
    const direction = level > now ? 'above' : 'below';
    this.setMsg('');
    this.dispatchEvent(new CustomEvent('sw-action', {
      bubbles: true,
      detail: { type: 'set-alert', symbolId: this.symbolId, direction, repeat: this.repeat, level },
    }));
    this.previewPrice = null;
    const input = this.querySelector('input[name="level"]');
    if (input) input.value = '';
    this.syncPreviewLine();
  }

  placeAddButton() {
    const btn = this.querySelector('[data-chart-add]');
    if (!btn) return;
    const level = Number(levelInput(this.previewPrice));
    const y = this.series && level > 0 && typeof this.series.priceToCoordinate === 'function'
      ? this.series.priceToCoordinate(level)
      : null;
    if (y == null || !Number.isFinite(y)) {
      btn.hidden = true;
      if (level > 0 && this.chart && this.isConnected && !this.hidden && !this.chart.timeScale().width()) {
        requestAnimationFrame(() => this.placeAddButton());
      }
      return;
    }
    const now = Number(levelInput(this.currentPrice()));
    const down = now > 0 && level < now;
    const up = now > 0 && level > now;
    btn.hidden = false;
    btn.classList.toggle('up', up);
    btn.classList.toggle('down', down);
    const name = down ? 'Cross down' : up ? 'Cross up' : 'Level is the current price';
    btn.setAttribute('aria-label', up || down ? `${name} at ${formatPrice(level)}` : name);
    let scale = 64;
    try {
      const width = this.chart.priceScale('right').width();
      if (width > 0) scale = width;
    } catch (err) {
      /* the right scale is the usual place for the button */
    }
    btn.style.top = `${y}px`;
    btn.style.right = `${scale + 4}px`;
  }

  syncPreviewLine() {
    this.placeAddButton();
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
      color: 'rgba(198, 161, 91, 0.55)',
      lineWidth: 1,
      lineStyle: this.lw.LineStyle.Dashed,
      axisLabelVisible: true,
      title: '',
    };
    if (this.previewLine) this.previewLine.applyOptions(opts);
    else this.previewLine = this.series.createPriceLine(opts);
  }

  syncPressed() {
    for (const btn of this.querySelectorAll('[data-kind]')) btn.setAttribute('aria-pressed', btn.dataset.kind === this.kind ? 'true' : 'false');
    for (const btn of this.querySelectorAll('[data-ma]')) btn.setAttribute('aria-pressed', this.maOn.has(Number(btn.dataset.ma)) ? 'true' : 'false');
    for (const btn of this.querySelectorAll('[data-range]')) btn.setAttribute('aria-pressed', btn.dataset.range === this.range ? 'true' : 'false');
    for (const btn of this.querySelectorAll('[data-dir]')) btn.setAttribute('aria-pressed', btn.dataset.dir === this.direction ? 'true' : 'false');
    for (const btn of this.querySelectorAll('[data-repeat]')) btn.setAttribute('aria-pressed', btn.dataset.repeat === this.repeat ? 'true' : 'false');
  }

  setMsg(text) {
    const el = this.querySelector('[data-msg]');
    if (el) el.textContent = text || '';
  }

  setLoading(on) {
    const el = this.querySelector('[data-load]');
    if (el) el.hidden = !on;
    const stage = this.querySelector('.chart-stage');
    if (stage) stage.setAttribute('aria-busy', on ? 'true' : 'false');
  }

  bindTimeScale() {
    const scale = this.chart.timeScale();
    scale.subscribeVisibleLogicalRangeChange(() => {
      this.placeAddButton();
      if (this.applyingWindow || this.holdingRange) return;
      this.rangeChanged = true;
      queueMicrotask(() => this.settleRange());
    });
    scale.subscribeSizeChange(() => {
      this.holdRange();
      this.sizeChanged = true;
      queueMicrotask(() => this.settleRange());
    });
  }

  // A resize notifies the visible range on a later frame. Ignore that so it does not count as a zoom.
  holdRange() {
    const token = (this.holdToken = (this.holdToken || 0) + 1);
    this.holdingRange = true;
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (this.holdToken === token) this.holdingRange = false;
    }));
  }

  settleRange() {
    if (this.applyingWindow) return;
    const sized = this.sizeChanged;
    const ranged = this.rangeChanged;
    if (!sized && !ranged) return;
    this.sizeChanged = false;
    this.rangeChanged = false;
    if (ranged && !sized) {
      this.userRanged = true;
      this.placeAddButton();
      return;
    }
    if (!this.userRanged) this.fitWindow();
    else this.placeAddButton();
  }

  withWindow(fn) {
    this.applyingWindow = true;
    try {
      fn();
    } finally {
      this.applyingWindow = false;
    }
  }

  fitWindow() {
    if (!this.chart || this.hidden || !this.isConnected) return;
    const bars = this.payload?.bars || [];
    if (!bars.length) return;
    if (!this.chart.timeScale().width()) {
      requestAnimationFrame(() => this.fitWindow());
      return;
    }
    this.holdRange();
    this.withWindow(() => this.chart.timeScale().fitContent());
    this.rangeChanged = false;
    this.sizeChanged = false;
    this.placeAddButton();
  }

  close() {
    this.closeAlertSheet();
    this.destroyChart();
  }

  destroyChart() {
    if (this.chart) {
      this.chart.remove();
      this.chart = null;
      this.series = null;
      this.alertLines = [];
      this.previewLine = null;
      this.maSeries = [];
    }
    this.placeAddButton();
  }

  plotWidth() {
    const host = this.querySelector('.chart-host');
    const hostWidth = host ? host.clientWidth : 0;
    if (!hostWidth) return 0;
    let scale = 64;
    try {
      const width = this.chart && this.chart.priceScale('right').width();
      if (width > 0) scale = width;
    } catch (err) {
      /* the scale appears once the chart has painted */
    }
    return Math.max(0, Math.round(hostWidth - scale));
  }

  foldStale() {
    if (!longRange(this.range)) return false;
    const width = this.plotWidth();
    return width > 0 && width !== this.sentWidth;
  }

  observePlot() {
    const host = this.querySelector('.chart-host');
    if (!host || typeof ResizeObserver === 'undefined') return;
    this.plotObserver = new ResizeObserver(() => this.scheduleFoldCheck());
    this.plotObserver.observe(host);
  }

  scheduleFoldCheck() {
    if (!longRange(this.range) || this.hidden) return;
    clearTimeout(this.plotTimer);
    this.plotTimer = setTimeout(() => {
      if (!this.isConnected || !this.symbolId || !longRange(this.range)) return;
      const width = this.plotWidth();
      if (width > 0 && width !== this.sentWidth) this.loadBars(true);
    }, 200);
  }

  disconnectedCallback() {
    clearTimeout(this.plotTimer);
    if (this.plotObserver) this.plotObserver.disconnect();
    this.destroyChart();
  }
}

function intradayRange(range) {
  return range === 'D' || range === '3D' || range === '1W' || range === '1M';
}

function longRange(range) {
  return range === '6M' || range === 'YTD' || range === '1Y' || range === '5Y';
}

function spanLabel(range, candle) {
  const name = candle === '3S' ? '3-day' : candle === 'W' ? 'Week' : candle === 'M' ? 'Month' : '';
  if (!name) return range || '';
  return `${range} · ${name}`;
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

function tickLabel(time, type, marks) {
  const parts = timeParts(time);
  if (!parts) return null;
  const month = MONTHS[parts.month - 1] || '';
  if (type === marks.Year) return String(parts.year);
  if (type === marks.Month) return parts.month === 1 ? `Jan ${parts.year}` : month;
  if (type === marks.Time || type === marks.TimeWithSeconds) {
    return parts.intraday ? clock(parts) : `${parts.day} ${month}`;
  }
  return `${parts.day} ${month}`;
}

function crosshairLabel(time) {
  const parts = timeParts(time);
  if (!parts) return '';
  const month = MONTHS[parts.month - 1] || '';
  if (parts.intraday) return `${parts.day} ${month} ${clock(parts)}`;
  return `${parts.day} ${month} ${parts.year}`;
}

function timeParts(time) {
  if (typeof time === 'number' && Number.isFinite(time)) {
    const date = new Date(time * 1000);
    return {
      year: date.getFullYear(),
      month: date.getMonth() + 1,
      day: date.getDate(),
      hour: date.getHours(),
      minute: date.getMinutes(),
      intraday: true,
    };
  }
  if (time && typeof time === 'object' && time.year) {
    return { year: time.year, month: time.month, day: time.day, intraday: false };
  }
  if (typeof time !== 'string') return null;
  const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(time);
  if (!match) return null;
  return { year: Number(match[1]), month: Number(match[2]), day: Number(match[3]), intraday: false };
}

function clock(parts) {
  return `${String(parts.hour).padStart(2, '0')}:${String(parts.minute).padStart(2, '0')}`;
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
