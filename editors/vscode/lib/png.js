'use strict';
// PNG, in plain JavaScript: inflate and enough of the format to read the images a texture
// layer draws. The same code runs in Node (the tests) and in the preview webview, so what
// the tests check is what the panel shows, and the extension needs no dependency.

// bitReader reads DEFLATE's least-significant-bit-first stream. Values are kept as plain
// numbers (never more than 23 bits at a time) so no bit operation overflows.
class bitReader {
  constructor(data) {
    this.data = data;
    this.pos = 0;
    this.buf = 0;
    this.count = 0;
  }

  bits(n) {
    while (this.count < n) {
      if (this.pos >= this.data.length) {
        throw new Error('deflate: stream ends inside a block');
      }
      this.buf += this.data[this.pos++] * (1 << this.count);
      this.count += 8;
    }
    const m = 1 << n;
    const v = this.buf % m;
    this.buf = (this.buf - v) / m;
    this.count -= n;
    return v;
  }

  align() {
    this.buf = 0;
    this.count = 0;
  }
}

// huffman builds the canonical code of a set of code lengths: how many codes have each
// length, and the symbols in code order.
function huffman(lengths) {
  const counts = new Int32Array(16);
  for (const l of lengths) {
    counts[l]++;
  }
  counts[0] = 0;
  const offsets = new Int32Array(16);
  for (let len = 1; len < 15; len++) {
    offsets[len + 1] = offsets[len] + counts[len];
  }
  const symbols = new Int32Array(lengths.length);
  for (let s = 0; s < lengths.length; s++) {
    if (lengths[s] !== 0) {
      symbols[offsets[lengths[s]]++] = s;
    }
  }
  return { counts, symbols };
}

// symbol decodes one Huffman code, walking the canonical code one bit at a time.
function symbol(br, h) {
  let code = 0;
  let first = 0;
  let index = 0;
  for (let len = 1; len <= 15; len++) {
    code |= br.bits(1);
    const count = h.counts[len];
    if (code - first < count) {
      return h.symbols[index + (code - first)];
    }
    index += count;
    first = (first + count) << 1;
    code <<= 1;
  }
  throw new Error('deflate: bad Huffman code');
}

const lengthBase = [3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258];
const lengthExtra = [0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0];
const distBase = [1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577];
const distExtra = [0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13];
const clOrder = [16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15];

let fixedLit = null;
let fixedDist = null;

function fixedTables() {
  if (!fixedLit) {
    const lit = new Int32Array(288);
    lit.fill(8, 0, 144);
    lit.fill(9, 144, 256);
    lit.fill(7, 256, 280);
    lit.fill(8, 280, 288);
    fixedLit = huffman(lit);
    fixedDist = huffman(new Int32Array(30).fill(5));
  }
  return [fixedLit, fixedDist];
}

// out grows the output buffer as the stream needs it.
class out {
  constructor(hint) {
    this.buf = new Uint8Array(Math.max(hint, 1024));
    this.len = 0;
  }

  room(n) {
    if (this.len + n <= this.buf.length) {
      return;
    }
    let size = this.buf.length * 2;
    while (size < this.len + n) {
      size *= 2;
    }
    const next = new Uint8Array(size);
    next.set(this.buf.subarray(0, this.len));
    this.buf = next;
  }

  byte(b) {
    this.room(1);
    this.buf[this.len++] = b;
  }

  copy(dist, len) {
    if (dist > this.len) {
      throw new Error('deflate: copy from before the start of the stream');
    }
    this.room(len);
    let from = this.len - dist;
    for (let i = 0; i < len; i++) {
      this.buf[this.len++] = this.buf[from++];
    }
  }

  bytes() {
    return this.buf.subarray(0, this.len);
  }
}

