'use strict';
// The texture format (assets/textures/<name>.tex.json) drawn in JavaScript: the layer
// program the engine compiles in Go (asset/texture), transliterated so the preview shows
// what the game will show without running the engine.
//
// Every float32 operation of the engine is rounded here with Math.fround, the noise hash
// is 64-bit as it is there, and the trigonometry is the engine's deterministic one, so the
// pixels come out identical: test/texture.test.js compares this renderer with the engine's
// own golden images. The engine stays the authority — this is a viewer, not a second
// format — so what it refuses is reported here as an error instead of being drawn.

const f = Math.fround;

// Limits and defaults of the format (asset/texture/texture.go).
const HEADER = 'texture/1';
const MaxSize = 4096;
const MaxLayers = 64;
const DefaultScale = 8;
const MaxScale = 4096;
const DefaultOctaves = 1;
const MaxOctaves = 8;
const MaxCells = 4096;
const MaxColors = 64;

const layerTypes = ['solid', 'noise', 'stripes', 'rect', 'circle', 'gradient', 'checker', 'image'];
const blendNames = ['normal', 'multiply', 'screen', 'add'];
const fitNames = ['contain', 'cover', 'stretch'];

// layerFields lists the type-specific fields each layer type accepts; every type also
// accepts "type", "opacity" and "blend".
const layerFields = {
  solid: ['color'],
  noise: ['seed', 'scale', 'octaves', 'color'],
  stripes: ['width', 'angle_deg', 'colors'],
  rect: ['xy', 'size', 'color', 'corner', 'outline'],
  circle: ['center', 'radius', 'color', 'outline'],
  gradient: ['from', 'to', 'angle_deg'],
  checker: ['cells', 'colors'],
  image: ['path', 'fit'],
};

const textureFields = ['veduta', 'size', 'tiling', 'mipmaps', 'layers'];
const allLayerFields = ['type', 'opacity', 'blend'].concat(...Object.values(layerFields));

// ---------------------------------------------------------------- deterministic trigonometry
// gmath/trig.go: Cody-Waite reduction and fdlibm minimax kernels, so an angle gives the
// same sine and cosine here as in the engine.

const PI = Math.PI;
const reduceMax = (1 << 20) * (PI / 2);
const pio2Hi = 1.57079632673412561417e+00;
const pio2Mid = 6.07710050630396597660e-11;
const pio2Lo = 2.02226624871116645580e-21;
const twoOPi = 6.36619772367581382433e-01;

function kernelSin(r) {
  const z = r * r;
  let p = -2.50507602534068634195e-08 + z * 1.58969099521155010221e-10;
  p = 2.75573137070700676789e-06 + z * p;
  p = -1.98412698298579493134e-04 + z * p;
  p = 8.33333333332248946124e-03 + z * p;
  p = -1.66666666666666324348e-01 + z * p;
  return r + r * z * p;
}

function kernelCos(r) {
  const z = r * r;
  let p = 2.08757232129817482790e-09 + z * -1.13596475577881948265e-11;
  p = -2.75573143513906633035e-07 + z * p;
  p = 2.48015872894767294178e-05 + z * p;
  p = -1.38888888888741095749e-03 + z * p;
  p = 4.16666666666666019037e-02 + z * p;
  const hz = 0.5 * z;
  const w = 1 - hz;
  return w + (((1 - w) - hz) + z * z * p);
}

// sinCos returns [sin(x), cos(x)] the way the engine computes them.
function sinCos(x) {
  if (!isFinite(x)) {
    return [NaN, NaN];
  }
  if (x === 0) {
    return [x, 1];
  }
  if (Math.abs(x) >= reduceMax) {
    x = x % (2 * PI);
  }
  const k = Math.floor(x * twoOPi + 0.5);
  let r = x - k * pio2Hi;
  r -= k * pio2Mid;
  r -= k * pio2Lo;
  const s = kernelSin(r);
  const c = kernelCos(r);
  switch (((k % 4) + 4) % 4) {
    case 0: return [s, c];
    case 1: return [c, -s];
    case 2: return [-s, -c];
    default: return [-c, s];
  }
}

// direction is the unit vector at angle deg in pixel space (0 degrees = +x, right;
// 90 = +y, down). Multiples of 90 are exact.
function direction(deg) {
  if (deg % 90 === 0) {
    switch (((Math.trunc((deg / 90) % 4) % 4) + 4) % 4) {
      case 0: return [1, 0];
      case 1: return [0, 1];
      case 2: return [-1, 0];
      default: return [0, -1];
    }
  }
  const [sin, cos] = sinCos(deg * (PI / 180));
  return [cos, sin];
}

// ---------------------------------------------------------------- the noise lattice hash
// asset/texture/noise.go: SplitMix64 over 64-bit words, kept as two 32-bit halves.

let mhi = 0;
let mlo = 0;

