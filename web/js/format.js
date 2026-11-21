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

export function priceFormat(price) {
  const abs = Math.abs(Number(price) || 0);
  let precision = 2;
  if (abs > 0 && abs < 1) precision = abs < 0.01 ? 6 : 4;
  return { type: 'price', precision, minMove: 10 ** -precision };
}