// inflateRaw decompresses a raw DEFLATE stream (RFC 1951). hint is the expected size.
function inflateRaw(data, hint) {
  const br = new bitReader(data);
  const o = new out(hint || data.length * 4);
  for (;;) {
    const last = br.bits(1);
    const type = br.bits(2);
    if (type === 0) {
      br.align();
      if (br.pos + 4 > data.length) {
        throw new Error('deflate: stream ends inside a stored block');
      }
      const n = data[br.pos] | (data[br.pos + 1] << 8);
      const inv = data[br.pos + 2] | (data[br.pos + 3] << 8);
      br.pos += 4;
      if ((n ^ 0xffff) !== inv) {
        throw new Error('deflate: stored block length does not match its complement');
      }
      if (br.pos + n > data.length) {
        throw new Error('deflate: stored block runs past the end');
      }
      o.room(n);
      o.buf.set(data.subarray(br.pos, br.pos + n), o.len);
      o.len += n;
      br.pos += n;
    } else if (type === 1 || type === 2) {
      let lit;
      let dist;
      if (type === 1) {
        [lit, dist] = fixedTables();
      } else {
        const nlen = br.bits(5) + 257;
        const ndist = br.bits(5) + 1;
        const ncode = br.bits(4) + 4;
        const cl = new Int32Array(19);
        for (let i = 0; i < ncode; i++) {
          cl[clOrder[i]] = br.bits(3);
        }
        const clh = huffman(cl);
        const lengths = new Int32Array(nlen + ndist);
        for (let i = 0; i < lengths.length;) {
          const s = symbol(br, clh);
          if (s < 16) {
            lengths[i++] = s;
          } else if (s === 16) {
            if (i === 0) {
              throw new Error('deflate: repeat with no previous length');
            }
            const prev = lengths[i - 1];
            for (let n = br.bits(2) + 3; n > 0 && i < lengths.length; n--) {
              lengths[i++] = prev;
            }
          } else if (s === 17) {
            i += br.bits(3) + 3;
          } else {
            i += br.bits(7) + 11;
          }
        }
        lit = huffman(lengths.subarray(0, nlen));
        dist = huffman(lengths.subarray(nlen));
      }
      for (;;) {
        const s = symbol(br, lit);
        if (s < 256) {
          o.byte(s);
        } else if (s === 256) {
          break;
        } else {
          const i = s - 257;
          if (i >= lengthBase.length) {
            throw new Error('deflate: bad length code');
          }
          const len = lengthBase[i] + br.bits(lengthExtra[i]);
          const d = symbol(br, dist);
          if (d >= distBase.length) {
            throw new Error('deflate: bad distance code');
          }
          o.copy(distBase[d] + br.bits(distExtra[d]), len);
        }
      }
    } else {
      throw new Error('deflate: reserved block type');
    }
    if (last) {
      return o.bytes();
    }
  }
}

// inflate decompresses a zlib stream (RFC 1950): the two-byte header, then DEFLATE. The
// Adler-32 checksum at the end is not verified; a corrupt stream fails while decoding.
function inflate(data, hint) {
  if (data.length < 2) {
    throw new Error('zlib: stream too short');
  }
  if ((data[0] & 0x0f) !== 8) {
    throw new Error('zlib: not deflate-compressed');
  }
  if (((data[0] << 8) | data[1]) % 31 !== 0) {
    throw new Error('zlib: bad header');
  }
  if (data[1] & 0x20) {
    throw new Error('zlib: preset dictionary not supported');
  }
  return inflateRaw(data.subarray(2), hint);
}

const signature = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];

// paeth is the PNG filter predictor.
function paeth(a, b, c) {
  const p = a + b - c;
  const pa = Math.abs(p - a);
  const pb = Math.abs(p - b);
  const pc = Math.abs(p - c);
  if (pa <= pb && pa <= pc) {
    return a;
  }
  return pb <= pc ? b : c;
}

// unfilter undoes the per-scanline filters in place and returns the raw scanlines,
// stride bytes each, with the filter bytes removed.
function unfilter(raw, w, h, bpp, stride) {
  const out = new Uint8Array(w === 0 ? 0 : stride * h);
  let src = 0;
  for (let y = 0; y < h; y++) {
    const filter = raw[src++];
    const row = y * stride;
    const prev = row - stride;
    for (let i = 0; i < stride; i++) {
      const x = raw[src + i];
      const a = i >= bpp ? out[row + i - bpp] : 0;
      const b = y > 0 ? out[prev + i] : 0;
      const c = i >= bpp && y > 0 ? out[prev + i - bpp] : 0;
      let v;
      switch (filter) {
        case 0: v = x; break;
        case 1: v = x + a; break;
        case 2: v = x + b; break;
        case 3: v = x + ((a + b) >> 1); break;
        case 4: v = x + paeth(a, b, c); break;
        default: throw new Error('png: unknown filter ' + filter + ' on row ' + y);
      }
      out[row + i] = v & 0xff;
    }
    src += stride;
  }
  return out;
}

// sample reads the i-th sample of a scanline of depth bits and scales it to 0..255, the
// way an 8-bit reader sees it: 16-bit samples keep their high byte, smaller ones are
// spread over the range (as PNG's sample depth scaling does).
function sample(row, i, depth) {
  switch (depth) {
    case 16:
      return row[2 * i];
    case 8:
      return row[i];
    case 4:
      return ((row[i >> 1] >> (i & 1 ? 0 : 4)) & 0x0f) * 17;
    case 2:
      return ((row[i >> 2] >> (6 - 2 * (i & 3))) & 0x03) * 85;
    default:
      return ((row[i >> 3] >> (7 - (i & 7))) & 0x01) * 255;
  }
}

// index reads the i-th palette index of a scanline of depth bits.
function index(row, i, depth) {
  switch (depth) {
    case 8:
      return row[i];
    case 4:
      return (row[i >> 1] >> (i & 1 ? 0 : 4)) & 0x0f;
    case 2:
      return (row[i >> 2] >> (6 - 2 * (i & 3))) & 0x03;
    default:
      return (row[i >> 3] >> (7 - (i & 7))) & 0x01;
  }
}