// mul sets the scratch word to itself times (bh, bl), modulo 2^64.
function mul(bh, bl) {
  const a0 = mlo & 0xffff;
  const a1 = mlo >>> 16;
  const b0 = bl & 0xffff;
  const b1 = bl >>> 16;
  const p0 = a0 * b0;
  const p1 = a1 * b0;
  const p2 = a0 * b1;
  const p3 = a1 * b1;
  const mid = (p0 >>> 16) + (p1 & 0xffff) + (p2 & 0xffff);
  const lo = (((mid & 0xffff) << 16) | (p0 & 0xffff)) >>> 0;
  const carry = (mid >>> 16) + (p1 >>> 16) + (p2 >>> 16) + p3;
  mhi = (Math.imul(mhi, bl) + Math.imul(mlo, bh) + carry) >>> 0;
  mlo = lo;
}

// shrXor is h ^= h >> n, for n below 32.
function shrXor(n) {
  const lo = ((mlo >>> n) | (mhi << (32 - n))) >>> 0;
  mhi = (mhi ^ (mhi >>> n)) >>> 0;
  mlo = (mlo ^ lo) >>> 0;
}

// mix64 is the SplitMix64 output function on the scratch word.
function mix64() {
  shrXor(30);
  mul(0xbf58476d, 0x1ce4e5b9);
  shrXor(27);
  mul(0x94d049bb, 0x133111eb);
  shrXor(31);
}

// u64 splits a whole number into its two 32-bit halves, two's complement.
function u64(n) {
  const x = BigInt(Math.trunc(n)) & 0xffffffffffffffffn;
  return [Number(x >> 32n), Number(x & 0xffffffffn)];
}

// noiseField is fractal value noise: octave o is a lattice of random values in [0, 1)
// with twice the cells of octave o-1, interpolated with smoothstep weights, weighted
// 1/2^o, and the sum divided by the total weight.
class noiseField {
  constructor(spec, l) {
    this.w = spec.w;
    this.h = spec.h;
    this.tiling = spec.tiling;
    this.cx = [];
    this.cy = [];
    this.px = [];
    this.py = [];
    this.octHi = [];
    this.octLo = [];
    let sx = l.scale;
    let sy = (spec.h * l.scale) / spec.w;
    if (spec.tiling) {
      sx = Math.max(1, Math.floor(l.scale + 0.5));
      sy = Math.max(1, Math.floor((spec.h * sx) / spec.w + 0.5));
    }
    const [sh, sl] = u64(l.seed);
    let weight = 1;
    let total = 0;
    for (let o = 0; o < l.octaves; o++) {
      const m = Math.pow(2, o);
      this.cx.push(sx * m);
      this.cy.push(sy * m);
      if (spec.tiling) {
        this.px.push(sx * m);
        this.py.push(sy * m);
      }
      // seed ^ 0x9e3779b97f4a7c15*(o+1), the word every lattice point of this octave
      // hashes from.
      mhi = 0x9e3779b9;
      mlo = 0x7f4a7c15;
      mul(0, o + 1);
      this.octHi.push((mhi ^ sh) >>> 0);
      this.octLo.push((mlo ^ sl) >>> 0);
      total += weight;
      weight /= 2;
    }
    this.norm = 1 / total;
  }

  // lattice is the random value in [0, 1) of lattice point (i, j) of octave o.
  lattice(o, i, j) {
    mhi = this.octHi[o];
    mlo = this.octLo[o];
    mix64();
    mhi = (mhi ^ Math.floor(i / 4294967296)) >>> 0;
    mlo = (mlo ^ (i >>> 0)) >>> 0;
    mix64();
    mhi = (mhi ^ Math.floor(j / 4294967296)) >>> 0;
    mlo = (mlo ^ (j >>> 0)) >>> 0;
    mix64();
    return (mhi * 2097152 + (mlo >>> 11)) / 9007199254740992;
  }

  at(x, y) {
    let sum = 0;
    let weight = 1;
    for (let o = 0; o < this.cx.length; o++) {
      const u = ((2 * x + 1) * this.cx[o]) / (2 * this.w);
      const v = ((2 * y + 1) * this.cy[o]) / (2 * this.h);
      const fu = Math.floor(u);
      const fv = Math.floor(v);
      const tu = u - fu;
      const tv = v - fv;
      let i0 = fu;
      let j0 = fv;
      let i1 = i0 + 1;
      let j1 = j0 + 1;
      if (this.tiling) {
        i0 = wrap(i0, this.px[o]);
        i1 = wrap(i1, this.px[o]);
        j0 = wrap(j0, this.py[o]);
        j1 = wrap(j1, this.py[o]);
      }
      const su = tu * tu * (3 - 2 * tu);
      const sv = tv * tv * (3 - 2 * tv);
      const v00 = this.lattice(o, i0, j0);
      const v10 = this.lattice(o, i1, j0);
      const v01 = this.lattice(o, i0, j1);
      const v11 = this.lattice(o, i1, j1);
      const top = v00 + (v10 - v00) * su;
      const bottom = v01 + (v11 - v01) * su;
      sum += weight * (top + (bottom - top) * sv);
      weight /= 2;
    }
    return f(sum * this.norm);
  }
}

