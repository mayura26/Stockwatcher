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
  return sign + v.toFixed(2);
}

export function pctClass(n) {
  if (n == null || Number(n) === 0) return 'flat';
  return Number(n) > 0 ? 'up' : 'down';
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

export function priceFormat(price) {
  const abs = Math.abs(Number(price) || 0);
  let precision = 2;
  if (abs > 0 && abs < 1) precision = abs < 0.01 ? 6 : 4;
  return { type: 'price', precision, minMove: 10 ** -precision };
}
