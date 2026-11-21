class SwSearch extends HTMLElement {
  connectedCallback() {
    this.className = '';
    this.assetClass = 'stock';
    this.innerHTML = `
      <div class="search-sheet" role="dialog" aria-label="Add symbol">
        <div class="search-head">
          <div class="seg" role="group" aria-label="Asset class">
            <button type="button" data-class="stock" aria-pressed="true">Stocks</button>
            <button type="button" data-class="future" aria-pressed="false">Futures</button>
            <button type="button" data-class="index" aria-pressed="false">Indexes</button>
            <button type="button" data-class="crypto" aria-pressed="false">Crypto</button>
          </div>
          <button type="button" class="text-btn" data-close>Close</button>
        </div>
        <div class="sheet-pad"><input type="search" placeholder="Symbol or name" aria-label="Search" enterkeyhint="search"></div>
        <p class="hint"></p>
        <div class="results"></div>
      </div>`;
    this.querySelector('[data-close]').addEventListener('click', () => this.emit({ type: 'close-search' }));
    this.addEventListener('click', (event) => {
      if (event.target === this) this.emit({ type: 'close-search' });
      const cls = event.target.closest('[data-class]');
      if (cls) {
        this.assetClass = cls.dataset.class;
        this.syncPressed();
        this.load();
      }
      const hit = event.target.closest('[data-hit]');
      if (hit) this.emit({ type: 'add', hit: JSON.parse(hit.dataset.hit) });
    });
    this.querySelector('input').addEventListener('input', () => {
      clearTimeout(this.timer);
      this.timer = setTimeout(() => this.load(), 180);
    });
  }

  open() {
    this.hidden = false;
    const input = this.querySelector('input');
    input.value = '';
    this.syncPressed();
    this.load();
    input.focus();
  }

  syncPressed() {
    for (const btn of this.querySelectorAll('[data-class]')) {
      btn.setAttribute('aria-pressed', btn.dataset.class === this.assetClass ? 'true' : 'false');
    }
  }

  async load() {
    const q = this.querySelector('input').value.trim();
    const hint = this.querySelector('.hint');
    const results = this.querySelector('.results');
    if (this.assetClass === 'stock' && q.length < 1) {
      hint.textContent = 'Type a ticker or company. AAPL works even if search is quiet.';
      results.innerHTML = '';
      return;
    }
    hint.textContent = '';
    results.innerHTML = '';
    try {
      const data = await this.api.get('/api/search?class=' + this.assetClass + '&q=' + encodeURIComponent(q));
      const hits = data.hits || [];
      if (!hits.length) {
        hint.textContent = 'No matches.';
        return;
      }
      results.innerHTML = hits.map((hit) => `
        <button type="button" class="result" data-hit="${escapeAttr(JSON.stringify(hit))}">
          <span class="sym">${escapeHTML(hit.display)}${yahooMark(hit)}</span>
          <span class="sub">${escapeHTML(hit.name || hit.provider_symbol)}</span>
        </button>`).join('');
    } catch (err) {
      hint.textContent = err.message;
    }
  }

  emit(detail) {
    this.dispatchEvent(new CustomEvent('sw-action', { bubbles: true, detail }));
  }
}

function yahooMark(hit) {
  if (!yahooQuoted(hit)) return '';
  return '<span class="yahoo-mark">Yahoo</span>';
}

function yahooQuoted(row) {
  if (!row || row.asset_class !== 'stock') return false;
  const provider = String(row.provider_symbol || '');
  return provider.startsWith('^') || provider.includes('.');
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
function escapeAttr(s) { return escapeHTML(s); }

customElements.define('sw-search', SwSearch);