function wrap(i, n) {
  i %= n;
  return i < 0 ? i + n : i;
}

// ---------------------------------------------------------------- source validation

// parse reads the JSON text of a texture source.
function parse(text) {
  try {
    const src = JSON.parse(text);
    if (src === null || typeof src !== 'object' || Array.isArray(src)) {
      return { src: null, error: 'source must be a JSON object' };
    }
    return { src, error: null };
  } catch (e) {
    return { src: null, error: String(e.message || e) };
  }
}

// deps lists the image files a source reads (paths relative to the assets directory),
// without judging them: a source that does not compile still names the files to read.
function deps(src) {
  const out = [];
  for (const l of (src && Array.isArray(src.layers) ? src.layers : [])) {
    if (l && l.type === 'image' && typeof l.path === 'string' && l.path !== '' && !out.includes(l.path)) {
      out.push(l.path);
    }
  }
  return out;
}

const has = (o, k) => Object.prototype.hasOwnProperty.call(o, k);

// checker gathers located problems the way asset.Checker does, so the panel can name the
// field the engine would name.
class check {
  constructor() {
    this.errors = [];
  }

  at(path, msg) {
    this.errors.push({ path, msg });
  }

  // number validates a finite JSON number.
  number(path, v) {
    if (typeof v !== 'number' || !isFinite(v)) {
      this.at(path, 'not a finite number');
      return null;
    }
    return f(v);
  }

  positive(path, v) {
    const n = this.number(path, v);
    if (n === null) {
      return 1;
    }
    if (!(n > 0)) {
      this.at(path, `must be a positive number, got ${v}`);
      return 1;
    }
    return n;
  }

  nonNegative(path, v) {
    if (v === undefined) {
      return 0;
    }
    const n = this.number(path, v);
    if (n === null) {
      return 0;
    }
    if (n < 0) {
      this.at(path, `must be a number >= 0, got ${v}`);
      return 0;
    }
    return n;
  }

  int(path, v, lo, hi, def) {
    if (v === undefined) {
      return def;
    }
    if (typeof v !== 'number' || !Number.isInteger(v)) {
      this.at(path, 'must be a whole number');
      return def;
    }
    if (v < lo || v > hi) {
      this.at(path, `${v} out of range [${lo}, ${hi}]`);
      return def;
    }
    return v;
  }

  vec2(path, v, required) {
    if (v === undefined) {
      if (required) {
        this.at(path, 'is required ([x, y] in pixels)');
      }
      return [0, 0];
    }
    if (!Array.isArray(v) || v.length !== 2) {
      this.at(path, `want 2 numbers, got ${Array.isArray(v) ? v.length : typeof v}`);
      return [0, 0];
    }
    const out = [0, 0];
    for (let i = 0; i < 2; i++) {
      const n = this.number(`${path}[${i}]`, v[i]);
      if (n === null) {
        return [0, 0];
      }
      out[i] = n;
    }
    return out;
  }

  sizeVec2(path, v) {
    if (v === undefined) {
      this.at(path, 'is required ([width, height] in pixels)');
      return [1, 1];
    }
    if (!Array.isArray(v) || v.length !== 2) {
      this.at(path, `want 2 numbers, got ${Array.isArray(v) ? v.length : typeof v}`);
      return [1, 1];
    }
    const out = [1, 1];
    for (let i = 0; i < 2; i++) {
      const n = this.number(`${path}[${i}]`, v[i]);
      if (n === null) {
        continue;
      }
      if (!(n > 0)) {
        this.at(`${path}[${i}]`, `must be a positive number, got ${v[i]}`);
        continue;
      }
      out[i] = n;
    }
    return out;
  }

  color(path, s, required) {
    if (s === undefined) {
      if (required) {
        this.at(path, 'is required (#RRGGBB or #RRGGBBAA)');
      }
      return [0, 0, 0, 0];
    }
    if (typeof s !== 'string' || !/^#([0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.test(s)) {
      this.at(path, `color ${JSON.stringify(s)}: want #RRGGBB or #RRGGBBAA`);
      return [0, 0, 0, 0];
    }
    const n = (i) => parseInt(s.slice(1 + 2 * i, 3 + 2 * i), 16);
    return [f(n(0) / 255), f(n(1) / 255), f(n(2) / 255), s.length === 9 ? f(n(3) / 255) : 1];
  }

  colors(path, v, lo, hi) {
    if (v === undefined) {
      this.at(path, 'is required');
      return null;
    }
    if (!Array.isArray(v) || v.length < lo || v.length > hi) {
      const want = lo === hi ? `exactly ${lo} colors` : `${lo} to ${hi} colors`;
      this.at(path, `want ${want}, got ${Array.isArray(v) ? v.length : typeof v}`);
      return null;
    }
    return v.map((s, i) => this.color(`${path}[${i}]`, s, true));
  }