// decode reads a PNG image and returns straight (not premultiplied) 8-bit RGBA, the same
// pixels the engine reads. Interlaced files are refused: nothing the engine writes is
// interlaced, and neither are the images an art tool exports by default.
function decode(bytes) {
  const b = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  for (let i = 0; i < signature.length; i++) {
    if (b[i] !== signature[i]) {
      throw new Error('png: not a PNG file');
    }
  }
  const view = new DataView(b.buffer, b.byteOffset, b.byteLength);
  let pos = 8;
  let w = 0;
  let h = 0;
  let depth = 0;
  let color = 0;
  let palette = null;
  let trns = null;
  const idat = [];
  let idatLen = 0;
  while (pos + 8 <= b.length) {
    const len = view.getUint32(pos);
    const type = String.fromCharCode(b[pos + 4], b[pos + 5], b[pos + 6], b[pos + 7]);
    const data = b.subarray(pos + 8, pos + 8 + len);
    if (pos + 12 + len > b.length) {
      throw new Error('png: chunk ' + type + ' runs past the end of the file');
    }
    if (type === 'IHDR') {
      w = view.getUint32(pos + 8);
      h = view.getUint32(pos + 12);
      depth = b[pos + 16];
      color = b[pos + 17];
      if (b[pos + 20] !== 0) {
        throw new Error('png: interlaced images are not supported');
      }
    } else if (type === 'PLTE') {
      palette = data.slice();
    } else if (type === 'tRNS') {
      trns = data.slice();
    } else if (type === 'IDAT') {
      idat.push(data);
      idatLen += len;
    } else if (type === 'IEND') {
      break;
    }
    pos += 12 + len;
  }
  if (w < 1 || h < 1) {
    throw new Error('png: no image header');
  }
  const channels = { 0: 1, 2: 3, 3: 1, 4: 2, 6: 4 }[color];
  if (channels === undefined) {
    throw new Error('png: unknown color type ' + color);
  }
  if (![1, 2, 4, 8, 16].includes(depth) || (color !== 0 && color !== 3 && depth < 8) || (color === 3 && depth === 16)) {
    throw new Error('png: color type ' + color + ' with bit depth ' + depth + ' is not supported');
  }
  let z = idat[0] || new Uint8Array(0);
  if (idat.length > 1) {
    z = new Uint8Array(idatLen);
    let at = 0;
    for (const part of idat) {
      z.set(part, at);
      at += part.length;
    }
  }
  const stride = Math.ceil((w * channels * depth) / 8);
  const raw = inflate(z, (stride + 1) * h);
  if (raw.length < (stride + 1) * h) {
    throw new Error('png: image data is short');
  }
  const bpp = Math.max(1, Math.ceil((channels * depth) / 8));
  const rows = unfilter(raw, w, h, bpp, stride);
  const rgba = new Uint8Array(w * h * 4);
  for (let y = 0; y < h; y++) {
    const row = rows.subarray(y * stride, (y + 1) * stride);
    for (let x = 0; x < w; x++) {
      const o = (y * w + x) * 4;
      let r;
      let g;
      let bl;
      let a = 255;
      if (color === 3) {
        const i = index(row, x, depth);
        if (!palette || 3 * i + 2 >= palette.length) {
          throw new Error('png: palette index out of range');
        }
        r = palette[3 * i];
        g = palette[3 * i + 1];
        bl = palette[3 * i + 2];
        if (trns && i < trns.length) {
          a = trns[i];
        }
      } else if (color === 0 || color === 4) {
        r = g = bl = sample(row, x * channels, depth);
        if (color === 4) {
          a = sample(row, x * channels + 1, depth);
        } else if (trns && trns.length >= 2) {
          const key = depth === 16 ? (trns[0] << 8) | trns[1] : trns[1];
          const raw16 = depth === 16 ? (row[2 * x] << 8) | row[2 * x + 1] : index(row, x, depth === 8 ? 8 : depth);
          if (raw16 === key) {
            a = 0;
          }
        }
      } else {
        r = sample(row, x * channels, depth);
        g = sample(row, x * channels + 1, depth);
        bl = sample(row, x * channels + 2, depth);
        if (color === 6) {
          a = sample(row, x * channels + 3, depth);
        } else if (trns && trns.length >= 6) {
          const at16 = (k) => (depth === 16 ? (row[(x * 3 + k) * 2] << 8) | row[(x * 3 + k) * 2 + 1] : row[x * 3 + k]);
          const key = (k) => (depth === 16 ? (trns[2 * k] << 8) | trns[2 * k + 1] : trns[2 * k + 1]);
          if (at16(0) === key(0) && at16(1) === key(1) && at16(2) === key(2)) {
            a = 0;
          }
        }
      }
      rgba[o] = r;
      rgba[o + 1] = g;
      rgba[o + 2] = bl;
      rgba[o + 3] = a;
    }
  }
  return { w, h, data: rgba };
}

const pngApi = { decode, inflate, inflateRaw };

// The webview loads this file with a script tag, Node with require.
if (typeof module !== 'undefined' && module.exports) {
  module.exports = pngApi;
} else {
  globalThis.vedutaPng = pngApi;
}
