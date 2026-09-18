'use strict';
// The map format (assets/maps/<name>.vmap) in JavaScript, for the map editor: the engine's
// decoder and checks (asset/map.go), its picture of a map (tilemap/picture.go, with the
// borders of tilemap/edge.go), a canonical writer and the cell operations the editor
// paints with.
//
// The picture must be the engine's to the pixel: test/tilemap.test.js compares it with the
// engine's golden images. Go wraps its float products so that arm64 does not fuse them;
// JavaScript never fuses, so plain float64 arithmetic in the same order gives the same
// numbers, and the fields the engine keeps as float32 go through Math.fround. The engine
// stays the authority: what it refuses is reported here, located as it locates it.
(function () {
  const texture = typeof require === 'function' ? require('./texture') : globalThis.vedutaTexture;
  const { goFloat, goQuote, nameError } = texture;
  const f = Math.fround;

  const HEADER = 'map/1';
  const MaxMapSize = 1024;
  const MaxMapLayers = 16;
  const MaxMapObjects = 4096;
  const MaxMapTerrains = 92;
  const MinLayer = -1000;
  const MaxLayer = 1000;

  // ---------------------------------------------------------------- JSON with positions
  // asset.Locator: the value at every path, every key in file order, and syntax errors and
  // duplicate keys reported where they are.

  const quoteChar = (c) => (c === '\'' ? '\'\\\'\'' : c === '"' ? '\'"\'' : `'${goQuote(c).slice(1, -1)}'`);

  // readJSON indexes text: {root, pos (path → offset), keys, error}. A node is {t, v, raw,
  // off, entries | items}.
  function readJSON(text) {
    const pos = new Map();
    const keys = [];
    const n = text.length;
    let i = 0;
    // extra moves the column by bytes: the engine points at the last byte of a character.
    const fail = (off, msg, extra = 0) => {
      throw { off, msg, extra };
    };
    const ws = () => {
      while (i < n) {
        const c = text.charCodeAt(i);
        if (c === 32 || c === 9 || c === 10 || c === 13) {
          i++;
        } else {
          break;
        }
      }
    };
    // invalid fails on the character at i; the engine points past a blank one and the
    // blanks after it.
    const invalid = (what) => {
      const ch = String.fromCodePoint(text.codePointAt(i));
      if (ch === ':' && /^[}\]]/.test(text.slice(i + 1).trimStart())) {
        what = 'looking for beginning of value'; // a separator before a close, as for a comma
      }
      const msg = `invalid JSON: invalid character ${quoteChar(ch)} ${what}`;
      if (' \t\n\r'.includes(ch)) {
        ws();
        fail(i, msg);
      }
      fail(i, msg, texture.utf8Length(ch) - 1);
    };
    const eof = () => fail(n, 'unexpected end of JSON input');
    const str = () => {
      const start = i;
      i++;
      for (;;) {
        if (i >= n) {
          eof();
        }
        const c = text.charCodeAt(i);
        if (c === 34) {
          break;
        }
        if (c < 0x20) {
          const bad = text[i];
          if (' \t\n\r'.includes(bad)) {
            ws();
          }
          fail(i, `invalid JSON: invalid character ${quoteChar(bad)} in string`);
        }
        if (c === 92) {
          const seq = text.slice(i, i + (text[i + 1] === 'u' ? 6 : 2));
          if (i + 1 >= n) {
            eof();
          }
          if (!'"\\/bfnrtu'.includes(text[i + 1]) || (text[i + 1] === 'u' && !/^\\u[0-9a-fA-F]{4}$/.test(seq))) {
            if (seq.length < (text[i + 1] === 'u' ? 6 : 2)) {
              eof();
            }
            fail(i + seq.length - 1, `invalid JSON: invalid escape sequence \`${seq}\` in string`);
          }
          i++;
        }
        i++;
      }
      i++;
      return JSON.parse(text.slice(start, i));
    };
    // trailing steps over a comma, which the engine reports itself when close follows.
    const trailing = () => {
      const comma = i;
      i++;
      ws();
      if (text[i] === '}' || text[i] === ']') {
        fail(comma, 'invalid JSON: invalid character \',\' looking for beginning of value');
      }
    };
    // number reads a JSON number, failing at the first character that cannot go on.
    const digit = () => i < n && text[i] >= '0' && text[i] <= '9';
    const needDigit = () => {
      if (i >= n) {
        eof();
      }
      if (!digit()) {
        invalid('in numeric literal');
      }
    };
    const number = () => {
      const start = i;
      if (text[i] === '-') {
        i++;
        needDigit();
      }
      if (text[i] === '0') {
        i++;
      } else {
        while (digit()) {
          i++;
        }
      }
      if (text[i] === '.') {
        i++;
        needDigit();
        while (digit()) {
          i++;
        }
      }
      if (text[i] === 'e' || text[i] === 'E') {
        i++;
        if (text[i] === '+' || text[i] === '-') {
          i++;
        }
        needDigit();
        while (digit()) {
          i++;
        }
      }
      return text.slice(start, i);
    };
    // memberName fails on a key that is not a string, as the engine's scanner does: a
    // literal or a number is read first, then refused where it starts.
    const memberName = () => {
      const start = i;
      const c = text[i];
      for (const word of ['true', 'false', 'null']) {
        if (c === word[0]) {
          for (const want of word) {
            if (i >= n) {
              eof();
            }
            if (text[i] !== want) {
              invalid(`in literal ${word} (expecting ${quoteChar(want)})`);
            }
            i++;
          }
        }
      }
      if (c === '-' || (c >= '0' && c <= '9')) {
        number();
      }
      if ('tfn{[-0123456789'.includes(c)) {
        fail(start, 'invalid JSON: object member name must be a string');
      }
      invalid('looking for beginning of value');
    };
    const value = (path) => {
      ws();
      if (i >= n) {
        eof();
      }
      const off = i;
      pos.set(path, off);
      const c = text[i];
      const child = (k) => (path === '' ? k : path + '.' + k);
      if (c === '{') {
        i++;
        const node = { t: 'object', off, entries: [] };
        const seen = new Set();
        ws();
        if (text[i] === '}') {
          i++;
          return node;
        }
        for (;;) {
          ws();
          if (i >= n) {
            eof();
          }
          if (text[i] !== '"') {
            memberName();
          }
          const koff = i;
          const name = str();
          if (seen.has(name)) {
            fail(koff, `duplicate key ${goQuote(name)}`);
          }
          seen.add(name);
          keys.push({ name, path: child(name), off: koff });
          ws();
          if (i >= n) {
            eof();
          }
          if (text[i] !== ':') {
            invalid('after object key');
          }
          i++;
          node.entries.push({ name, off: koff, node: value(child(name)) });
          ws();
          if (i >= n) {
            eof();
          }
          if (text[i] === ',') {
            trailing();
            continue;
          }
          if (text[i] === '}') {
            i++;
            return node;
          }
          invalid('after object key:value pair');
        }
      }
      if (c === '[') {
        i++;
        const node = { t: 'array', off, items: [] };
        ws();
        if (text[i] === ']') {
          i++;
          return node;
        }
        for (;;) {
          node.items.push(value(`${path}[${node.items.length}]`));
          ws();
          if (i >= n) {
            eof();
          }
          if (text[i] === ',') {
            trailing();
            continue;
          }
          if (text[i] === ']') {
            i++;
            return node;
          }
          invalid('after array element');
        }
      }
      if (c === '"') {
        return { t: 'string', off, v: str() };
      }
      for (const [word, v] of [['true', true], ['false', false], ['null', null]]) {
        if (c === word[0]) {
          for (const want of word) {
            if (i >= n) {
              eof();
            }
            if (text[i] !== want) {
              invalid(`in literal ${word} (expecting ${quoteChar(want)})`);
            }
            i++;
          }
          return { t: v === null ? 'null' : 'bool', off, v };
        }
      }
      if (c === '-' || (c >= '0' && c <= '9')) {
        const raw = number();
        return { t: 'number', off, v: Number(raw), raw };
      }
      return invalid('looking for beginning of value');
    };
    try {
      ws();
      if (i >= n) {
        return { root: null, pos, keys, error: null }; // no value: not an object
      }
      const root = value('');
      ws();
      if (i < n && (text[i] === '}' || text[i] === ']')) {
        invalid('looking for beginning of value');
      }
      if (i < n) {
        while (i < n && ' \t\n\r,:'.includes(text[i])) {
          i++;
        }
        fail(i, 'unexpected data after the JSON value');
      }
      return { root, pos, keys, error: null };
    } catch (e) {
      if (e && typeof e.off === 'number') {
        return { root: null, pos, keys, error: e };
      }
      throw e;
    }
  }

  // locator turns paths and offsets into 1-based lines and columns, columns in bytes as
  // the engine counts them.
  function locator(text, read) {
    const starts = [0];
    for (let i = 0; i < text.length; i++) {
      if (text.charCodeAt(i) === 10) {
        starts.push(i + 1);
      }
    }
    const lineCol = (off) => {
      let lo = 0;
      let hi = starts.length - 1;
      while (lo < hi) {
        const mid = (lo + hi + 1) >> 1;
        if (starts[mid] <= off) {
          lo = mid;
        } else {
          hi = mid - 1;
        }
      }
      return { line: lo + 1, col: texture.utf8Length(text.slice(starts[lo], off)) + 1 };
    };
    return {
      lineCol,
      has: (path) => read.pos.has(path),
      // at locates the value at path, or its closest ancestor.
      at(path) {
        for (;;) {
          if (read.pos.has(path)) {
            return lineCol(read.pos.get(path));
          }
          if (path === '') {
            return { line: 1, col: 1 };
          }
          path = parentPath(path);
        }
      },
    };
  }

  function parentPath(p) {
    if (p.endsWith(']')) {
      const i = p.lastIndexOf('[');
      if (i >= 0) {
        return p.slice(0, i);
      }
    }
    const i = p.lastIndexOf('.');
    return i >= 0 ? p.slice(0, i) : '';
  }

  // ---------------------------------------------------------------- strict decoding
  // encoding/json into asset.MapSource with DisallowUnknownFields: the first problem in the
  // file stops it, named as the engine names it.

  const T = {
    string: { go: 'string', kind: 'string' },
    int: { go: 'int', kind: 'int' },
    float32: { go: 'float32', kind: 'float32' },
    any: { go: 'interface {}', kind: 'any' },
  };
  const slice = (of) => ({ go: '[]' + of.go, kind: 'slice', of });
  T.terrain = { go: 'asset.MapTerrainSource', kind: 'struct', fields: { key: T.string, name: T.string, texture: T.string, material: T.string, tags: slice(T.string) } };
  T.layer = { go: 'asset.MapLayerSource', kind: 'struct', fields: { name: T.string, z: T.float32, layer: T.int, rows: slice(T.string) } };
  T.object = { go: 'asset.MapObjectSource', kind: 'struct', fields: { name: T.string, at: slice(T.int), size: slice(T.int), tags: slice(T.string), props: { go: 'map[string]interface {}', kind: 'map', of: T.any } } };
  T.map = { go: 'asset.MapSource', kind: 'struct', fields: { veduta: T.string, size: slice(T.int), tile: T.float32, origin: slice(T.float32), terrains: slice(T.terrain), layers: slice(T.layer), objects: slice(T.object) } };

  class decodeError {
    constructor(off, msg) {
      this.off = off;
      this.msg = msg;
    }
  }

  // decode turns node into a value of type t: undefined for null, as Go leaves a nil.
  function decode(node, t, path, read) {
    if (node.t === 'null') {
      return undefined;
    }
    const wrong = (value) => {
      throw new decodeError(node.off, `${path || 'value'}: cannot use JSON ${value} as ${t.go}`);
    };
    const number = (bits) => {
      const v = Number(node.raw);
      if (!isFinite(v) || (bits === 32 && !isFinite(f(v)))) {
        if (bits === 64) {
          throw new decodeError(node.off, `${path}: cannot use JSON number ${node.raw} as float64`);
        }
        wrong('number ' + node.raw);
      }
      return v;
    };
    switch (t.kind) {
      case 'string':
        return node.t === 'string' ? node.v : wrong(node.t === 'number' ? 'number' : node.t);
      case 'int':
        if (node.t !== 'number') {
          wrong(node.t);
        }
        if (!/^-?\d+$/.test(node.raw) || !Number.isSafeInteger(Number(node.raw))) {
          wrong('number ' + node.raw);
        }
        return Number(node.raw);
      case 'float32':
        return node.t === 'number' ? number(32) : wrong(node.t);
      case 'any':
        switch (node.t) {
          case 'number': return number(64);
          case 'array': return node.items.map((x, i) => decode(x, T.any, `${path}[${i}]`, read));
          case 'object': return node.entries.map((e) => [e.name, decode(e.node, T.any, path + '.' + e.name, read)]);
          default: return node.v;
        }
      case 'slice':
        if (node.t !== 'array') {
          wrong(node.t === 'number' ? 'number' : node.t);
        }
        return node.items.map((x, i) => {
          const v = decode(x, t.of, `${path}[${i}]`, read);
          return v === undefined ? zero(t.of) : v;
        });
      case 'map': {
        if (node.t !== 'object') {
          wrong(node.t === 'number' ? 'number' : node.t);
        }
        return node.entries.map((e) => [e.name, decode(e.node, t.of, path + '.' + e.name, read)]);
      }
      default: {
        if (node.t !== 'object') {
          wrong(node.t === 'number' ? 'number' : node.t);
        }
        const out = {};
        for (const e of node.entries) {
          // encoding/json matches a key to a field exactly, else ignoring case.
          const name = t.fields[e.name] ? e.name : Object.keys(t.fields).find((k) => k.toLowerCase() === e.name.toLowerCase());
          if (!name) {
            // Located as the engine locates it: the first key of that name in the file.
            const k = read.keys.find((x) => x.name === e.name);
            throw new decodeError(k.off, `${k.path}: unknown field`);
          }
          out[name] = decode(e.node, t.fields[name], path === '' ? e.name : path + '.' + e.name, read);
        }
        return out;
      }
    }
  }

  // zero is what Go puts in a slice for a null element.
  function zero(t) {
    switch (t.kind) {
      case 'string': return '';
      case 'int':
      case 'float32': return 0;
      case 'struct': return {};
      default: return undefined;
    }
  }

  // ---------------------------------------------------------------- the checks of asset/map.go

  const validKey = (k) => k > 0x20 && k < 0x7f && k !== 0x22 && k !== 0x5c;
  const goInts = (a) => (a ? `[${a.join(' ')}]` : '[]');
  const bytesOf = (s) => new TextEncoder().encode(s);

  // compile validates a decoded source like asset.CompileMap and returns the map the
  // editor works on, or null, with every problem as {path, msg}.
  function compile(name, src) {
    const errors = [];
    const err = (path, msg) => errors.push({ path, msg });
    const checkName = (path, s) => {
      const why = nameError(s);
      if (why) {
        err(path, why);
      }
      return !why;
    };
    const tags = (path, list) => {
      const out = [];
      (list || []).forEach((tag, k) => {
        if (!checkName(`${path}[${k}]`, tag)) {
          return;
        }
        if (out.includes(tag)) {
          err(`${path}[${k}]`, `duplicate ${goQuote(tag)}`);
          return;
        }
        out.push(tag);
      });
      return out;
    };
    const nameBad = nameError(name);
    if (nameBad) {
      err('', 'map ' + nameBad);
    }
    const m = { name, w: 1, h: 1, tile: src.tile, origin: src.origin, terrains: [], layers: [], objects: [], hasObjects: src.objects !== undefined };
    if (src.tile !== undefined && !(f(src.tile) > 0)) {
      err('tile', `must be a positive number, got ${goFloat(f(src.tile), true)}`);
    }
    if (src.origin !== undefined && src.origin.length !== 3) {
      err('origin', `want 3 numbers, got ${src.origin.length}`);
    }
    if (src.size === undefined) {
      err('size', 'is required ([columns, rows])');
    } else if (src.size.length !== 2) {
      err('size', `want [columns, rows], got ${src.size.length} numbers`);
    } else {
      let ok = true;
      src.size.forEach((v, i) => {
        if (v < 1 || v > MaxMapSize) {
          err(`size[${i}]`, `${v} out of range [1, ${MaxMapSize}]`);
          ok = false;
        }
      });
      if (ok) {
        m.w = src.size[0];
        m.h = src.size[1];
      }
    }
    const keys = new Map(); // key byte → terrain index + 1
    const names = new Map();
    const terrains = src.terrains || [];
    if (terrains.length === 0) {
      err('terrains', 'at least one terrain is required');
    } else if (terrains.length > MaxMapTerrains) {
      err('terrains', `${terrains.length} terrains, want at most ${MaxMapTerrains}`);
    }
    terrains.forEach((t, i) => {
      const p = `terrains[${i}]`;
      const key = t.key || '';
      const kb = bytesOf(key);
      if (kb.length !== 1 || !validKey(kb[0])) {
        err(p + '.key', `must be one printable ASCII character but space, '"' and '\\', got ${goQuote(key)}`);
      } else if (keys.has(kb[0])) {
        err(p + '.key', `duplicate key ${goQuote(key)} (first used by terrains[${keys.get(kb[0]) - 1}])`);
      } else {
        keys.set(kb[0], i + 1);
      }
      const tn = t.name || '';
      if (tn === '') {
        err(p + '.name', 'is required');
      } else if (checkName(p + '.name', tn)) {
        if (names.has(tn)) {
          err(p + '.name', `duplicate terrain name ${goQuote(tn)} (first used by terrains[${names.get(tn)}])`);
        } else {
          names.set(tn, i);
        }
      }
      const tx = t.texture || '';
      const mt = t.material || '';
      if (tx === '' && mt === '') {
        err(p, 'needs a texture or a material');
      } else if (tx !== '' && mt !== '') {
        err(p + '.material', 'not allowed with texture: a terrain is drawn with one or the other');
      } else if (tx !== '') {
        checkName(p + '.texture', tx);
      } else {
        checkName(p + '.material', mt);
      }
      const tt = tags(p + '.tags', t.tags);
      m.terrains.push({ key, name: tn, texture: tx, material: mt, tags: t.tags === undefined ? undefined : tt });
    });
    const layers = src.layers || [];
    if (layers.length === 0) {
      err('layers', 'at least one layer is required');
    } else if (layers.length > MaxMapLayers) {
      err('layers', `${layers.length} layers, want at most ${MaxMapLayers}`);
    }
    const layerNames = new Map();
    layers.forEach((l, i) => {
      const p = `layers[${i}]`;
      if (l.layer !== undefined && l.layer !== 0 && (l.layer < MinLayer || l.layer > MaxLayer)) {
        err(p + '.layer', `${l.layer} out of range [${MinLayer}, ${MaxLayer}]`);
      }
      const ln = l.name || '';
      if (ln === '') {
        err(p + '.name', 'is required');
      } else if (checkName(p + '.name', ln)) {
        if (layerNames.has(ln)) {
          err(p + '.name', `duplicate layer name ${goQuote(ln)} (first used by layers[${layerNames.get(ln)}])`);
        } else {
          layerNames.set(ln, i);
        }
      }
      if (l.z !== undefined && (f(l.z) < -1e6 || f(l.z) > 1e6)) {
        err(p + '.z', `${goFloat(f(l.z), true)} out of range [-1e+06, 1e+06]`);
      }
      const cells = new Uint8Array(m.w * m.h);
      const rows = l.rows || [];
      if (rows.length !== m.h) {
        err(p + '.rows', `${rows.length} rows, want ${m.h} (the map's size)`);
      }
      for (let y = 0; y < rows.length && y < m.h; y++) {
        const rp = `${p}.rows[${y}]`;
        const row = bytesOf(rows[y]);
        if (row.length !== m.w) {
          err(rp, `${row.length} characters, want ${m.w} (the map's size; a space is an empty cell)`);
          continue;
        }
        for (let x = 0; x < m.w; x++) {
          const k = row[x];
          if (k === 0x20) {
            continue;
          }
          if (!keys.has(k)) {
            err(rp, `character ${goQuote(String.fromCharCode(k))} at column ${x} is not a terrain's key (a space is an empty cell)`);
            break;
          }
          cells[y * m.w + x] = keys.get(k);
        }
      }
      m.layers.push({ name: ln, z: l.z, layer: l.layer, cells });
    });
    const objects = src.objects || [];
    if (objects.length > MaxMapObjects) {
      err('objects', `${objects.length} objects, want at most ${MaxMapObjects}`);
    }
    const objNames = new Map();
    objects.forEach((o, i) => {
      const p = `objects[${i}]`;
      const on = o.name || '';
      let x = 0;
      let y = 0;
      if (on === '') {
        err(p + '.name', 'is required');
      } else if (checkName(p + '.name', on)) {
        if (objNames.has(on)) {
          err(p + '.name', `duplicate object name ${goQuote(on)} (first used by objects[${objNames.get(on)}])`);
        } else {
          objNames.set(on, i);
        }
      }
      if (o.at === undefined) {
        err(p + '.at', 'is required ([column, row] of its top-left cell)');
      } else if (o.at.length !== 2) {
        err(p + '.at', `want [column, row], got ${o.at.length} numbers`);
      } else if (o.at[0] < 0 || o.at[1] < 0 || o.at[0] >= m.w || o.at[1] >= m.h) {
        err(p + '.at', `cell ${goInts(o.at)} is outside the ${m.w} × ${m.h} map`);
      } else {
        x = o.at[0];
        y = o.at[1];
      }
      if (o.size !== undefined) {
        if (o.size.length !== 2) {
          err(p + '.size', `want [columns, rows], got ${o.size.length} numbers`);
        } else if (o.size[0] < 1 || o.size[1] < 1 || x + o.size[0] > m.w || y + o.size[1] > m.h) {
          err(p + '.size', `${goInts(o.size)} cells from ${goInts(o.at)} leave the ${m.w} × ${m.h} map (each at least 1)`);
        }
      }
      const ot = tags(p + '.tags', o.tags);
      const props = o.props || [];
      for (const [k, v] of props.slice().sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0))) {
        const pp = `${p}.props.${k}`;
        if (k === '' || texture.utf8Length(k) > 64 || /[. \t\n]/.test(k)) {
          err(pp, `a property name is 1-64 characters without spaces or '.', got ${goQuote(k)}`);
          continue;
        }
        if (typeof v !== 'string' && typeof v !== 'boolean' && typeof v !== 'number') {
          err(pp, 'must be a string, a number or a boolean');
        }
      }
      m.objects.push({ name: on, at: o.at, size: o.size, tags: o.tags === undefined ? undefined : ot, props: o.props });
    });
    return { map: errors.length === 0 ? m : null, errors };
  }

  // load reads the text of a map file called file (its name is the map's) the way
  // asset.ParseMap does: {map, errors}, each error {path, msg, line, col}.
  function load(text, file) {
    const read = readJSON(text);
    const loc = locator(text, read);
    const located = (list) => list.map((e) => {
      const at = e.off !== undefined ? loc.lineCol(e.off) : loc.at(e.path);
      at.col += e.extra || 0;
      return Object.assign({ path: e.path, msg: e.msg }, at);
    });
    if (read.error) {
      return { map: null, errors: located([{ path: '', msg: read.error.msg, off: read.error.off, extra: read.error.extra }]) };
    }
    if (!read.root || read.root.t !== 'object') {
      return { map: null, errors: located([{ path: '', msg: 'source must be a JSON object' }]) };
    }
    // As json.Unmarshal fills a *string: null leaves it nil, a value of another type
    // leaves it "".
    let header;
    for (const e of read.root.entries) {
      if (e.name.toLowerCase() === 'veduta') {
        header = e.node.t === 'string' ? e.node.v : e.node.t === 'null' ? undefined : '';
      }
    }
    if (header === undefined) {
      return { map: null, errors: located([{ path: '', msg: `missing "veduta" header (want "${HEADER}")` }]) };
    }
    if (header !== HEADER) {
      return { map: null, errors: located([{ path: 'veduta', msg: `header is ${goQuote(header)}, want "${HEADER}"` }]) };
    }
    let src;
    try {
      src = decode(read.root, T.map, '', read);
    } catch (e) {
      if (e instanceof decodeError) {
        const i = e.msg.indexOf(': ');
        return { map: null, errors: located([{ path: e.msg.slice(0, i), msg: e.msg.slice(i + 2), off: e.off }]) };
      }
      throw e;
    }
    const base = String(file || '').split(/[\\/]/).pop();
    if (!base.endsWith('.vmap') || base.length === 5) {
      return { map: null, errors: [{ path: '', msg: `file name ${goQuote(base)} must end in ".vmap" with a non-empty name`, line: 0, col: 0 }] };
    }
    const name = base.slice(0, -5);
    const bad = nameError(name);
    if (bad) {
      return { map: null, errors: [{ path: '', msg: 'file name: map ' + bad, line: 0, col: 0 }] };
    }
    const out = compile(name, src);
    return { map: out.map, errors: located(out.errors) };
  }

  // describe writes an error as the engine's tools do: line:col: path: message.
  function describe(e) {
    return (e.line > 0 ? `${e.line}:${e.col}: ` : '') + (e.path ? e.path + ': ' : '') + e.msg;
  }

  // ---------------------------------------------------------------- the canonical text

  const js = (v) => JSON.stringify(v);
  const num = (n) => String(n);
  const list = (a, item) => `[${a.map(item).join(', ')}]`;

  // rows writes the cells of a layer, a terrain's key per cell and a space for none.
  function rows(m, l) {
    const keys = m.terrains.map((t) => t.key);
    const out = [];
    for (let y = 0; y < m.h; y++) {
      let s = '';
      for (let x = 0; x < m.w; x++) {
        const v = m.layers[l].cells[y * m.w + x];
        s += v === 0 ? ' ' : keys[v - 1];
      }
      out.push(s);
    }
    return out;
  }

  function propValue(v) {
    return typeof v === 'number' ? num(v) : js(v);
  }

  // format writes a map as the editor saves it: two-space indents, a terrain or an object
  // per line, a row of cells per line, the keys the map has and no others, in a fixed order.
  function format(m) {
    const out = ['{', `  "veduta": "${HEADER}",`, `  "size": [${m.w}, ${m.h}],`];
    if (m.tile !== undefined) {
      out.push(`  "tile": ${num(m.tile)},`);
    }
    if (m.origin !== undefined) {
      out.push(`  "origin": ${list(m.origin, num)},`);
    }
    out.push('  "terrains": [');
    m.terrains.forEach((t, i) => {
      const parts = [`"key": ${js(t.key)}`, `"name": ${js(t.name)}`];
      parts.push(t.material ? `"material": ${js(t.material)}` : `"texture": ${js(t.texture)}`);
      if (t.tags !== undefined) {
        parts.push(`"tags": ${list(t.tags, js)}`);
      }
      out.push(`    { ${parts.join(', ')} }${i < m.terrains.length - 1 ? ',' : ''}`);
    });
    out.push('  ],', '  "layers": [');
    m.layers.forEach((l, i) => {
      const head = [`"name": ${js(l.name)}`];
      if (l.z !== undefined) {
        head.push(`"z": ${num(l.z)}`);
      }
      if (l.layer !== undefined) {
        head.push(`"layer": ${num(l.layer)}`);
      }
      out.push(`    { ${head.join(', ')}, "rows": [`);
      const r = rows(m, i);
      r.forEach((row, y) => out.push(`      ${js(row)}${y < r.length - 1 ? ',' : ''}`));
      out.push(`    ] }${i < m.layers.length - 1 ? ',' : ''}`);
    });
    if (m.hasObjects) {
      out.push('  ],');
      if (m.objects.length === 0) {
        out.push('  "objects": []');
      } else {
        out.push('  "objects": [');
        m.objects.forEach((o, i) => {
          const parts = [`"name": ${js(o.name)}`, `"at": ${list(o.at, num)}`];
          if (o.size !== undefined) {
            parts.push(`"size": ${list(o.size, num)}`);
          }
          if (o.tags !== undefined) {
            parts.push(`"tags": ${list(o.tags, js)}`);
          }
          if (o.props !== undefined) {
            parts.push(o.props.length === 0 ? '"props": {}' : `"props": { ${o.props.map(([k, v]) => `${js(k)}: ${propValue(v)}`).join(', ')} }`);
          }
          out.push(`    { ${parts.join(', ')} }${i < m.objects.length - 1 ? ',' : ''}`);
        });
        out.push('  ]');
      }
    } else {
      out.push('  ]');
    }
    out.push('}', '');
    return out.join('\n');
  }

  // clone copies a map deeply, so an edit can be tried and dropped.
  function clone(m) {
    return {
      name: m.name, w: m.w, h: m.h, tile: m.tile, origin: m.origin && m.origin.slice(), hasObjects: m.hasObjects,
      terrains: m.terrains.map((t) => Object.assign({}, t, { tags: t.tags && t.tags.slice() })),
      layers: m.layers.map((l) => Object.assign({}, l, { cells: l.cells.slice() })),
      objects: m.objects.map((o) => Object.assign({}, o, {
        at: o.at.slice(), size: o.size && o.size.slice(), tags: o.tags && o.tags.slice(), props: o.props && o.props.map((p) => p.slice()),
      })),
    };
  }

  // ---------------------------------------------------------------- cell operations
  // Each returns the cells it changed, [x, y] pairs, so the editor redraws those and the
  // cells around them.

  function inside(m, x, y) {
    return x >= 0 && y >= 0 && x < m.w && y < m.h;
  }

  function get(m, l, x, y) {
    return inside(m, x, y) ? m.layers[l].cells[y * m.w + x] : 0;
  }

  // paint sets cell (x, y) of layer l to v (a terrain index + 1, 0 for empty).
  function paint(m, l, x, y, v, changed = []) {
    if (inside(m, x, y) && m.layers[l].cells[y * m.w + x] !== v) {
      m.layers[l].cells[y * m.w + x] = v;
      changed.push([x, y]);
    }
    return changed;
  }

  // brush paints a size × size square around (x, y): centred for odd sizes, the extra
  // row and column right and below for even ones.
  function brush(m, l, x, y, size, v, changed = []) {
    const o = Math.floor((size - 1) / 2);
    for (let j = 0; j < size; j++) {
      for (let i = 0; i < size; i++) {
        paint(m, l, x - o + i, y - o + j, v, changed);
      }
    }
    return changed;
  }

  // rect paints the cells between two corners, both included.
  function rect(m, l, x0, y0, x1, y1, v, changed = []) {
    for (let y = Math.max(0, Math.min(y0, y1)); y <= Math.min(m.h - 1, Math.max(y0, y1)); y++) {
      for (let x = Math.max(0, Math.min(x0, x1)); x <= Math.min(m.w - 1, Math.max(x0, x1)); x++) {
        paint(m, l, x, y, v, changed);
      }
    }
    return changed;
  }

  // flood paints the cells of layer l 4-connected to (x, y) that have its terrain.
  function flood(m, l, x, y, v, changed = []) {
    if (!inside(m, x, y)) {
      return changed;
    }
    const cells = m.layers[l].cells;
    const from = cells[y * m.w + x];
    if (from === v) {
      return changed;
    }
    const stack = [y * m.w + x];
    while (stack.length > 0) {
      const i = stack.pop();
      if (cells[i] !== from) {
        continue;
      }
      cells[i] = v;
      const cx = i % m.w;
      const cy = (i - cx) / m.w;
      changed.push([cx, cy]);
      if (cx > 0) stack.push(i - 1);
      if (cx < m.w - 1) stack.push(i + 1);
      if (cy > 0) stack.push(i - m.w);
      if (cy < m.h - 1) stack.push(i + m.w);
    }
    return changed;
  }

  // resize gives the map w × h cells keeping its top-left: new cells are empty, objects
  // that start off the map are dropped and the others cut to fit. It returns the names of
  // the objects dropped.
  function resize(m, w, h) {
    for (const l of m.layers) {
      const cells = new Uint8Array(w * h);
      for (let y = 0; y < Math.min(h, m.h); y++) {
        cells.set(l.cells.subarray(y * m.w, y * m.w + Math.min(w, m.w)), y * w);
      }
      l.cells = cells;
    }
    const dropped = [];
    m.objects = m.objects.filter((o) => {
      if (o.at[0] >= w || o.at[1] >= h) {
        dropped.push(o.name);
        return false;
      }
      if (o.size) {
        o.size = [Math.min(o.size[0], w - o.at[0]), Math.min(o.size[1], h - o.at[1])];
      }
      return true;
    });
    m.w = w;
    m.h = h;
    return dropped;
  }

  // KEYS is the order new terrains take their key in: marks that read as ground first,
  // then letters and digits, then the rest of printable ASCII.
  const KEYS = '.~#=+*o%&@' + 'abcdefghijklmnopqrstuvwxyz' + 'ABCDEFGHIJKLMNOPQRSTUVWXYZ' + '0123456789' +
    Array.from({ length: 94 }, (_, i) => String.fromCharCode(33 + i)).filter((c) => c !== '"' && c !== '\\').join('');

  // freeKey returns the first key of KEYS no terrain has, or ''.
  function freeKey(m) {
    for (const k of KEYS) {
      if (!m.terrains.some((t) => t.key === k)) {
        return k;
      }
    }
    return '';
  }

  // freeName returns base, or base_2, base_3… when a name in taken has it.
  function freeName(base, taken) {
    if (!taken.includes(base)) {
      return base;
    }
    for (let i = 2; ; i++) {
      const s = `${base}_${i}`.slice(-64);
      if (!taken.includes(s)) {
        return s;
      }
    }
  }

  // addTerrain adds a terrain painted with texture: its name the texture's (made unique)
  // and the first free key. It returns its index, or -1 when the map has all it can hold.
  function addTerrain(m, textureName) {
    const key = freeKey(m);
    if (!key || m.terrains.length >= MaxMapTerrains) {
      return -1;
    }
    m.terrains.push({ key, name: freeName(textureName, m.terrains.map((t) => t.name)), texture: textureName, material: '', tags: undefined });
    return m.terrains.length - 1;
  }

  // removeTerrain deletes terrain i and empties the cells painted with it.
  function removeTerrain(m, i) {
    m.terrains.splice(i, 1);
    for (const l of m.layers) {
      const c = l.cells;
      for (let k = 0; k < c.length; k++) {
        if (c[k] === i + 1) {
          c[k] = 0;
        } else if (c[k] > i + 1) {
          c[k]--;
        }
      }
    }
  }

  // uses counts the cells painted with terrain i.
  function uses(m, i) {
    let n = 0;
    for (const l of m.layers) {
      for (const v of l.cells) {
        if (v === i + 1) {
          n++;
        }
      }
    }
    return n;
  }

  // ---------------------------------------------------------------- borders (tilemap/edge.go)

  const shapeH = 0;
  const shapeV = 1;
  const shapeL = 2;
  const shapeCorner = 3;
  const SHAPES = 4;
  const quarterNW = 0;
  const quarterNE = 1;
  const quarterSW = 2;
  const QUARTERS = 4;
  const sideN = 0;
  const sideS = 1;
  const sideW = 2;
  const sideE = 3;
  const wanderSteps = 4;

  // hash32 mixes a seed, a side and a step into 32 bits (the finalizer of MurmurHash3).
  function hash32(seed, side, step) {
    let h = ((seed >>> 0) ^ Math.imul(side, 0x9e3779b1) ^ Math.imul(step, 0x85ebca77)) >>> 0;
    h = (h ^ (h >>> 16)) >>> 0;
    h = Math.imul(h, 0x7feb352d) >>> 0;
    h = (h ^ (h >>> 15)) >>> 0;
    h = Math.imul(h, 0x846ca68b) >>> 0;
    h = (h ^ (h >>> 16)) >>> 0;
    return h;
  }

  // wander is the offset in [-1, 1] of side at t along it: 0 at both ends.
  function wander(seed, side, t) {
    const x = t * wanderSteps;
    let i = Math.trunc(x);
    if (i >= wanderSteps) {
      i = wanderSteps - 1;
    }
    const fr = x - i;
    const v = (k) => (k === 0 || k === wanderSteps ? 0 : ((hash32(seed, side, k) >>> 8) * 2 - (1 << 24)) / (1 << 24));
    const a = v(i);
    const b = v(i + 1);
    const s = fr * fr * (3 - 2 * fr);
    return a + (b - a) * s;
  }

  // reach is how far the border along side reaches into the cell at t, in pixels.
  function reach(e, side, t, half) {
    const w = e.width;
    const r = w * (1 + e.roughness * wander(e.seed, side, t));
    return Math.min(Math.max(r, 0), half);
  }

  // inShape reports whether pixel (px, py) of a fw × fh frame is covered by shape s of
  // quarter q.
  function inShape(e, s, q, px, py, fw, fh) {
    const cx = px + 0.5;
    const cy = py + 0.5;
    const w = fw;
    const h = fh;
    const half = Math.min(w, h) / 2;
    const top = q === quarterNW || q === quarterNE;
    const left = q === quarterNW || q === quarterSW;
    const horizontal = () => (top ? cy < reach(e, sideN, cx / w, half) : h - cy < reach(e, sideS, cx / w, half));
    const vertical = () => (left ? cx < reach(e, sideW, cy / h, half) : w - cx < reach(e, sideE, cy / h, half));
    switch (s) {
      case shapeH: return horizontal();
      case shapeV: return vertical();
      case shapeL: return horizontal() || vertical();
      default: break;
    }
    const ox = left ? 0 : w;
    const oy = top ? 0 : h;
    const dx = cx - ox;
    const dy = cy - oy;
    const r = Math.min(e.width, half);
    return dx * dx + dy * dy < r * r;
  }

  // edgeAtlas is the edge atlas of a texture with an edge (tilemap.EdgeAtlas): per frame, a
  // band of 4 × 4 quarter images (shapes across, quarters down), each padded by a pixel
  // repeating its border.
  function edgeAtlas(t) {
    const src = t.image;
    const gc = Math.max(t.grid[0], 1);
    const gr = Math.max(t.grid[1], 1);
    const fw = Math.trunc(src.w / gc);
    const fh = Math.trunc(src.h / gr);
    const qw = fw >> 1;
    const qh = fh >> 1;
    const cw = qw + 2;
    const ch = qh + 2;
    const frames = gc * gr;
    const w = SHAPES * cw;
    const h = QUARTERS * ch * frames;
    const data = new Uint8ClampedArray(w * h * 4);
    for (let fr = 0; fr < frames; fr++) {
      const fx = (fr % gc) * fw;
      const fy = Math.trunc(fr / gc) * fh;
      for (let q = 0; q < QUARTERS; q++) {
        const qx = (q % 2) * qw;
        const qy = (q >> 1) * qh;
        for (let s = 0; s < SHAPES; s++) {
          const ox = s * cw;
          const oy = (fr * QUARTERS + q) * ch;
          for (let j = -1; j <= qh; j++) {
            for (let i = -1; i <= qw; i++) {
              const px = qx + Math.min(Math.max(i, 0), qw - 1);
              const py = qy + Math.min(Math.max(j, 0), qh - 1);
              if (inShape(t.edge, s, q, px, py, fw, fh)) {
                const k = ((fy + py) * src.w + fx + px) * 4;
                const o = ((oy + j + 1) * w + ox + i + 1) * 4;
                data[o] = src.data[k];
                data[o + 1] = src.data[k + 1];
                data[o + 2] = src.data[k + 2];
                data[o + 3] = src.data[k + 3];
              }
            }
          }
        }
      }
    }
    return { w, h, data, grid: frames > 1 ? [1, frames] : [0, 0], clips: t.clips, play: t.play };
  }

  // ---------------------------------------------------------------- the picture (tilemap/picture.go)

  // textureOf turns what lib/texture.js compiled into what a map draws with.
  function textureOf(name, compiled) {
    const s = compiled.spec;
    return { name, image: compiled.image, grid: s.grid, clips: s.clips, play: s.play, edge: s.edge, autotile: !!s.autotile };
  }

  // prepare works out how every terrain is drawn (tilemap.Map.prepare): its texture, and
  // for one with an edge its priority, its rank among the edges and its atlas. lib is
  // {textures: name → texture, materials: name → the texture it names ('' for none)}.
  function prepare(m, lib) {
    const atlases = new Map();
    const draws = m.terrains.map((t) => {
      const d = { tex: null, edge: null, priority: 0, rank: 0, frame: [0, 0], atlas: null, auto: 0 };
      let base = true;
      if (t.material) {
        base = Object.prototype.hasOwnProperty.call(lib.materials || {}, t.material);
        d.tex = base ? (lib.textures[lib.materials[t.material]] || null) : null;
      } else {
        d.tex = lib.textures[t.texture] || null;
      }
      const tx = d.tex;
      if (tx && tx.autotile && base && tx.image) {
        d.auto = Math.trunc(Math.trunc(tx.image.w / Math.max(tx.grid[0], 1)) / AUTO_COLS);
      }
      if (!tx || !tx.edge || !base || !tx.image) {
        return d;
      }
      d.edge = tx.edge;
      d.priority = tx.edge.priority;
      d.frame = [Math.trunc(tx.image.w / Math.max(tx.grid[0], 1)), Math.trunc(tx.image.h / Math.max(tx.grid[1], 1))];
      if (!atlases.has(tx.name)) {
        atlases.set(tx.name, edgeAtlas(tx));
      }
      d.atlas = atlases.get(tx.name);
      return d;
    });
    const withEdge = draws.map((d, i) => i).filter((i) => draws[i].edge);
    withEdge.sort((a, b) => draws[a].priority - draws[b].priority || a - b);
    withEdge.forEach((i, r) => {
      draws[i].rank = r + 1;
    });
    return draws;
  }

  // cellSize is the pixels of a cell: the largest frame of the terrains' textures, or tile
  // of an autotile.
  function cellSize(draws) {
    let w = 1;
    let h = 1;
    for (const d of draws) {
      if (d.auto > 0) {
        w = Math.max(w, d.auto); // a tile of it
        h = Math.max(h, d.auto);
      } else if (d.tex && d.tex.image) {
        w = Math.max(w, Math.trunc(d.tex.image.w / Math.max(d.tex.grid[0], 1)));
        h = Math.max(h, Math.trunc(d.tex.image.h / Math.max(d.tex.grid[1], 1)));
      }
    }
    return [w, h];
  }

  const NEAR = [[0, -1], [1, -1], [1, 0], [1, 1], [0, 1], [-1, 1], [-1, 0], [-1, -1]]; // N, NE, E, SE, S, SW, W, NW
  const QUARTER_SIDES = [[0, 6, 7], [0, 2, 1], [4, 6, 5], [4, 2, 3]]; // above or below, left or right, diagonal

  // cellEdges calls fn(u, shape, quarter) for every border drawn in cell (x, y) of layer l,
  // in the engine's order: by neighbour (N, NE, E, SE, S, SW, W, NW, the first of each
  // terrain), then by quarter.
  const near = new Int32Array(8);
  function cellEdges(m, draws, l, x, y, fn) {
    const v = get(m, l, x, y);
    const p = v === 0 ? -1 : draws[v - 1].priority;
    let any = false;
    for (let k = 0; k < 8; k++) {
      const u = get(m, l, x + NEAR[k][0], y + NEAR[k][1]);
      near[k] = u;
      any = any || (u !== 0 && draws[u - 1].edge !== null && draws[u - 1].priority > p);
    }
    if (!any) {
      return;
    }
    for (let k = 0; k < 8; k++) {
      const u = near[k];
      if (u === 0 || !draws[u - 1].edge || draws[u - 1].priority <= p) {
        continue;
      }
      let seen = false;
      for (let j = 0; j < k; j++) {
        seen = seen || near[j] === u;
      }
      if (seen) {
        continue;
      }
      for (let q = 0; q < 4; q++) {
        const [a, b, c] = QUARTER_SIDES[q];
        const hz = near[a] === u;
        const vt = near[b] === u;
        const dg = near[c] === u;
        if (hz && vt) {
          fn(u, shapeL, q);
        } else if (hz) {
          fn(u, shapeH, q);
        } else if (vt) {
          fn(u, shapeV, q);
        } else if (dg) {
          fn(u, shapeCorner, q);
        }
      }
    }
  }

  // ---------------------------------------------------------------- autotiles (tilemap/autotile.go)
  // A frame is 6 × 3 tiles: an island (0–2) and a lake (3–5, its middle unused). Each
  // quarter of a cell falls in a class from its two sides and its corner; a cell whose four
  // classes are a tile's, as it sits in its drawing, draws the whole tile, any other each
  // quarter from the tile of its class.

  const AUTO_COLS = 6;
  const LAKE_EMPTY = 1 * AUTO_COLS + 4;
  const FULL = 0;
  const INNER = 1;
  const CONVEX = 2;
  const V_STRAIGHT = 3;
  const V_CONCAVE = 4;
  const H_STRAIGHT = 5;
  const H_CONCAVE = 6;
  const QUARTER_BITS = [[1, 64, 128], [1, 4, 2], [16, 64, 32], [16, 4, 8]]; // above or below, left or right, corner

  // quarterClass is the class of quarter q of a cell whose same neighbours are mask (bit k
  // for NEAR[k]).
  function quarterClass(mask, q) {
    const [a, b, c] = QUARTER_BITS[q];
    const v = (mask & a) !== 0;
    const h = (mask & b) !== 0;
    const d = (mask & c) !== 0;
    if (v && h) {
      return d ? FULL : INNER;
    }
    if (!v && !h) {
      return CONVEX;
    }
    if (!v) {
      return d ? V_CONCAVE : V_STRAIGHT;
    }
    return d ? H_CONCAVE : H_STRAIGHT;
  }

  // CLASS_TILE is, per class and quarter, the tile a quarter of that class is cut from.
  const CLASS_TILE = [[7, 7, 7, 7], [17, 15, 5, 3], [0, 2, 12, 14], [1, 1, 13, 13], [16, 16, 4, 4], [6, 8, 6, 8], [11, 9, 11, 9]];

  // TILE_CLASSES is, per tile, its quarters' classes packed 4 bits each (-1: the lake's middle).
  const TILE_CLASSES = (() => {
    const out = [];
    for (let t = 0; t < AUTO_COLS * 3; t++) {
      const x = t % AUTO_COLS;
      const y = Math.trunc(t / AUTO_COLS);
      const same = (nx, ny) => (x < 3 ? nx >= 0 && nx < 3 && ny >= 0 && ny < 3 : nx !== 4 || ny !== 1);
      let mask = 0;
      for (let k = 0; k < 8; k++) {
        if (same(x + NEAR[k][0], y + NEAR[k][1])) {
          mask |= 1 << k;
        }
      }
      let packed = 0;
      for (let q = 0; q < 4; q++) {
        packed |= quarterClass(mask, q) << (q * 4);
      }
      out.push(t === LAKE_EMPTY ? -1 : packed);
    }
    return out;
  })();

  // autoPick returns the tile of each quarter of a cell whose same neighbours are mask, and
  // whether they are one whole tile.
  function autoPick(mask) {
    let packed = 0;
    for (let q = 0; q < 4; q++) {
      packed |= quarterClass(mask, q) << (q * 4);
    }
    const t = TILE_CLASSES.indexOf(packed);
    if (t >= 0) {
      return { tiles: [t, t, t, t], whole: true };
    }
    return { tiles: [0, 1, 2, 3].map((q) => CLASS_TILE[(packed >> (q * 4)) & 15][q]), whole: false };
  }

  // autoMask returns which neighbours of cell (x, y) of layer l are its terrain v; one off
  // the map counts as v.
  function autoMask(m, l, x, y, v) {
    let mask = 0;
    for (let k = 0; k < 8; k++) {
      const nx = x + NEAR[k][0];
      const ny = y + NEAR[k][1];
      if (!inside(m, nx, ny) || get(m, l, nx, ny) === v) {
        mask |= 1 << k;
      }
    }
    return mask;
  }

  // quarterRect is quarter q of the cw × ch cell at (x, y): an odd cell's right and bottom
  // quarters are a pixel larger.
  function quarterRect(q, x, y, cw, ch) {
    const hw = cw >> 1;
    const hh = ch >> 1;
    return [q % 2 === 1 ? x + hw : x, q >> 1 === 1 ? y + hh : y, q % 2 === 1 ? cw - hw : hw, q >> 1 === 1 ? ch - hh : hh];
  }

  // frameAt is the frame of its sheet a texture shows at tick: its play clip's, else 0.
  function frameAt(t, tick, rate) {
    const c = t.play ? texture.clipOf(t, t.play) : null;
    return c && rate > 0 ? texture.clipFrame(c, tick, rate) : 0;
  }

  // words views the pixels of an image as 32-bit words (little-endian: alpha on top).
  const wordViews = new WeakMap();
  function words(img) {
    let w = wordViews.get(img.data);
    if (!w) {
      w = new Uint32Array(img.data.buffer, img.data.byteOffset, img.data.length >> 2);
      wordViews.set(img.data, w);
    }
    return w;
  }

  // blit draws the part (sx, sy, sw, sh) of image s into (dx, dy, dw, dh) of img, nearest
  // texel, cut out below half alpha.
  function blit(img, s, sx, sy, sw, sh, dx, dy, dw, dh) {
    const sd = words(s);
    const dd = words(img);
    for (let j = 0; j < dh; j++) {
      const row = (sy + (sh === dh ? j : Math.trunc((j * sh) / dh))) * s.w + sx;
      let o = (dy + j) * img.w + dx;
      if (sw === dw) {
        for (let i = 0; i < dw; i++, o++) {
          const c = sd[row + i];
          if (c >>> 24 >= 128) {
            dd[o] = c | 0xff000000;
          }
        }
        continue;
      }
      for (let i = 0; i < dw; i++, o++) {
        const c = sd[row + Math.trunc((i * sw) / dw)];
        if (c >>> 24 >= 128) {
          dd[o] = c | 0xff000000;
        }
      }
    }
  }

  // Picture draws a map cell by cell. A cell's pixels depend only on its own cells and
  // their borders, so drawing some cells again gives what drawing the whole map gives.
  class Picture {
    // opts: cell ([w, h], default the largest frame), hidden (layer indices not drawn),
    // missing(terrain index) → [r, g, b] for a terrain without a texture (the engine draws
    // nothing).
    constructor(m, lib, opts = {}) {
      this.m = m;
      this.draws = prepare(m, lib);
      [this.cw, this.ch] = opts.cell || cellSize(this.draws);
      this.hidden = opts.hidden || [];
      this.missing = opts.missing || null;
      this.w = m.w * this.cw;
      this.h = m.h * this.ch;
      this.data = new Uint8ClampedArray(this.w * this.h * 4);
      this.order = m.layers.map((l, i) => i);
      const z = (i) => (m.layers[i].z !== undefined ? f(m.layers[i].z) : f(i / 10));
      const layer = (i) => m.layers[i].layer || 0;
      this.order.sort((a, b) => layer(a) - layer(b) || z(a) - z(b) || a - b);
      // The borders of a cell, packed terrain << 8 | shape << 4 | quarter.
      this.eu = new Int32Array(64);
      this.ne = 0;
      this.collect = (u, shape, quarter) => {
        this.eu[this.ne++] = (u << 8) | (shape << 4) | quarter;
      };
      this.setTick(0, 0);
    }

    // setTick picks the frames of tick; it returns the terrain indices whose frame changed.
    setTick(tick, rate) {
      const changed = [];
      const next = this.draws.map((d, i) => {
        if (!d.tex) {
          return [0, 0];
        }
        const fr = frameAt(d.tex, tick, rate);
        const cell = fr % (Math.max(d.tex.grid[0], 1) * Math.max(d.tex.grid[1], 1));
        const edge = d.atlas ? fr % Math.max(d.atlas.grid[1], 1) : 0;
        if (this.frames && (this.frames[i][0] !== cell || this.frames[i][1] !== edge)) {
          changed.push(i);
        }
        return [cell, edge];
      });
      this.frames = next;
      return changed;
    }

    // drawAll draws every cell.
    drawAll() {
      this.data.fill(0);
      for (let y = 0; y < this.m.h; y++) {
        for (let x = 0; x < this.m.w; x++) {
          this.drawCell(x, y, false);
        }
      }
    }

    // drawRect draws the cells from (x0, y0) to (x1, y1), both included.
    drawRect(x0, y0, x1, y1) {
      for (let y = Math.max(0, y0); y <= Math.min(this.m.h - 1, y1); y++) {
        for (let x = Math.max(0, x0); x <= Math.min(this.m.w - 1, x1); x++) {
          this.drawCell(x, y, true);
        }
      }
    }

    drawCell(x, y, clear) {
      const { m, draws, cw, ch } = this;
      if (clear) {
        for (let j = 0; j < ch; j++) {
          this.data.fill(0, ((y * ch + j) * this.w + x * cw) * 4, ((y * ch + j) * this.w + (x + 1) * cw) * 4);
        }
      }
      const eu = this.eu;
      for (let li = 0; li < this.order.length; li++) {
        const l = this.order[li];
        if (this.hidden.includes(l)) {
          continue;
        }
        const v = m.layers[l].cells[y * m.w + x];
        if (v > 0 && draws[v - 1].tex && draws[v - 1].tex.image) {
          const t = draws[v - 1].tex;
          const gc = Math.max(t.grid[0], 1);
          const gr = Math.max(t.grid[1], 1);
          const fw = Math.trunc(t.image.w / gc);
          const fh = Math.trunc(t.image.h / gr);
          const fr = this.frames[v - 1][0];
          const ts = draws[v - 1].auto;
          if (ts > 0) {
            const { tiles, whole } = autoPick(autoMask(m, l, x, y, v));
            for (let q = 0; q < 4; q++) {
              const sx = (fr % gc) * fw + (tiles[q] % AUTO_COLS) * ts;
              const sy = Math.trunc(fr / gc) * fh + Math.trunc(tiles[q] / AUTO_COLS) * ts;
              if (whole) {
                blit(this, t.image, sx, sy, ts, ts, x * cw, y * ch, cw, ch);
                break;
              }
              const [dx, dy, dw, dh] = quarterRect(q, x * cw, y * ch, cw, ch);
              blit(this, t.image, sx + (q % 2) * (ts >> 1), sy + (q >> 1) * (ts >> 1), ts >> 1, ts >> 1, dx, dy, dw, dh);
            }
          } else {
            blit(this, t.image, (fr % gc) * fw, Math.trunc(fr / gc) * fh, fw, fh, x * cw, y * ch, cw, ch);
          }
        } else if (v > 0 && this.missing) {
          this.fillMissing(x, y, this.missing(v - 1));
        }
        this.ne = 0;
        cellEdges(m, draws, l, x, y, this.collect);
        // Borders of a higher rank over lower ones (insertion sort, stable); one terrain's
        // never overlap.
        for (let a = 1; a < this.ne; a++) {
          const k = eu[a];
          const r = draws[(k >> 8) - 1].rank;
          let b = a - 1;
          while (b >= 0 && draws[(eu[b] >> 8) - 1].rank > r) {
            eu[b + 1] = eu[b];
            b--;
          }
          eu[b + 1] = k;
        }
        for (let e = 0; e < this.ne; e++) {
          const u = eu[e] >> 8;
          const shape = (eu[e] >> 4) & 15;
          const quarter = eu[e] & 15;
          const d = draws[u - 1];
          const qw = d.frame[0] >> 1;
          const qh = d.frame[1] >> 1;
          const band = QUARTERS * (qh + 2);
          const sx = shape * (qw + 2) + 1;
          const sy = this.frames[u - 1][1] * band + quarter * (qh + 2) + 1;
          const [dx, dy, dw, dh] = quarterRect(quarter, x * cw, y * ch, cw, ch);
          blit(this, d.atlas, sx, sy, qw, qh, dx, dy, dw, dh);
        }
      }
    }

    // fillMissing marks a cell whose terrain has no texture with stripes of its colour.
    fillMissing(x, y, [r, g, b]) {
      for (let j = 0; j < this.ch; j++) {
        for (let i = 0; i < this.cw; i++) {
          const o = ((y * this.ch + j) * this.w + x * this.cw + i) * 4;
          const stripe = ((i + j) >> 2) & 1;
          this.data[o] = stripe ? r : r >> 1;
          this.data[o + 1] = stripe ? g : g >> 1;
          this.data[o + 2] = stripe ? b : b >> 1;
          this.data[o + 3] = 255;
        }
      }
    }

    // touches reports whether cell (x, y) or a neighbour, on a drawn layer, has one of the
    // terrains (indices): what to draw again when their frames change.
    touches(x, y, terrains) {
      for (let l = 0; l < this.m.layers.length; l++) {
        if (this.hidden.includes(l)) {
          continue;
        }
        for (let dy = -1; dy <= 1; dy++) {
          for (let dx = -1; dx <= 1; dx++) {
            const v = get(this.m, l, x + dx, y + dy);
            if (v > 0 && terrains.includes(v - 1)) {
              return true;
            }
          }
        }
      }
      return false;
    }
  }

  // picture composes the map as tilemap.Map.Picture does, at tick (rate ticks a second).
  function picture(m, lib, tick, rate) {
    const p = new Picture(m, lib);
    p.setTick(tick, rate);
    p.drawAll();
    return { w: p.w, h: p.h, data: p.data };
  }

  const tilemapApi = {
    HEADER, MaxMapSize, MaxMapLayers, MaxMapObjects, MaxMapTerrains, MinLayer, MaxLayer, KEYS,
    readJSON, load, compile, describe, format, clone, rows,
    inside, get, paint, brush, rect, flood, resize, freeKey, freeName, addTerrain, removeTerrain, uses,
    hash32, wander, inShape, edgeAtlas, textureOf, prepare, cellSize, cellEdges, frameAt, Picture, picture,
    quarterClass, autoPick, autoMask, AUTO_COLS, LAKE_EMPTY,
  };
  // The webview loads this file with a script tag, after texture.js; Node with require.
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = tilemapApi;
  } else {
    globalThis.vedutaTilemap = tilemapApi;
  }
})();