  enum(path, s, allowed, def) {
    if (s === undefined) {
      return def;
    }
    if (!allowed.includes(s)) {
      this.at(path, `unknown value ${JSON.stringify(s)} (want one of ${allowed.join(', ')})`);
      return def;
    }
    return s;
  }
}

// validate turns a decoded source into a spec with every default resolved, reporting the
// problems the engine reports. images maps an image layer's path to a decoded picture
// ({w, h, data}) or to {error}.
function validate(src, images) {
  const c = new check();
  const spec = { w: 1, h: 1, tiling: false, mipmaps: true, layers: [] };
  if (!has(src, 'veduta')) {
    c.at('', `missing "veduta" header (want "${HEADER}")`);
  } else if (src.veduta !== HEADER) {
    c.at('veduta', `header is ${JSON.stringify(src.veduta)}, want "${HEADER}"`);
  }
  for (const k of Object.keys(src)) {
    if (!textureFields.includes(k)) {
      c.at(k, 'unknown field');
    }
  }
  if (src.size === undefined) {
    c.at('size', 'is required ([width, height] in pixels)');
  } else if (!Array.isArray(src.size) || src.size.length !== 2) {
    c.at('size', `want 2 integers [width, height], got ${Array.isArray(src.size) ? src.size.length : typeof src.size}`);
  } else {
    let ok = true;
    for (let i = 0; i < 2; i++) {
      const v = src.size[i];
      if (typeof v !== 'number' || !Number.isInteger(v) || v < 1 || v > MaxSize) {
        c.at(`size[${i}]`, `${v} out of range [1, ${MaxSize}]`);
        ok = false;
      }
    }
    if (ok) {
      spec.w = src.size[0];
      spec.h = src.size[1];
    }
  }
  if (has(src, 'tiling')) {
    if (typeof src.tiling !== 'boolean') {
      c.at('tiling', 'must be true or false');
    } else {
      spec.tiling = src.tiling;
    }
  }
  if (has(src, 'mipmaps')) {
    if (typeof src.mipmaps !== 'boolean') {
      c.at('mipmaps', 'must be true or false');
    } else {
      spec.mipmaps = src.mipmaps;
    }
  }
  const layers = src.layers;
  if (!Array.isArray(layers) || layers.length === 0) {
    c.at('layers', 'at least one layer is required');
  } else if (layers.length > MaxLayers) {
    c.at('layers', `${layers.length} layers, want at most ${MaxLayers}`);
  }
  for (let i = 0; Array.isArray(layers) && i < layers.length; i++) {
    spec.layers.push(validateLayer(c, spec, i, layers[i], images));
  }
  return { spec, errors: c.errors };
}

function validateLayer(c, spec, i, l, images) {
  const lp = `layers[${i}]`;
  const field = (name) => `${lp}.${name}`;
  const ls = { type: '', opacity: 1, blend: 'normal' };
  if (l === null || typeof l !== 'object' || Array.isArray(l)) {
    c.at(lp, 'must be a JSON object');
    return ls;
  }
  ls.type = typeof l.type === 'string' ? l.type : '';
  if (l.type === undefined) {
    c.at(field('type'), `is required (one of ${layerTypes.join(', ')})`);
  } else if (!layerFields[l.type]) {
    c.at(field('type'), `unknown value ${JSON.stringify(l.type)} (want one of ${layerTypes.join(', ')})`);
  } else {
    for (const k of Object.keys(l)) {
      if (!allLayerFields.includes(k)) {
        c.at(field(k), 'unknown field');
      } else if (k !== 'type' && k !== 'opacity' && k !== 'blend' && !layerFields[l.type].includes(k)) {
        c.at(field(k), 'not used by layer type ' + l.type);
      }
    }
    validateTyped(c, spec, field, l, ls, images);
  }
  if (has(l, 'opacity')) {
    const n = c.number(field('opacity'), l.opacity);
    if (n !== null) {
      if (n < 0 || n > 1) {
        c.at(field('opacity'), `${l.opacity} out of range [0, 1]`);
      } else {
        ls.opacity = n;
      }
    }
  }
  ls.blend = c.enum(field('blend'), l.blend, blendNames, 'normal');
  return ls;
}

