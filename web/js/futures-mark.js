const CREAM = '#e8e0cc';
const DARK = '#1c1a14';

function mark(bg, ink = CREAM, edge = 'transparent') {
  return { bg, ink, edge };
}

// Fills follow the underlying: index ETFs, metals, energy, and so on.
// Micros share the full-size color; the letters still show the root.
const MARKS = {
  ES: mark('#b8433a'),
  MES: mark('#b8433a'),
  NQ: mark('#3d6fbf'),
  MNQ: mark('#3d6fbf'),
  YM: mark('#5e6a78'),
  MYM: mark('#5e6a78'),
  RTY: mark('#a36b32'),
  M2K: mark('#a36b32'),

  CL: mark('#1c1e22', CREAM, 'rgba(232,224,204,0.35)'),
  MCL: mark('#1c1e22', CREAM, 'rgba(232,224,204,0.35)'),
  NG: mark('#3c7d86'),
  RB: mark('#6e8f3a'),
  HO: mark('#8a5a2a'),

  GC: mark('#c6a15b', DARK),
  MGC: mark('#c6a15b', DARK),
  SI: mark('#c8ccd0', DARK),
  HG: mark('#b56a3a'),
  PL: mark('#d4d0c8', DARK),
  PA: mark('#9a9088'),

  ZB: mark('#2f6a4e'),
  UB: mark('#2f6a4e'),
  ZN: mark('#2f6a4e'),
  TN: mark('#2f6a4e'),
  ZF: mark('#2f6a4e'),
  ZT: mark('#2f6a4e'),
  SR3: mark('#3d6e78'),

  '6E': mark('#2a4f8f'),
  '6J': mark('#8c3038'),
  '6B': mark('#24345c'),
  '6A': mark('#2f6b45'),
  '6C': mark('#a84a3a'),
  '6S': mark('#9a3d45'),
  '6N': mark('#1e463f'),
  '6M': mark('#7a3040'),

  ZC: mark('#c9a227', DARK),
  ZS: mark('#5d7a34'),
  ZW: mark('#c4a06a', DARK),
  KE: mark('#a07a42'),
  ZL: mark('#b89a3a', DARK),
  ZM: mark('#8a6844'),
  LE: mark('#7a4536'),
  HE: mark('#c48b96', DARK),
  GF: mark('#6a5344'),

  BTC: mark('#e08a2c', DARK),
  MBT: mark('#e08a2c', DARK),
  ETH: mark('#6e74c4'),
};

export function futureMark(display) {
  const root = String(display || '').replace(/\s+/g, '').toUpperCase();
  return MARKS[root] || null;
}
