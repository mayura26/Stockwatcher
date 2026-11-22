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

const DEFAULTS = [
  { slot: 1, kind: 'ema', period: 21, color: '#6e92d4' },
  { slot: 2, kind: 'ema', period: 50, color: '#5eaea4' },
  { slot: 3, kind: 'sma', period: 200, color: '#b196cc' },
];

function swatches(slot) {
  return COLORS.map((c) => `<button type="button" class="swatch" data-color="${c.value}" style="background:${c.value}" aria-label="Line ${slot} ${c.label}" aria-pressed="false"></button>`).join('');
}

function row(line) {
  return `
    <fieldset class="avg-row" data-slot="${line.slot}" data-kind="${line.kind}" data-color="${line.color}">
      <legend>Line ${line.slot}</legend>
      <label class="field">Period
        <input name="period" inputmode="numeric" value="${line.period}" aria-label="Line ${line.slot} period" autocomplete="off">
      </label>
      <div class="seg" role="group" aria-label="Line ${line.slot} type">
        <button type="button" data-kind="ema">EMA</button>
        <button type="button" data-kind="sma">SMA</button>
      </div>
      <div>
        <div class="pick-label">Color</div>
        <div class="swatches" role="group" aria-label="Line ${line.slot} color">${swatches(line.slot)}</div>
      </div>
    </fieldset>`;
}

class SwSettings extends HTMLElement {
  connectedCallback() {
    this.innerHTML = `
      <div class="settings-screen">
        <div class="sheet-pad">
          <p class="note">Three daily averages. A 21-day line stays on the daily series, including when the chart is showing 15-minute candles.</p>
          <form data-averages>
            ${DEFAULTS.map(row).join('')}
            <button type="submit" class="btn">Save</button>
            <p class="sub" data-save-state></p>
          </form>
        </div>
      </div>`;
    this.syncRows();
    this.querySelector('form').addEventListener('submit', (event) => {
      event.preventDefault();
      this.save();
    });
    this.addEventListener('click', (event) => {
      const slot = event.target.closest('[data-slot]');
      if (!slot) return;
      const kind = event.target.closest('[data-kind]');
      if (kind) {
        slot.dataset.kind = kind.dataset.kind;
        this.syncRows();
        return;
      }
      const color = event.target.closest('[data-color]');
      if (color) {
        slot.dataset.color = color.dataset.color;
        this.syncRows();
      }
    });
  }

  async load() {
    if (!this.api) return;
    const state = this.querySelector('[data-save-state]');
    try {
      const data = await this.api.get('/api/averages');
      this.apply(data.lines);
      if (state) state.textContent = '';
    } catch (err) {
      this.emit({ type: 'error', message: err.message });
    }
  }

  apply(lines) {
    const bySlot = new Map((lines || []).map((line) => [line.slot, line]));
    for (const row of this.querySelectorAll('[data-slot]')) {
      const line = bySlot.get(Number(row.dataset.slot));
      if (!line) continue;
      row.dataset.kind = line.kind === 'sma' ? 'sma' : 'ema';
      row.dataset.color = line.color || row.dataset.color;
      const input = row.querySelector('[name="period"]');
      if (input) input.value = line.period;
    }
    this.syncRows();
  }

  syncRows() {
    for (const row of this.querySelectorAll('[data-slot]')) {
      for (const btn of row.querySelectorAll('[data-kind]')) {
        btn.setAttribute('aria-pressed', btn.dataset.kind === row.dataset.kind ? 'true' : 'false');
      }
      for (const btn of row.querySelectorAll('[data-color]')) {
        btn.setAttribute('aria-pressed', btn.dataset.color === row.dataset.color ? 'true' : 'false');
      }
    }
  }

  async save() {
    const lines = [...this.querySelectorAll('[data-slot]')].map((row) => ({
      slot: Number(row.dataset.slot),
      kind: row.dataset.kind,
      period: Number(row.querySelector('[name="period"]').value),
      color: row.dataset.color,
    }));
    for (const line of lines) {
      if (!Number.isInteger(line.period) || line.period < 2 || line.period > 400) {
        this.emit({ type: 'error', message: 'Period must be between 2 and 400.' });
        return;
      }
    }
    const button = this.querySelector('button[type="submit"]');
    const state = this.querySelector('[data-save-state]');
    button.disabled = true;
    if (state) state.textContent = 'Saving…';
    try {
      const data = await this.api.put('/api/averages', { lines });
      this.apply(data.lines);
      if (state) state.textContent = 'Saved';
      this.emit({ type: 'averages-saved' });
    } catch (err) {
      if (state) state.textContent = '';
      this.emit({ type: 'error', message: err.message });
    } finally {
      button.disabled = false;
    }
  }

  emit(detail) {
    this.dispatchEvent(new CustomEvent('sw-action', { bubbles: true, detail }));
  }
}

customElements.define('sw-settings', SwSettings);