function validateTyped(c, spec, field, l, ls, images) {
  switch (l.type) {
    case 'solid':
      ls.color = c.color(field('color'), l.color, true);
      break;
    case 'noise':
      ls.seed = c.int(field('seed'), l.seed, -Number.MAX_SAFE_INTEGER, Number.MAX_SAFE_INTEGER, 0);
      ls.scale = DefaultScale;
      if (has(l, 'scale')) {
        const n = c.number(field('scale'), l.scale);
        if (n !== null) {
          if (!(n > 0) || n > MaxScale) {
            c.at(field('scale'), `${l.scale} out of range (0, ${MaxScale}]`);
          } else {
            ls.scale = n;
          }
        }
      }
      ls.octaves = c.int(field('octaves'), l.octaves, 1, MaxOctaves, DefaultOctaves);
      ls.color = c.color(field('color'), l.color, true);
      break;
    case 'stripes':
      ls.width = has(l, 'width') ? c.positive(field('width'), l.width) : (c.at(field('width'), 'is required'), 1);
      ls.angle = has(l, 'angle_deg') ? (c.number(field('angle_deg'), l.angle_deg) || 0) : 0;
      ls.colors = c.colors(field('colors'), l.colors, 2, MaxColors);
      break;
    case 'rect':
      ls.xy = c.vec2(field('xy'), l.xy, true);
      ls.size = c.sizeVec2(field('size'), l.size);
      ls.color = c.color(field('color'), l.color, true);
      ls.corner = c.nonNegative(field('corner'), l.corner);
      ls.outline = c.nonNegative(field('outline'), l.outline);
      break;
    case 'circle':
      ls.center = c.vec2(field('center'), l.center, true);
      ls.radius = has(l, 'radius') ? c.positive(field('radius'), l.radius) : (c.at(field('radius'), 'is required'), 1);
      ls.color = c.color(field('color'), l.color, true);
      ls.outline = c.nonNegative(field('outline'), l.outline);
      break;
    case 'gradient':
      ls.from = c.color(field('from'), l.from, true);
      ls.to = c.color(field('to'), l.to, true);
      ls.angle = has(l, 'angle_deg') ? (c.number(field('angle_deg'), l.angle_deg) || 0) : 0;
      break;
    case 'checker':
      if (!has(l, 'cells')) {
        c.at(field('cells'), `is required (cells per side, 1 to ${MaxCells})`);
        ls.cells = 1;
      } else {
        ls.cells = c.int(field('cells'), l.cells, 1, MaxCells, 1);
      }
      ls.colors = c.colors(field('colors'), l.colors, 2, 2);
      break;
    case 'image': {
      ls.fit = c.enum(field('fit'), l.fit, fitNames, 'contain');
      const why = badImagePath(l.path);
      if (why) {
        c.at(field('path'), why);
        break;
      }
      const img = images && images[l.path];
      if (!img) {
        c.at(field('path'), `${JSON.stringify(l.path)}: file not found in the assets directory`);
      } else if (img.error) {
        c.at(field('path'), `${JSON.stringify(l.path)}: ${img.error}`);
      } else {
        ls.img = img;
      }
      break;
    }
    default:
      break;
  }
}

// badImagePath returns why p is not an acceptable image path, or "".
function badImagePath(p) {
  if (p === undefined || p === '') {
    return 'is required (a .png file relative to the assets directory)';
  }
  if (typeof p !== 'string') {
    return 'must be a string';
  }
  if (p.includes('\\')) {
    return `${JSON.stringify(p)}: use forward slashes`;
  }
  if (p.startsWith('/') || p.includes(':')) {
    return `${JSON.stringify(p)}: must be relative to the assets directory`;
  }
  if (p.split('/').includes('..')) {
    return `${JSON.stringify(p)}: must not leave the assets directory ("..")`;
  }
  if (p.split('/').some((s) => s === '' || s === '.') || p.endsWith('/')) {
    return `${JSON.stringify(p)}: not a clean relative path (no "." or empty segments, no trailing "/")`;
  }
  if (!/\.png$/i.test(p)) {
    return `${JSON.stringify(p)}: must be a .png file`;
  }
  return '';
}

// ---------------------------------------------------------------- rendering
// asset/texture/render.go.

// ss is the supersampling grid per axis; ssMargin bounds the distance from a pixel center
// to its farthest sample.
const ss = 4;
const ssMargin = 0.54;

// composite applies a layer with straight color s and effective alpha a to the canvas
// pixel d (straight RGB + alpha): W3C source-over with a separable blend function.
function composite(d, s, a, blend) {
  if (!(a > 0)) {
    return;
  }
  if (a > 1) {
    a = 1;
  }
  const da = d[3];
  if (da === 0) {
    d[0] = s[0];
    d[1] = s[1];
    d[2] = s[2];
    d[3] = a;
    return;
  }
  const b = [0, 0, 0];
  for (let i = 0; i < 3; i++) {
    b[i] = blendFn(blend, d[i], s[i]);
  }
  if (da === 1) {
    for (let i = 0; i < 3; i++) {
      d[i] = f(d[i] + f(f(b[i] - d[i]) * a));
    }
    return;
  }
  const ao = f(a + f(da * f(1 - a)));
  for (let i = 0; i < 3; i++) {
    const co = f(f(f(f(a * f(1 - da)) * s[i]) + f(f(a * da) * b[i])) + f(f(f(1 - a) * da) * d[i]));
    d[i] = f(co / ao);
  }
  d[3] = Math.min(ao, 1);
}

function blendFn(blend, d, s) {
  switch (blend) {
    case 'multiply': return f(d * s);
    case 'screen': return f(1 - f(f(1 - d) * f(1 - s)));
    case 'add': return Math.min(1, f(d + s));
    default: return s;
  }
}

