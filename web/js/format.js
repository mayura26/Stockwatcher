export function yahooQuoted(row) {
  if (!row || row.asset_class !== 'stock') return false;
  const provider = String(row.provider_symbol || '');
  return provider.startsWith('^') || provider.includes('.');
}

export function formatPrice(n) {
  if (n == null || Number.isNaN(Number(n))) return '—';
  const v = Number(n);
  const abs = Math.abs(v);
  let digits = 2;
  if (abs > 0 && abs < 1) digits = abs < 0.01 ? 6 : 4;
  return v.toLocaleString('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

export function formatPct(n) {
  if (n == null || Number.isNaN(Number(n))) return '—';
  const v = Number(n);
  const sign = v > 0 ? '+' : '';
  return sign + v.toFixed(2) + '%';
}

export function pctClass(n) {
  if (n == null || Number(n) === 0) return 'flat';
  return Number(n) > 0 ? 'up' : 'down';
}

export function priceTone(n) {
  const tone = pctClass(n);
  return tone === 'flat' ? '' : tone;
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

export function formatCompact(n) {
  if (n == null || Number.isNaN(Number(n))) return '';
  const v = Number(n);
  const sign = v < 0 ? '-' : '';
  const abs = Math.abs(v);
  const units = [[1e12, 'T'], [1e9, 'B'], [1e6, 'M']];
  for (const [scale, suffix] of units) {
    if (abs >= scale) {
      const scaled = abs / scale;
      let body = scaled >= 100 ? scaled.toFixed(0) : scaled.toFixed(1);
      if (body.endsWith('.0')) body = body.slice(0, -2);
      return sign + body + suffix;
    }
  }
  return sign + formatPrice(abs);
}

export function formatPE(n) {
  if (n == null || !(Number(n) > 0)) return '';
  return Number(n).toFixed(1);
}

export function formatDay(iso) {
  if (!iso || !/^\d{4}-\d{2}-\d{2}$/.test(iso)) return '';
  const [y, m, d] = iso.split('-').map(Number);
  if (m < 1 || m > 12 || d < 1 || d > 31) return '';
  let text = d + ' ' + MONTHS[m - 1];
  if (y !== new Date().getFullYear()) text += ' ' + String(y).slice(2);
  return text;
}

export function formatClock(unix) {
  if (!unix) return '';
  const d = new Date(unix * 1000);
  const h = String(d.getHours()).padStart(2, '0');
  const m = String(d.getMinutes()).padStart(2, '0');
  return h + ':' + m;
}

export function crossLabel(direction) {
  return direction === 'below' ? 'cross down' : 'cross up';
}

export function repeatLabel(repeat) {
  return repeat === 'continuous' ? 'continuous' : 'once';
}

export function alertDetail(alert) {
  const kind = alert.bracket_id ? 'bracket · ' : '';
  return kind + repeatLabel(alert.repeat) + ' · ' + alert.status;
}

export function alertMarks(alert) {
  const up = alert.direction !== 'below';
  const direction = up
    ? mark(crossLabel(alert.direction), 'dir up', '<path d="M8 12.8V3.6"></path><path d="M4.4 7 8 3.4 11.6 7"></path>')
    : mark(crossLabel(alert.direction), 'dir down', '<path d="M8 3.2v9.2"></path><path d="M4.4 9 8 12.6 11.6 9"></path>');
  const repeat = alert.repeat === 'continuous'
    ? mark('continuous', '', '<path d="M11.6 5.8a3.8 3.8 0 0 0-6.8-1.4"></path><path d="M4.2 2.8v2.4h2.4"></path><path d="M4.4 10.2a3.8 3.8 0 0 0 6.8 1.4"></path><path d="M11.8 13.2V10.8H9.4"></path>')
    : `<span class="alert-mark once" role="img" aria-label="once">1</span>`;
  const bracket = alert.bracket_id
    ? mark('bracket', '', '<path d="M6.4 3.2H3.8v9.6h2.6"></path><path d="M9.6 3.2h2.6v9.6H9.6"></path>')
    : '';
  const status = alert.status === 'triggered' ? 'triggered' : 'armed';
  return `<span class="alert-main">${direction}<span class="num">${formatPrice(alert.level)}</span>${repeat}${bracket}<span class="sub">${status}</span></span>`;
}

function mark(label, cls, body) {
  const extra = cls ? ' ' + cls : '';
  return `<span class="alert-mark${extra}" role="img" aria-label="${label}"><svg viewBox="0 0 16 16" aria-hidden="true">${body}</svg></span>`;
}

export function priceFormat(price) {
  const abs = Math.abs(Number(price) || 0);
  let precision = 2;
  if (abs > 0 && abs < 1) precision = abs < 0.01 ? 6 : 4;
  return { type: 'price', precision, minMove: 10 ** -precision };
}
