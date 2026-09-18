'use strict';
// The tile editor's model, without VS Code or a page: a pixel texture (a .vtex whose one
// layer is a PNG as large as the texture) read into frames and written back, the drawing
// operations on a frame, and the autotile preview. The editor's webview and the tests load
// the same file.

(function () {
  const texture = typeof require === 'function' ? require('./texture') : globalThis.vedutaTexture;
  const tilemap = typeof require === 'function' ? require('./tilemap') : globalThis.vedutaTilemap;

  const KINDS = ['tile', 'animated', 'autotile'];
  const SIZES = [8, 16, 24, 32, 48, 64];
  const CLIP = 'loop'; // the clip the editor makes for an animation
  const DEFAULT_FPS = 6;

  // The fields the editor writes; anything else in the file is kept as it is.
  const ORDER = ['veduta', 'size', 'tiling', 'mipmaps', 'layers', 'grid', 'frames', 'clips', 'play', 'edge', 'autotile'];

  // TILE_NAMES names the tiles of an autotile frame, column + 6 × row.
  const TILE_NAMES = [
    'island NW', 'island N', 'island NE', 'lake NW', 'lake N', 'lake NE',
    'island W', 'island middle', 'island E', 'lake W', 'lake middle (not used)', 'lake E',
    'island SW', 'island S', 'island SE', 'lake SW', 'lake S', 'lake SE',
  ];

  // pixelTexture tells whether a texture source is one the editor draws: a PNG image as its
  // only layer, without frames drawn one by one. It returns {path} or {error}.
  function pixelTexture(src) {
    if (!src || typeof src !== 'object' || Array.isArray(src)) {
      return { error: 'the file is not a texture object' };
    }
    const layers = src.layers;
    if (src.frames !== undefined) {
      return { error: 'its frames are drawn by layers ("frames"), not a PNG' };
    }
    if (!Array.isArray(layers) || layers.length !== 1 || !layers[0] || layers[0].type !== 'image') {
      return { error: 'it is a layer program, not one PNG image' };
    }
    const l = layers[0];
    const extra = Object.keys(l).filter((k) => !['type', 'path', 'fit', 'opacity', 'blend'].includes(k));
    if (extra.length > 0) {
      return { error: `its image has ${extra.map((k) => JSON.stringify(k)).join(', ')}` };
    }
    if (typeof l.path !== 'string' || texture.badImagePath(l.path) !== '') {
      return { error: 'its image path is not a PNG in the assets directory' };
    }
    return { path: l.path };
  }

  // read turns a texture source and its decoded PNG ({w, h, data}, or null when the file is
  // not there yet) into the editor's model: the frame size, the frames (RGBA each), whether
  // it is an autotile and the fps of its animation.
  function read(src, image) {
    const p = pixelTexture(src);
    if (p.error) {
      return { error: p.error };
    }
    const size = Array.isArray(src.size) && src.size.length === 2 ? src.size : null;
    if (!size || !size.every((v) => Number.isInteger(v) && v >= 1 && v <= texture.MaxSize)) {
      return { error: 'its size is not [width, height]' };
    }
    const [W, H] = size;
    if (image && (image.w !== W || image.h !== H)) {
      return { error: `the PNG is ${image.w} × ${image.h} pixels, the texture's size ${W} × ${H}` };
    }
    let gc = 1;
    let gr = 1;
    if (src.grid !== undefined) {
      const g = src.grid;
      if (!Array.isArray(g) || g.length !== 2 || !g.every((v) => Number.isInteger(v) && v >= 1) || W % g[0] !== 0 || H % g[1] !== 0) {
        return { error: 'its grid does not cut the image into frames' };
      }
      [gc, gr] = g;
    }
    const w = W / gc;
    const h = H / gr;
    const frames = [];
    for (let f = 0; f < gc * gr; f++) {
      const fx = (f % gc) * w;
      const fy = Math.floor(f / gc) * h;
      const px = new Uint8ClampedArray(w * h * 4);
      if (image) {
        for (let y = 0; y < h; y++) {
          px.set(image.data.subarray(((fy + y) * W + fx) * 4, ((fy + y) * W + fx + w) * 4), y * w * 4);
        }
      }
      frames.push(px);
    }
    const clip = src.clips && typeof src.play === 'string' ? src.clips[src.play] : null;
    const fps = clip && typeof clip.fps === 'number' && clip.fps > 0 ? clip.fps : DEFAULT_FPS;
    return { w, h, frames, autotile: src.autotile === true, fps, path: p.path };
  }

  // sheet puts the frames side by side: the PNG the editor writes.
  function sheet(model) {
    const { w, h, frames } = model;
    const W = w * frames.length;
    const data = new Uint8ClampedArray(W * h * 4);
    frames.forEach((px, f) => {
      for (let y = 0; y < h; y++) {
        data.set(px.subarray(y * w * 4, (y + 1) * w * 4), (y * W + f * w) * 4);
      }
    });
    return { w: W, h, data };
  }

  // write returns the texture source for the model, from the file's own source (null for a
  // new one): size, image, grid, the animation's clip and autotile are the editor's; the
  // rest is kept.
  function write(model, src, pngPath) {
    const n = model.frames.length;
    const out = Object.assign({}, src || {});
    out.veduta = texture.HEADER;
    out.size = [model.w * n, model.h];
    const old = src && Array.isArray(src.layers) && src.layers[0] && src.layers[0].type === 'image' ? src.layers[0] : {};
    out.layers = [Object.assign({ type: 'image' }, old, { path: pngPath })];
    const clips = out.clips && typeof out.clips === 'object' ? JSON.parse(JSON.stringify(out.clips)) : {};
    const name = typeof out.play === 'string' && out.play !== '' ? out.play : CLIP;
    if (n > 1) {
      out.grid = [n, 1];
      clips[name] = Object.assign({}, clips[name] || {}, { frames: [...Array(n).keys()], fps: model.fps });
      out.play = name;
      delete out.tiling; // a grid of frames does not tile
    } else {
      delete out.grid;
      if (out.play) {
        delete clips[out.play];
        delete out.play;
      }
    }
    // Clips of frames no longer there lose them.
    for (const k of Object.keys(clips)) {
      const c = clips[k];
      if (c && Array.isArray(c.frames)) {
        c.frames = c.frames.filter((f) => !Number.isInteger(f) || f < n);
        if (c.frames.length === 0) {
          delete clips[k];
        }
      }
    }
    for (const k of Object.keys(clips)) {
      const next = clips[k] && clips[k].next;
      if (typeof next === 'string' && !clips[next]) {
        delete clips[k].next;
      }
    }
    if (Object.keys(clips).length > 0) {
      out.clips = clips;
    } else {
      delete out.clips;
    }
    if (model.autotile) {
      out.autotile = true;
      delete out.edge;
      delete out.tiling;
    } else {
      delete out.autotile;
    }
    const ordered = {};
    for (const k of ORDER) {
      if (out[k] !== undefined) {
        ordered[k] = out[k];
      }
    }
    for (const k of Object.keys(out)) {
      if (!ORDER.includes(k)) {
        ordered[k] = out[k];
      }
    }
    return ordered;
  }

  // format writes a source as the files the tool makes look: two-space indents, short
  // arrays and small objects on one line.
  function format(v) {
    const one = (x) => {
      if (Array.isArray(x)) {
        return '[' + x.map(one).join(', ') + ']';
      }
      if (x && typeof x === 'object') {
        const ks = Object.keys(x);
        return ks.length === 0 ? '{}' : '{ ' + ks.map((k) => `${JSON.stringify(k)}: ${one(x[k])}`).join(', ') + ' }';
      }
      return JSON.stringify(x);
    };
    const lines = ['{'];
    const keys = Object.keys(v);
    keys.forEach((k, i) => {
      const x = v[k];
      let body;
      if (Array.isArray(x) && x.some((e) => e && typeof e === 'object')) {
        body = '[\n' + x.map((e) => '    ' + one(e)).join(',\n') + '\n  ]';
      } else if (x && typeof x === 'object' && !Array.isArray(x) && JSON.stringify(x).length > 60) {
        const ks = Object.keys(x);
        body = '{\n' + ks.map((kk) => `    ${JSON.stringify(kk)}: ${one(x[kk])}`).join(',\n') + '\n  }';
      } else {
        body = one(x);
      }
      lines.push(`  ${JSON.stringify(k)}: ${body}${i < keys.length - 1 ? ',' : ''}`);
    });
    lines.push('}');
    return lines.join('\n') + '\n';
  }

  // blank returns the model of a new texture: a tile, a tile with frames or an autotile,
  // of tiles size pixels.
  function blank(kind, size) {
    const auto = kind === 'autotile';
    const w = auto ? size * 6 : size;
    const h = auto ? size * 3 : size;
    const n = kind === 'animated' ? 2 : 1;
    return { w, h, frames: Array.from({ length: n }, () => new Uint8ClampedArray(w * h * 4)), autotile: auto, fps: DEFAULT_FPS };
  }

  // newSource is the source of a new texture of the kind: a plain tile repeats (a map
  // draws cells of it side by side as one).
  function newSource(kind, model, pngPath) {
    const src = kind === 'tile' ? { veduta: texture.HEADER, tiling: true } : null;
    return write(model, src, pngPath);
  }

  // checkSize tells what is wrong with a tile size for the kind, or ''.
  function checkSize(kind, size) {
    if (!Number.isInteger(size) || size < 1 || size > 256) {
      return 'a whole number of pixels, 1 to 256';
    }
    if (kind === 'autotile' && size % 2 !== 0) {
      return 'an autotile\'s tiles have an even size (a map cuts them in quarters)';
    }
    return '';
  }

  // ------------------------------------------------------------------ drawing on a frame
  // A frame is w × h RGBA pixels; a color is [r, g, b, a].

  function getPixel(px, w, x, y) {
    const o = (y * w + x) * 4;
    return [px[o], px[o + 1], px[o + 2], px[o + 3]];
  }

  // setPixel sets a pixel inside the frame (clip, when given, is [x0, y0, x1, y1] the
  // pixels may be set in, x1 and y1 excluded); it tells whether it changed.
  function setPixel(px, w, h, x, y, c, clip) {
    if (x < 0 || y < 0 || x >= w || y >= h) {
      return false;
    }
    if (clip && (x < clip[0] || y < clip[1] || x >= clip[2] || y >= clip[3])) {
      return false;
    }
    const o = (y * w + x) * 4;
    if (px[o] === c[0] && px[o + 1] === c[1] && px[o + 2] === c[2] && px[o + 3] === c[3]) {
      return false;
    }
    px[o] = c[0];
    px[o + 1] = c[1];
    px[o + 2] = c[2];
    px[o + 3] = c[3];
    return true;
  }

  // linePoints returns the pixels of the line from (x0, y0) to (x1, y1), both included.
  function linePoints(x0, y0, x1, y1) {
    const out = [];
    const dx = Math.abs(x1 - x0);
    const dy = -Math.abs(y1 - y0);
    const sx = x0 < x1 ? 1 : -1;
    const sy = y0 < y1 ? 1 : -1;
    let err = dx + dy;
    for (;;) {
      out.push([x0, y0]);
      if (x0 === x1 && y0 === y1) {
        return out;
      }
      const e2 = 2 * err;
      if (e2 >= dy) {
        err += dy;
        x0 += sx;
      }
      if (e2 <= dx) {
        err += dx;
        y0 += sy;
      }
    }
  }

  // rectPoints returns the pixels of the rectangle between two corners: its outline, or
  // all of it when filled.
  function rectPoints(x0, y0, x1, y1, filled) {
    const out = [];
    const [ax, bx] = x0 < x1 ? [x0, x1] : [x1, x0];
    const [ay, by] = y0 < y1 ? [y0, y1] : [y1, y0];
    for (let y = ay; y <= by; y++) {
      for (let x = ax; x <= bx; x++) {
        if (filled || y === ay || y === by || x === ax || x === bx) {
          out.push([x, y]);
        }
      }
    }
    return out;
  }

  // flood paints c over the pixels of the same color as (x, y) that touch it by a side,
  // within clip; it returns how many changed.
  function flood(px, w, h, x, y, c, clip) {
    const [x0, y0, x1, y1] = clip || [0, 0, w, h];
    if (x < x0 || y < y0 || x >= x1 || y >= y1) {
      return 0;
    }
    const from = getPixel(px, w, x, y);
    if (from.every((v, i) => v === c[i])) {
      return 0;
    }
    const same = (i) => px[i] === from[0] && px[i + 1] === from[1] && px[i + 2] === from[2] && px[i + 3] === from[3];
    const stack = [[x, y]];
    let n = 0;
    while (stack.length > 0) {
      const [cx, cy] = stack.pop();
      if (cx < x0 || cy < y0 || cx >= x1 || cy >= y1 || !same((cy * w + cx) * 4)) {
        continue;
      }
      setPixel(px, w, h, cx, cy, c);
      n++;
      stack.push([cx + 1, cy], [cx - 1, cy], [cx, cy + 1], [cx, cy - 1]);
    }
    return n;
  }

  // copyRect returns the pixels of a rectangle of a frame: {w, h, data}.
  function copyRect(px, w, x, y, rw, rh) {
    const data = new Uint8ClampedArray(rw * rh * 4);
    for (let j = 0; j < rh; j++) {
      data.set(px.subarray(((y + j) * w + x) * 4, ((y + j) * w + x + rw) * 4), j * rw * 4);
    }
    return { w: rw, h: rh, data };
  }

  // pasteRect puts a block of pixels at (x, y), cut at the frame's sides; transparent
  // pixels of the block are pasted too unless over is set.
  function pasteRect(px, w, h, block, x, y, over) {
    for (let j = 0; j < block.h; j++) {
      for (let i = 0; i < block.w; i++) {
        const o = (j * block.w + i) * 4;
        if (over && block.data[o + 3] === 0) {
          continue;
        }
        setPixel(px, w, h, x + i, y + j, [block.data[o], block.data[o + 1], block.data[o + 2], block.data[o + 3]]);
      }
    }
  }

  // flipBlock mirrors a block left to right (horizontal) or top to bottom.
  function flipBlock(b, horizontal) {
    const data = new Uint8ClampedArray(b.data.length);
    for (let j = 0; j < b.h; j++) {
      for (let i = 0; i < b.w; i++) {
        const si = horizontal ? b.w - 1 - i : i;
        const sj = horizontal ? j : b.h - 1 - j;
        data.set(b.data.subarray((sj * b.w + si) * 4, (sj * b.w + si) * 4 + 4), (j * b.w + i) * 4);
      }
    }
    return { w: b.w, h: b.h, data };
  }

  // rotateBlock turns a block a quarter clockwise.
  function rotateBlock(b) {
    const data = new Uint8ClampedArray(b.data.length);
    for (let j = 0; j < b.h; j++) {
      for (let i = 0; i < b.w; i++) {
        // (i, j) goes to (h - 1 - j, i) of a block h wide.
        data.set(b.data.subarray((j * b.w + i) * 4, (j * b.w + i) * 4 + 4), (i * b.h + (b.h - 1 - j)) * 4);
      }
    }
    return { w: b.h, h: b.w, data };
  }

  // colors lists the colors of a frame, most used first (at most max), as [r, g, b, a];
  // fully transparent pixels are left out.
  function colors(px, max = 64) {
    const count = new Map();
    for (let i = 0; i < px.length; i += 4) {
      if (px[i + 3] === 0) {
        continue;
      }
      const k = ((px[i] << 24) | (px[i + 1] << 16) | (px[i + 2] << 8) | px[i + 3]) >>> 0;
      count.set(k, (count.get(k) || 0) + 1);
    }
    return [...count.entries()].sort((a, b) => b[1] - a[1] || a[0] - b[0]).slice(0, max)
      .map(([k]) => [k >>> 24, (k >>> 16) & 255, (k >>> 8) & 255, k & 255]);
  }

  function hex(c) {
    const h = (v) => v.toString(16).padStart(2, '0');
    return '#' + h(c[0]) + h(c[1]) + h(c[2]) + (c[3] === 255 ? '' : h(c[3]));
  }

  // parseHex reads #RGB, #RRGGBB or #RRGGBBAA; null when it is none of them.
  function parseHex(s) {
    const m = /^#?([0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/i.exec(String(s).trim());
    if (!m) {
      return null;
    }
    let d = m[1];
    if (d.length === 3) {
      d = d.split('').map((x) => x + x).join('');
    }
    if (d.length === 6) {
      d += 'ff';
    }
    return [0, 2, 4, 6].map((i) => parseInt(d.slice(i, i + 2), 16));
  }

  // ------------------------------------------------------------------ autotiles

  // tileAt names the autotile tile under pixel (x, y) of a frame of tiles ts large.
  function tileAt(ts, x, y) {
    const t = Math.floor(y / ts) * 6 + Math.floor(x / ts);
    return { index: t, name: TILE_NAMES[t] || '' };
  }

  // SAMPLE is the map the autotile preview draws: islands, a lake, lines, lone cells,
  // corners that touch.
  const SAMPLE = [
    '............',
    '.####...#...',
    '.#..#..###..',
    '.####...#...',
    '............',
    '.#####..#.#.',
    '.##.##...#..',
    '.#####..#.#.',
    '............',
  ];

  // lakeFromIsland starts the lake from the island: its sides are the island's opposite
  // sides (the lake's top tile has the empty cell below it, as the island's bottom tile
  // does), its corners the island's middle, for the concave corner to be drawn in.
  function lakeFromIsland(px, ts) {
    const w = ts * 6;
    const copy = (from, to) => {
      const b = copyRect(px, w, (from % 6) * ts, Math.floor(from / 6) * ts, ts, ts);
      pasteRect(px, w, ts * 3, b, (to % 6) * ts, Math.floor(to / 6) * ts, false);
    };
    copy(13, 4); // lake N ← island S
    copy(1, 16); // lake S ← island N
    copy(8, 9); // lake W ← island E
    copy(6, 11); // lake E ← island W
    for (const t of [3, 5, 15, 17]) {
      copy(7, t);
    }
  }

  // autoPreview draws the sample map with an autotile frame (tiles ts large) over bg, as a
  // map draws it: {w, h, data}.
  function autoPreview(px, ts, rows = SAMPLE) {
    const cols = rows[0].length;
    const W = cols * ts;
    const H = rows.length * ts;
    const data = new Uint8ClampedArray(W * H * 4);
    const fw = ts * 6;
    const at = (x, y) => (x < 0 || y < 0 || x >= cols || y >= rows.length ? true : rows[y][x] === '#');
    const near = [[0, -1], [1, -1], [1, 0], [1, 1], [0, 1], [-1, 1], [-1, 0], [-1, -1]];
    const half = ts >> 1;
    for (let y = 0; y < rows.length; y++) {
      for (let x = 0; x < cols; x++) {
        if (!at(x, y)) {
          continue;
        }
        let mask = 0;
        near.forEach(([dx, dy], k) => {
          if (at(x + dx, y + dy)) {
            mask |= 1 << k;
          }
        });
        const { tiles, whole } = tilemap.autoPick(mask);
        for (let q = 0; q < 4; q++) {
          const tx = (tiles[q] % 6) * ts;
          const ty = Math.floor(tiles[q] / 6) * ts;
          const ox = whole ? 0 : (q % 2) * half;
          const oy = whole ? 0 : (q >> 1) * half;
          const size = whole ? ts : half;
          for (let j = 0; j < size; j++) {
            const s = ((ty + oy + j) * fw + tx + ox) * 4;
            data.set(px.subarray(s, s + size * 4), ((y * ts + oy + j) * W + x * ts + ox) * 4);
          }
          if (whole) {
            break;
          }
        }
      }
    }
    return { w: W, h: H, data };
  }

  const tilesetApi = {
    KINDS, SIZES, CLIP, DEFAULT_FPS, TILE_NAMES, SAMPLE,
    pixelTexture, read, sheet, write, format, blank, newSource, checkSize,
    getPixel, setPixel, linePoints, rectPoints, flood, copyRect, pasteRect, flipBlock, rotateBlock, colors, hex, parseHex,
    tileAt, lakeFromIsland, autoPreview,
  };
  // The webview loads this file with a script tag, after texture.js and tilemap.js; Node
  // with require.
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = tilesetApi;
  } else {
    globalThis.vedutaTileset = tilesetApi;
  }
})();