// q8 quantizes a channel: clamped to [0, 1], times 255, rounded half up.
function q8(x) {
  if (!(x > 0)) {
    return 0;
  }
  if (x >= 1) {
    return 255;
  }
  return Math.trunc(f(f(x * 255) + 0.5));
}

function clamp01(x) {
  if (!(x > 0)) {
    return 0;
  }
  return x >= 1 ? 1 : f(x);
}

// premulAvg averages ss*ss premultiplied samples: acc holds the sums of a*rgb and of a.
function premulAvg(acc, n, out) {
  if (!(acc[3] > 0)) {
    out[0] = out[1] = out[2] = out[3] = 0;
    return;
  }
  const inv = 1 / acc[3];
  out[0] = clamp01(acc[0] * inv);
  out[1] = clamp01(acc[1] * inv);
  out[2] = clamp01(acc[2] * inv);
  out[3] = f(acc[3] / n);
}

// A painter evaluates one layer: at(x, y, out) writes the layer's straight RGB and its
// alpha before opacity into out.

class solidPainter {
  constructor(l) {
    this.c = l.color;
  }

  at(x, y, out) {
    out[0] = this.c[0];
    out[1] = this.c[1];
    out[2] = this.c[2];
    out[3] = this.c[3];
  }
}

class noisePainter {
  constructor(spec, l) {
    this.c = l.color;
    this.field = new noiseField(spec, l);
  }

  at(x, y, out) {
    out[0] = this.c[0];
    out[1] = this.c[1];
    out[2] = this.c[2];
    out[3] = f(this.c[3] * this.field.at(x, y));
  }
}

// stripes are parallel bands of width pixels along (cos, sin) from the texture origin.
class stripesPainter {
  constructor(l) {
    this.colors = l.colors;
    const [cos, sin] = direction(l.angle);
    this.cos = cos;
    this.sin = sin;
    this.width = l.width;
  }

  band(x, y) {
    return Math.floor((x * this.cos + y * this.sin) / this.width);
  }

  color(k) {
    const n = this.colors.length;
    let i = k % n;
    if (i < 0) {
      i += n;
    }
    return this.colors[Math.min(Math.trunc(i), n - 1)];
  }

  at(x, y, out) {
    const fx = x;
    const fy = y;
    const k0 = this.band(fx, fy);
    const k1 = this.band(fx + 1, fy);
    const k2 = this.band(fx, fy + 1);
    const k3 = this.band(fx + 1, fy + 1);
    if (k0 === k1 && k0 === k2 && k0 === k3) {
      const c = this.color(k0);
      out[0] = c[0];
      out[1] = c[1];
      out[2] = c[2];
      out[3] = c[3];
      return;
    }
    const acc = [0, 0, 0, 0];
    let first = null;
    let same = true;
    for (let j = 0; j < ss; j++) {
      const sy = fy + (j + 0.5) / ss;
      for (let i = 0; i < ss; i++) {
        const c = this.color(this.band(fx + (i + 0.5) / ss, sy));
        if (i === 0 && j === 0) {
          first = c;
        } else if (c !== first) {
          same = false;
        }
        const a = c[3];
        acc[0] += a * c[0];
        acc[1] += a * c[1];
        acc[2] += a * c[2];
        acc[3] += a;
      }
    }
    if (same) {
      out[0] = first[0];
      out[1] = first[1];
      out[2] = first[2];
      out[3] = first[3];
      return;
    }
    premulAvg(acc, ss * ss, out);
  }
}

// roundRect is a rectangle with rounded corners; a circle is one with hx = hy = r.
class roundRect {
  constructor(cx, cy, hx, hy, r) {
    this.cx = cx;
    this.cy = cy;
    this.hx = hx;
    this.hy = hy;
    this.r = r;
  }

  // dist is the exact signed Euclidean distance to the outline (negative inside).
  dist(x, y) {
    const qx = Math.abs(x - this.cx) - (this.hx - this.r);
    const qy = Math.abs(y - this.cy) - (this.hy - this.r);
    const ox = Math.max(qx, 0);
    const oy = Math.max(qy, 0);
    return Math.sqrt(ox * ox + oy * oy) + Math.min(Math.max(qx, qy), 0) - this.r;
  }
}

// shapePainter is a filled or outlined rectangle or circle with supersampled coverage.
class shapePainter {
  constructor(l) {
    this.c = l.color;
    this.hole = false;
    if (l.type === 'circle') {
      const r = l.radius;
      this.outer = new roundRect(l.center[0], l.center[1], r, r, r);
      const o = l.outline;
      if (o > 0 && o < r) {
        this.inner = new roundRect(l.center[0], l.center[1], r - o, r - o, r - o);
        this.hole = true;
      }
    } else {
      const hx = l.size[0] / 2;
      const hy = l.size[1] / 2;
      const r = Math.min(l.corner, hx, hy);
      this.outer = new roundRect(l.xy[0] + hx, l.xy[1] + hy, hx, hy, r);
      const o = l.outline;
      if (o > 0 && o < hx && o < hy) {
        this.inner = new roundRect(this.outer.cx, this.outer.cy, hx - o, hy - o, Math.max(r - o, 0));
        this.hole = true;
      }
    }
  }

  covered(x, y) {
    return this.outer.dist(x, y) <= 0 && !(this.hole && this.inner.dist(x, y) < 0);
  }

  at(x, y, out) {
    out[0] = this.c[0];
    out[1] = this.c[1];
    out[2] = this.c[2];
    const doo = this.outer.dist(x + 0.5, y + 0.5);
    if (doo > ssMargin) {
      out[3] = 0;
      return;
    }
    let di = Infinity;
    if (this.hole) {
      di = this.inner.dist(x + 0.5, y + 0.5);
      if (di < -ssMargin) {
        out[3] = 0;
        return;
      }
    }
    if (doo < -ssMargin && di > ssMargin) {
      out[3] = this.c[3];
      return;
    }
    let n = 0;
    for (let j = 0; j < ss; j++) {
      const sy = y + (j + 0.5) / ss;
      for (let i = 0; i < ss; i++) {
        if (this.covered(x + (i + 0.5) / ss, sy)) {
          n++;
        }
      }
    }
    out[3] = f(f(this.c[3] * n) / (ss * ss));
  }
}

// gradientPainter interpolates from -> to across the texture along a direction, with
// premultiplied alpha.
class gradientPainter {
  constructor(spec, l) {
    this.from = l.from;
    this.to = l.to;
    const [cos, sin] = direction(l.angle);
    this.cos = cos;
    this.sin = sin;
    let lo = Infinity;
    let hi = -Infinity;
    for (const [cx, cy] of [[0.5, 0.5], [spec.w - 0.5, 0.5], [0.5, spec.h - 0.5], [spec.w - 0.5, spec.h - 0.5]]) {
      const t = cx * cos + cy * sin;
      lo = Math.min(lo, t);
      hi = Math.max(hi, t);
    }
    this.t0 = lo;
    this.span = hi - lo;
  }

  at(x, y, out) {
    let t = 0;
    if (this.span > 0) {
      t = clamp01(((x + 0.5) * this.cos + (y + 0.5) * this.sin - this.t0) / this.span);
    }
    const u = f(1 - t);
    const c0 = this.from;
    const c1 = this.to;
    if (c0[3] === c1[3]) {
      out[0] = f(f(c0[0] * u) + f(c1[0] * t));
      out[1] = f(f(c0[1] * u) + f(c1[1] * t));
      out[2] = f(f(c0[2] * u) + f(c1[2] * t));
      out[3] = c0[3];
      return;
    }
    const a = f(f(c0[3] * u) + f(c1[3] * t));
    if (!(a > 0)) {
      out[0] = out[1] = out[2] = out[3] = 0;
      return;
    }
    for (let i = 0; i < 3; i++) {
      out[i] = Math.min(f(f(f(f(c0[i] * c0[3]) * u) + f(f(c1[i] * c1[3]) * t)) / a), 1);
    }
    out[3] = a;
  }
}

// checkerPainter is an n*n grid over the whole texture; a pixel takes the color of the
// cell containing its center, the first when column + row is even.
class checkerPainter {
  constructor(spec, l) {
    this.c0 = l.colors[0];
    this.c1 = l.colors[1];
    this.n = l.cells;
    this.w = spec.w;
    this.h = spec.h;
  }

  at(x, y, out) {
    const col = Math.floor(((2 * x + 1) * this.n) / (2 * this.w));
    const row = Math.floor(((2 * y + 1) * this.n) / (2 * this.h));
    const c = (col + row) & 1 ? this.c1 : this.c0;
    out[0] = c[0];
    out[1] = c[1];
    out[2] = c[2];
    out[3] = c[3];
  }
}

// imagePainter draws a PNG placed at (ox, oy) with size dw x dh texture pixels: exact
// area coverage at the edges, and kx*ky bilinear samples per pixel when shrinking.
class imagePainter {
  constructor(spec, l) {
    const W = spec.w;
    const H = spec.h;
    const img = l.img;
    const iw = img.w;
    const ih = img.h;
    this.img = img;
    if (l.fit === 'stretch') {
      this.dw = W;
      this.dh = H;
      this.ox = 0;
      this.oy = 0;
    } else {
      const k = l.fit === 'cover' ? Math.max(W / iw, H / ih) : Math.min(W / iw, H / ih);
      this.dw = iw * k;
      this.dh = ih * k;
      this.ox = (W - this.dw) / 2;
      this.oy = (H - this.dh) / 2;
    }
    this.fx = iw / this.dw;
    this.fy = ih / this.dh;
    this.kx = Math.max(1, Math.ceil(this.fx));
    this.ky = Math.max(1, Math.ceil(this.fy));
  }

  texel(i, j, out) {
    i = Math.min(Math.max(i, 0), this.img.w - 1);
    j = Math.min(Math.max(j, 0), this.img.h - 1);
    const p = (j * this.img.w + i) * 4;
    const d = this.img.data;
    const a = d[p + 3] / 255;
    out[0] = (d[p] / 255) * a;
    out[1] = (d[p + 1] / 255) * a;
    out[2] = (d[p + 2] / 255) * a;
    out[3] = a;
  }

  // bilinear samples the image at texel coordinates (u, v) with clamp-to-edge addressing
  // and returns premultiplied RGBA.
  bilinear(u, v, out) {
    const fu = Math.floor(u);
    const fv = Math.floor(v);
    const tu = u - fu;
    const tv = v - fv;
    const c00 = [0, 0, 0, 0];
    const c10 = [0, 0, 0, 0];
    const c01 = [0, 0, 0, 0];
    const c11 = [0, 0, 0, 0];
    this.texel(fu, fv, c00);
    this.texel(fu + 1, fv, c10);
    this.texel(fu, fv + 1, c01);
    this.texel(fu + 1, fv + 1, c11);
    for (let k = 0; k < 4; k++) {
      const top = c00[k] + (c10[k] - c00[k]) * tu;
      const bottom = c01[k] + (c11[k] - c01[k]) * tu;
      out[k] = top + (bottom - top) * tv;
    }
  }

  at(x, y, out) {
    const cov = overlap(x, this.ox, this.dw) * overlap(y, this.oy, this.dh);
    if (!(cov > 0)) {
      out[0] = out[1] = out[2] = out[3] = 0;
      return;
    }
    const acc = [0, 0, 0, 0];
    const c = [0, 0, 0, 0];
    for (let j = 0; j < this.ky; j++) {
      const sy = Math.min(Math.max(y + (j + 0.5) / this.ky, this.oy), this.oy + this.dh);
      const v = (sy - this.oy) * this.fy - 0.5;
      for (let i = 0; i < this.kx; i++) {
        const sx = Math.min(Math.max(x + (i + 0.5) / this.kx, this.ox), this.ox + this.dw);
        const u = (sx - this.ox) * this.fx - 0.5;
        this.bilinear(u, v, c);
        for (let k = 0; k < 4; k++) {
          acc[k] += c[k];
        }
      }
    }
    premulAvg(acc, this.kx * this.ky, out);
    out[3] = f(out[3] * f(cov));
  }
}

// overlap is the length of [a, a+1] intersected with [lo, lo+n].
function overlap(a, lo, n) {
  return Math.max(0, Math.min(a + 1, lo + n) - Math.max(a, lo));
}

function newPainter(spec, l) {
  switch (l.type) {
    case 'solid': return new solidPainter(l);
    case 'noise': return new noisePainter(spec, l);
    case 'stripes': return new stripesPainter(l);
    case 'rect':
    case 'circle': return new shapePainter(l);
    case 'gradient': return new gradientPainter(spec, l);
    case 'checker': return new checkerPainter(spec, l);
    case 'image': return new imagePainter(spec, l);
    default: return null;
  }
}

// render runs the layer program of spec, leaving out the layers in skip, and returns
// straight 8-bit RGBA rows, top to bottom.
function render(spec, skip) {
  const passes = [];
  for (let i = 0; i < spec.layers.length; i++) {
    if (skip && skip[i]) {
      continue;
    }
    const p = newPainter(spec, spec.layers[i]);
    if (p) {
      passes.push({ p, opacity: spec.layers[i].opacity, blend: spec.layers[i].blend });
    }
  }
  const data = new Uint8ClampedArray(spec.w * spec.h * 4);
  const px = new Float32Array(4);
  const out = new Float32Array(4);
  for (let y = 0; y < spec.h; y++) {
    for (let x = 0; x < spec.w; x++) {
      px[0] = px[1] = px[2] = px[3] = 0;
      for (const ps of passes) {
        ps.p.at(x, y, out);
        composite(px, out, f(out[3] * ps.opacity), ps.blend);
      }
      const k = (y * spec.w + x) * 4;
      data[k] = q8(px[0]);
      data[k + 1] = q8(px[1]);
      data[k + 2] = q8(px[2]);
      data[k + 3] = q8(px[3]);
    }
  }
  return { w: spec.w, h: spec.h, data };
}

// compile validates a decoded source and draws it. It returns the picture, or only the
// errors when the engine would refuse the source.
function compile(src, images, skip) {
  const { spec, errors } = validate(src, images);
  if (errors.length > 0) {
    return { spec, errors, image: null };
  }
  return { spec, errors, image: render(spec, skip) };
}

const textureApi = {
  HEADER, MaxSize, MaxLayers, MaxCells, MaxColors, MaxOctaves, MaxScale,
  layerTypes, blendNames, fitNames,
  parse, deps, validate, render, compile, badImagePath, sinCos, direction,
};
// The webview loads this file with a script tag, Node with require.
if (typeof module !== 'undefined' && module.exports) {
  module.exports = textureApi;
} else {
  globalThis.vedutaTexture = textureApi;
}
