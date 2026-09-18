'use strict';
// The map editor: paints a .vmap with the textures it names. What it draws comes from
// lib/tilemap.js, the engine's picture of a map; what it saves is the map written the one
// canonical way, sent to the extension as the document's new text after every gesture, so
// VS Code's undo, redo, dirty state and save work as for any text file.
(function () {
  const vscode = acquireVsCodeApi();
  const tex = globalThis.vedutaTexture;
  const tm = globalThis.vedutaTilemap;
  const png = globalThis.vedutaPng;

  const SIZES = [1, 2, 3, 5];
  const TOOLS = { b: 'brush', r: 'rect', f: 'fill', e: 'eraser', i: 'pick', o: 'object' };
  const MAX_PIXELS = 1 << 24; // the picture's pixels; bigger maps get smaller cells
  const MAX_SIDE = 16384;

  const el = (id) => document.getElementById(id);
  const canvas = el('canvas');
  const ctx = canvas.getContext('2d');
  const off = document.createElement('canvas');
  const offCtx = off.getContext('2d');

  const saved = vscode.getState() || {};
  const state = {
    file: '',
    text: null, // the document text the model was read from, or last sent
    map: null, // the map being edited (lib/tilemap.js), null while the text does not load
    errors: [],
    lib: { textures: {}, materials: {} },
    textureErrors: {}, // texture name → why it is not drawn
    textureNames: [],
    tickRate: 20,
    pic: null,
    image: null,
    tool: saved.tool || 'brush',
    size: saved.size || 1,
    terrain: 0, // index of the terrain painted with
    layer: 0, // index of the layer painted on
    hidden: new Set(), // names of the layers not drawn (the editor's own, not saved)
    grid: saved.grid !== false,
    zoom: 0, // screen pixels per picture pixel; 0 until the first fit
    panX: 0,
    panY: 0,
    selected: -1, // the object shown in the form
    animate: false,
    tick: 0,
    timer: null,
    hover: null, // the cell under the pointer
    drag: null, // the gesture under way
    space: false,
    confirms: new Map(),
  };

  function save() {
    vscode.setState({ tool: state.tool, size: state.size, grid: state.grid });
  }

  // ---------------------------------------------------------------- assets

  const decoded = new Map();
  function imageOf(p, base64) {
    const was = decoded.get(p);
    if (was && was.base64 === base64) {
      return was.img;
    }
    let img;
    try {
      const s = atob(base64);
      const bytes = new Uint8Array(s.length);
      for (let i = 0; i < s.length; i++) {
        bytes[i] = s.charCodeAt(i);
      }
      img = png.decode(bytes);
    } catch (e) {
      img = { error: String((e && e.message) || e) };
    }
    decoded.set(p, { base64, img });
    return img;
  }

  // compiled keeps what a texture source drew, so a new message draws only what changed.
  const compiled = new Map();
  function assets(msg) {
    const images = {};
    for (const p of Object.keys(msg.images || {})) {
      const v = msg.images[p];
      images[p] = !v || v.error ? { error: (v && v.error) || 'not read' } : imageOf(p, v);
    }
    const textures = {};
    const errors = {};
    for (const name of Object.keys(msg.textures || {})) {
      const t = msg.textures[name];
      if (t.error) {
        errors[name] = t.error;
        continue;
      }
      const key = t.text + '\n' + tex.deps(tex.parse(t.text).src).map((p) => (msg.images[p] && msg.images[p].length) || 0).join(',');
      let out = compiled.get(name);
      if (!out || out.key !== key) {
        const parsed = tex.parse(t.text);
        out = { key, result: parsed.src ? tex.compile(parsed.src, images) : { errors: [{ path: '', msg: parsed.error }] } };
        compiled.set(name, out);
      }
      if (out.result.errors.length > 0) {
        errors[name] = `${t.file}: ${out.result.errors[0].path ? out.result.errors[0].path + ': ' : ''}${out.result.errors[0].msg}`;
        continue;
      }
      textures[name] = tm.textureOf(name, out.result);
    }
    state.lib = { textures, materials: msg.materials || {} };
    state.textureErrors = errors;
    state.textureNames = msg.textureNames || [];
    state.tickRate = msg.tickRate > 0 ? msg.tickRate : 20;
    if (state.animate) {
      startAnimation();
    }
    texturePicker();
    rebuild();
    panels();
  }

  // ---------------------------------------------------------------- the document

  // documentText takes the document's text: a text the editor sent itself changes nothing,
  // another replaces the map, redrawing only the cells that differ when it can.
  function documentText(msg) {
    state.file = msg.file;
    const text = msg.text.replace(/\r\n/g, '\n'); // what the editor sent, in a CRLF document
    if (text === state.text && state.map) {
      return;
    }
    state.text = text;
    const out = tm.load(msg.text, msg.file);
    state.errors = out.errors;
    document.body.classList.toggle('broken', !out.map);
    el('error').hidden = !!out.map;
    if (!out.map) {
      const lines = out.errors.slice(0, 20).map(tm.describe);
      if (out.errors.length > 20) {
        lines.push(`and ${out.errors.length - 20} more`);
      }
      el('errors').textContent = lines.join('\n');
      cancelDrag();
      return;
    }
    const was = state.map;
    state.map = out.map;
    state.hidden = new Set([...state.hidden].filter((n) => out.map.layers.some((l) => l.name === n)));
    state.terrain = Math.min(state.terrain, out.map.terrains.length - 1);
    state.layer = Math.min(state.layer, out.map.layers.length - 1);
    if (state.selected >= out.map.objects.length) {
      state.selected = -1;
    }
    cancelDrag();
    if (was && state.pic && sameLook(was, out.map)) {
      const changed = [];
      out.map.layers.forEach((l, i) => {
        const before = was.layers[i].cells;
        for (let k = 0; k < l.cells.length; k++) {
          if (l.cells[k] !== before[k]) {
            changed.push([k % out.map.w, Math.floor(k / out.map.w)]);
          }
        }
      });
      state.pic.m = out.map;
      redrawCells(changed);
    } else {
      rebuild();
    }
    panels();
    if (!state.zoom) {
      fit();
    }
  }

  // sameLook reports whether two maps draw alike but for their cells: the picture can be
  // kept and only the cells that differ drawn again.
  function sameLook(a, b) {
    return a.w === b.w && a.h === b.h && a.layers.length === b.layers.length && a.terrains.length === b.terrains.length &&
      a.terrains.every((t, i) => t.texture === b.terrains[i].texture && t.material === b.terrains[i].material) &&
      a.layers.every((l, i) => l.name === b.layers[i].name && l.z === b.layers[i].z && l.layer === b.layers[i].layer);
  }

  // commit sends the map, written canonically, as the document's new text: one edit per
  // gesture, which VS Code undoes as one.
  function commit() {
    if (!state.map) {
      return;
    }
    const text = tm.format(state.map);
    if (text === state.text) {
      return;
    }
    state.text = text;
    vscode.postMessage({ type: 'edit', text });
  }

  // ---------------------------------------------------------------- the picture

  function colorOf(name) {
    let h = 0;
    for (const c of name) {
      h = (Math.imul(h, 31) + c.charCodeAt(0)) >>> 0;
    }
    const hue = h % 360;
    const f = (n) => {
      const k = (n + hue / 30) % 12;
      return Math.round(255 * (0.55 - 0.35 * Math.max(-1, Math.min(k - 3, 9 - k, 1))));
    };
    return [f(0), f(8), f(4)];
  }

  // rebuild draws the whole map again: its terrains, layers or size changed.
  function rebuild() {
    const m = state.map;
    if (!m) {
      return;
    }
    let [cw, ch] = tm.cellSize(tm.prepare(m, state.lib));
    if (cw === 1 && ch === 1) {
      cw = ch = 16; // no texture yet: cells big enough to see
    }
    const s = Math.min(1, Math.sqrt(MAX_PIXELS / (m.w * cw * m.h * ch)), MAX_SIDE / (m.w * cw), MAX_SIDE / (m.h * ch));
    if (s < 1) {
      cw = Math.max(1, Math.floor(cw * s));
      ch = Math.max(1, Math.floor(ch * s));
    }
    const hidden = m.layers.map((l, i) => (state.hidden.has(l.name) ? i : -1)).filter((i) => i >= 0);
    if (state.pic && state.zoom && state.pic.cw !== cw) {
      state.zoom *= state.pic.cw / cw; // the map keeps its size on screen
    }
    state.pic = new tm.Picture(m, state.lib, { cell: [cw, ch], hidden, missing: (i) => colorOf(m.terrains[i].name) });
    state.pic.setTick(state.animate ? state.tick : 0, state.tickRate);
    state.pic.drawAll();
    off.width = state.pic.w;
    off.height = state.pic.h;
    state.image = new ImageData(state.pic.data, state.pic.w, state.pic.h);
    offCtx.putImageData(state.image, 0, 0);
    request();
  }

  // redrawCells draws again the cells given and those around them (their borders), and
  // copies the chunks they are in to the picture on screen.
  function redrawCells(cells) {
    const pic = state.pic;
    if (!pic || cells.length === 0) {
      return;
    }
    const C = 16;
    const chunks = new Set();
    const around = new Set();
    for (const [x, y] of cells) {
      for (let dy = -1; dy <= 1; dy++) {
        for (let dx = -1; dx <= 1; dx++) {
          if (tm.inside(pic.m, x + dx, y + dy)) {
            around.add((y + dy) * pic.m.w + x + dx);
          }
        }
      }
    }
    for (const k of around) {
      const cx = k % pic.m.w;
      const cy = (k - cx) / pic.m.w;
      pic.drawCell(cx, cy, true);
      chunks.add(Math.floor(cy / C) * 4096 + Math.floor(cx / C));
    }
    for (const k of chunks) {
      const cx = k % 4096;
      const cy = Math.floor(k / 4096);
      const w = Math.min(C, pic.m.w - cx * C) * pic.cw;
      const h = Math.min(C, pic.m.h - cy * C) * pic.ch;
      offCtx.putImageData(state.image, 0, 0, cx * C * pic.cw, cy * C * pic.ch, w, h);
    }
    request();
  }

  // ---------------------------------------------------------------- the view

  let pending = false;
  function request() {
    if (!pending) {
      pending = true;
      requestAnimationFrame(draw);
    }
  }

  function viewSize() {
    const r = el('view').getBoundingClientRect();
    return { w: Math.max(1, r.width), h: Math.max(1, r.height) };
  }

  function fit() {
    const pic = state.pic;
    if (!pic) {
      return;
    }
    const v = viewSize();
    state.zoom = Math.max(0.02, Math.min(64, Math.min((v.w - 16) / pic.w, (v.h - 16) / pic.h)));
    state.panX = (v.w - pic.w * state.zoom) / 2;
    state.panY = (v.h - pic.h * state.zoom) / 2;
    request();
  }

  // zoomAt changes the zoom keeping the point (sx, sy) of the view where it is.
  function zoomAt(z, sx, sy) {
    z = Math.max(0.02, Math.min(64, z));
    state.panX = sx - ((sx - state.panX) * z) / state.zoom;
    state.panY = sy - ((sy - state.panY) * z) / state.zoom;
    state.zoom = z;
    request();
  }

  function draw() {
    pending = false;
    const v = viewSize();
    const dpr = window.devicePixelRatio || 1;
    const W = Math.round(v.w * dpr);
    const H = Math.round(v.h * dpr);
    if (canvas.width !== W || canvas.height !== H) {
      canvas.width = W;
      canvas.height = H;
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, v.w, v.h);
    el('zoom').textContent = `${Math.round(state.zoom * 100)}%`;
    const pic = state.pic;
    const m = state.map;
    if (!pic || !m) {
      return;
    }
    const z = state.zoom;
    const cw = pic.cw * z;
    const ch = pic.ch * z;
    const x0 = state.panX;
    const y0 = state.panY;
    // A checkerboard under the map, so empty cells read as empty.
    ctx.fillStyle = '#6a6a6a';
    ctx.fillRect(x0, y0, pic.w * z, pic.h * z);
    ctx.fillStyle = '#7c7c7c';
    const sq = Math.max(cw / 2, 4);
    const c0 = Math.max(0, Math.floor(-x0 / sq));
    const r0 = Math.max(0, Math.floor(-y0 / sq));
    const c1 = Math.min(Math.ceil((pic.w * z) / sq), Math.ceil((v.w - x0) / sq));
    const r1 = Math.min(Math.ceil((pic.h * z) / sq), Math.ceil((v.h - y0) / sq));
    for (let r = r0; r < r1; r++) {
      for (let c = c0 + ((c0 + r) & 1); c < c1; c += 2) {
        ctx.fillRect(x0 + c * sq, y0 + r * sq, Math.min(sq, pic.w * z - c * sq), Math.min(sq, pic.h * z - r * sq));
      }
    }
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(off, x0, y0, pic.w * z, pic.h * z);
    if (state.grid && cw >= 5) {
      const gx0 = Math.max(0, Math.floor(-x0 / cw));
      const gy0 = Math.max(0, Math.floor(-y0 / ch));
      const gx1 = Math.min(m.w, Math.ceil((v.w - x0) / cw));
      const gy1 = Math.min(m.h, Math.ceil((v.h - y0) / ch));
      ctx.strokeStyle = 'rgba(0, 0, 0, 0.25)';
      ctx.lineWidth = 1;
      ctx.beginPath();
      for (let x = gx0; x <= gx1; x++) {
        const sx = Math.round(x0 + x * cw) + 0.5;
        ctx.moveTo(sx, y0 + gy0 * ch);
        ctx.lineTo(sx, y0 + gy1 * ch);
      }
      for (let y = gy0; y <= gy1; y++) {
        const sy = Math.round(y0 + y * ch) + 0.5;
        ctx.moveTo(x0 + gx0 * cw, sy);
        ctx.lineTo(x0 + gx1 * cw, sy);
      }
      ctx.stroke();
    }
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.5)';
    ctx.strokeRect(Math.round(x0) - 0.5, Math.round(y0) - 0.5, Math.round(pic.w * z) + 1, Math.round(pic.h * z) + 1);
    // Objects: rectangles with their names.
    ctx.font = '11px sans-serif';
    ctx.textBaseline = 'top';
    m.objects.forEach((o, i) => {
      const size = o.size || [1, 1];
      const sel = i === state.selected;
      const rx = x0 + o.at[0] * cw;
      const ry = y0 + o.at[1] * ch;
      ctx.fillStyle = sel ? 'rgba(255, 204, 0, 0.25)' : 'rgba(255, 204, 0, 0.08)';
      ctx.fillRect(rx, ry, size[0] * cw, size[1] * ch);
      ctx.strokeStyle = sel ? '#ffcc00' : 'rgba(255, 204, 0, 0.8)';
      ctx.lineWidth = sel ? 2 : 1;
      ctx.strokeRect(rx + 0.5, ry + 0.5, size[0] * cw - 1, size[1] * ch - 1);
      if (cw * size[0] >= 30) {
        ctx.fillStyle = '#000000';
        ctx.fillText(o.name, rx + 4, ry + 4);
        ctx.fillStyle = '#ffdd55';
        ctx.fillText(o.name, rx + 3, ry + 3);
      }
    });
    ctx.lineWidth = 1;
    // What the pointer would do.
    const d = state.drag;
    if (d && (d.kind === 'rect' || d.kind === 'object') && d.end) {
      const [ax, ay, bx, by] = [Math.min(d.start[0], d.end[0]), Math.min(d.start[1], d.end[1]), Math.max(d.start[0], d.end[0]), Math.max(d.start[1], d.end[1])];
      ctx.setLineDash([4, 3]);
      ctx.strokeStyle = d.kind === 'object' ? '#ffcc00' : '#ffffff';
      ctx.strokeRect(x0 + ax * cw + 0.5, y0 + ay * ch + 0.5, (bx - ax + 1) * cw - 1, (by - ay + 1) * ch - 1);
      ctx.setLineDash([]);
    } else if (state.hover && !d && !state.space) {
      const [hx, hy] = state.hover;
      let n = 1;
      let o = 0;
      if (state.tool === 'brush' || state.tool === 'eraser') {
        n = state.size;
        o = Math.floor((n - 1) / 2);
      }
      ctx.strokeStyle = state.tool === 'eraser' ? '#ff6060' : '#ffffff';
      ctx.strokeRect(x0 + (hx - o) * cw + 0.5, y0 + (hy - o) * ch + 0.5, n * cw - 1, n * ch - 1);
    }
  }

  // cellAt turns a pointer position into a cell (it may be off the map).
  function cellAt(e) {
    const r = canvas.getBoundingClientRect();
    const pic = state.pic;
    return [Math.floor((e.clientX - r.left - state.panX) / (pic.cw * state.zoom)), Math.floor((e.clientY - r.top - state.panY) / (pic.ch * state.zoom))];
  }

  // ---------------------------------------------------------------- painting

  function paintValue() {
    return state.tool === 'eraser' ? 0 : state.terrain + 1;
  }

  // line paints along the cells from a to b, so a quick stroke leaves no gaps.
  function strokeTo(a, b, changed) {
    let [x, y] = a;
    const [x1, y1] = b;
    const dx = Math.abs(x1 - x);
    const dy = -Math.abs(y1 - y);
    const sx = x < x1 ? 1 : -1;
    const sy = y < y1 ? 1 : -1;
    let err = dx + dy;
    for (;;) {
      tm.brush(state.map, state.layer, x, y, state.size, paintValue(), changed);
      if (x === x1 && y === y1) {
        break;
      }
      const e2 = 2 * err;
      if (e2 >= dy) {
        err += dy;
        x += sx;
      }
      if (e2 <= dx) {
        err += dx;
        y += sy;
      }
    }
  }

  // pick takes the terrain of a cell: on the layer painted on, else the topmost shown layer
  // that has one there, which becomes the layer painted on.
  function pick(cell) {
    const m = state.map;
    const layers = [state.layer, ...m.layers.map((l, i) => i).reverse()];
    for (const l of layers) {
      if (state.hidden.has(m.layers[l].name)) {
        continue;
      }
      const v = tm.get(m, l, cell[0], cell[1]);
      if (v > 0) {
        state.terrain = v - 1;
        state.layer = l;
        panels();
        return;
      }
    }
  }

  function objectAt(cell) {
    const m = state.map;
    for (let i = m.objects.length - 1; i >= 0; i--) {
      const o = m.objects[i];
      const s = o.size || [1, 1];
      if (cell[0] >= o.at[0] && cell[1] >= o.at[1] && cell[0] < o.at[0] + s[0] && cell[1] < o.at[1] + s[1]) {
        return i;
      }
    }
    return -1;
  }

  function cancelDrag() {
    const d = state.drag;
    state.drag = null;
    if (d && d.pointer !== undefined && canvas.hasPointerCapture && canvas.hasPointerCapture(d.pointer)) {
      canvas.releasePointerCapture(d.pointer);
    }
    request();
  }

  canvas.addEventListener('pointerdown', (e) => {
    if (!state.map || !state.pic || state.drag) {
      return;
    }
    canvas.setPointerCapture(e.pointerId);
    if (e.button === 1 || (e.button === 0 && state.space)) {
      state.drag = { kind: 'pan', pointer: e.pointerId, x: e.clientX, y: e.clientY, panX: state.panX, panY: state.panY };
      e.preventDefault();
      return;
    }
    if (e.button !== 0) {
      return;
    }
    const cell = cellAt(e);
    if (e.altKey || state.tool === 'pick') {
      pick(cell);
      state.drag = { kind: 'pick', pointer: e.pointerId };
      return;
    }
    if (state.hidden.has(state.map.layers[state.layer].name) && state.tool !== 'object') {
      note(`The layer ${state.map.layers[state.layer].name} is hidden: show it to paint on it.`);
      return;
    }
    switch (state.tool) {
      case 'brush':
      case 'eraser': {
        const changed = [];
        strokeTo(cell, cell, changed);
        redrawCells(changed);
        state.drag = { kind: 'stroke', pointer: e.pointerId, last: cell };
        break;
      }
      case 'rect':
        state.drag = { kind: 'rect', pointer: e.pointerId, start: cell, end: cell };
        break;
      case 'fill':
        redrawCells(tm.flood(state.map, state.layer, cell[0], cell[1], paintValue()));
        commit();
        break;
      case 'object':
        state.drag = { kind: 'object', pointer: e.pointerId, start: cell, end: null, x: e.clientX, y: e.clientY };
        break;
      default:
        break;
    }
    request();
  });

  canvas.addEventListener('pointermove', (e) => {
    if (!state.map || !state.pic) {
      return;
    }
    const d = state.drag;
    if (d && d.kind === 'pan') {
      state.panX = d.panX + e.clientX - d.x;
      state.panY = d.panY + e.clientY - d.y;
      request();
      return;
    }
    const cell = cellAt(e);
    const moved = !state.hover || state.hover[0] !== cell[0] || state.hover[1] !== cell[1];
    state.hover = cell;
    if (moved) {
      status(cell);
    }
    if (!d) {
      if (moved) {
        request();
      }
      return;
    }
    switch (d.kind) {
      case 'stroke': {
        if (moved) {
          const changed = [];
          strokeTo(d.last, cell, changed);
          d.last = cell;
          redrawCells(changed);
        }
        break;
      }
      case 'rect':
        d.end = cell;
        break;
      case 'object':
        if (d.end || Math.abs(e.clientX - d.x) + Math.abs(e.clientY - d.y) > 4) {
          d.end = cell;
        }
        break;
      case 'pick':
        if (moved) {
          pick(cell);
        }
        break;
      default:
        break;
    }
    request();
  });

  function endDrag(e) {
    const d = state.drag;
    if (!d || (e.pointerId !== undefined && d.pointer !== e.pointerId)) {
      return;
    }
    state.drag = null;
    if (canvas.hasPointerCapture(e.pointerId)) {
      canvas.releasePointerCapture(e.pointerId);
    }
    switch (d.kind) {
      case 'stroke':
        commit();
        break;
      case 'rect':
        redrawCells(tm.rect(state.map, state.layer, d.start[0], d.start[1], d.end[0], d.end[1], paintValue()));
        commit();
        break;
      case 'object':
        if (d.end) {
          makeObject(d.start, d.end);
        } else {
          state.selected = objectAt(d.start);
          objectForm();
        }
        break;
      default:
        break;
    }
    request();
  }
  canvas.addEventListener('pointerup', endDrag);
  canvas.addEventListener('pointercancel', (e) => {
    cancelDrag();
    endDrag(e);
  });
  canvas.addEventListener('pointerleave', () => {
    state.hover = null;
    el('pos').textContent = ' ';
    el('cellinfo').textContent = '';
    request();
  });

  canvas.addEventListener('wheel', (e) => {
    e.preventDefault();
    if (!state.pic) {
      return;
    }
    const r = canvas.getBoundingClientRect();
    zoomAt(state.zoom * Math.pow(1.0015, -e.deltaY * (e.deltaMode === 1 ? 30 : 1)), e.clientX - r.left, e.clientY - r.top);
  }, { passive: false });

  canvas.addEventListener('auxclick', (e) => e.preventDefault());

  // makeObject adds an object over the rectangle between two cells, called object_N.
  function makeObject(a, b) {
    const m = state.map;
    const x0 = Math.max(0, Math.min(a[0], b[0]));
    const y0 = Math.max(0, Math.min(a[1], b[1]));
    const x1 = Math.min(m.w - 1, Math.max(a[0], b[0]));
    const y1 = Math.min(m.h - 1, Math.max(a[1], b[1]));
    if (x1 < x0 || y1 < y0) {
      return;
    }
    let n = 1;
    while (m.objects.some((o) => o.name === `object_${n}`)) {
      n++;
    }
    const o = { name: `object_${n}`, at: [x0, y0], size: undefined, tags: undefined, props: undefined };
    if (x1 > x0 || y1 > y0) {
      o.size = [x1 - x0 + 1, y1 - y0 + 1];
    }
    m.objects.push(o);
    m.hasObjects = true;
    state.selected = m.objects.length - 1;
    commit();
    objectForm();
  }

  // ---------------------------------------------------------------- the status line

  function status(cell) {
    const m = state.map;
    if (!m || !tm.inside(m, cell[0], cell[1])) {
      el('pos').textContent = ' ';
      el('cellinfo').textContent = '';
      return;
    }
    el('pos').textContent = `${cell[0]}, ${cell[1]}`;
    const parts = [];
    for (let l = m.layers.length - 1; l >= 0; l--) {
      const v = tm.get(m, l, cell[0], cell[1]);
      const t = v > 0 ? m.terrains[v - 1] : null;
      parts.push(`${m.layers[l].name}: ${t ? t.name + (t.tags && t.tags.length ? ' [' + t.tags.join(', ') + ']' : '') : '—'}`);
    }
    const o = objectAt(cell);
    if (o >= 0) {
      parts.push(`object ${m.objects[o].name}`);
    }
    el('cellinfo').textContent = parts.join('   ');
  }

  let noteTimer = null;
  function note(text) {
    el('cellinfo').textContent = text;
    clearTimeout(noteTimer);
    noteTimer = setTimeout(() => {
      if (state.hover) {
        status(state.hover);
      }
    }, 2500);
  }

  // ---------------------------------------------------------------- the side panels

  function button(label, title, onClick) {
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = label;
    b.title = title;
    b.addEventListener('click', (e) => {
      e.stopPropagation();
      onClick();
    });
    return b;
  }

  function span(cls, text) {
    const s = document.createElement('span');
    s.className = cls;
    s.textContent = text;
    return s;
  }

  // thumbnail draws frame 0 of a terrain's texture (or its stripes when it has none).
  function thumbnail(i) {
    const m = state.map;
    const c = document.createElement('canvas');
    c.width = 24;
    c.height = 24;
    const g = c.getContext('2d');
    const t = m.terrains[i];
    const name = t.material ? state.lib.materials[t.material] : t.texture;
    const tx = name ? state.lib.textures[name] : null;
    if (tx && tx.image) {
      const fw = Math.floor(tx.image.w / Math.max(tx.grid[0], 1));
      const fh = Math.floor(tx.image.h / Math.max(tx.grid[1], 1));
      const one = document.createElement('canvas');
      one.width = fw;
      one.height = fh;
      const data = new Uint8ClampedArray(fw * fh * 4);
      for (let y = 0; y < fh; y++) {
        data.set(tx.image.data.subarray(y * tx.image.w * 4, (y * tx.image.w + fw) * 4), y * fw * 4);
      }
      one.getContext('2d').putImageData(new ImageData(data, fw, fh), 0, 0);
      g.imageSmoothingEnabled = false;
      g.drawImage(one, 0, 0, 24, 24);
    } else {
      const [r, gg, b] = colorOf(t.name);
      for (let y = 0; y < 24; y += 4) {
        for (let x = 0; x < 24; x += 4) {
          g.fillStyle = ((x + y) >> 2) & 1 ? `rgb(${r},${gg},${b})` : `rgb(${r >> 1},${gg >> 1},${b >> 1})`;
          g.fillRect(x, y, 4, 4);
        }
      }
    }
    return c;
  }

  // why says why a terrain is drawn without its texture, or ''.
  function why(t) {
    if (t.material) {
      if (!Object.prototype.hasOwnProperty.call(state.lib.materials, t.material)) {
        return `no material ${t.material} in the project`;
      }
      const name = state.lib.materials[t.material];
      if (!name) {
        return `material ${t.material} has no texture`;
      }
      return state.textureErrors[name] || (state.lib.textures[name] ? '' : `no texture ${name} in the project`);
    }
    return state.textureErrors[t.texture] || (state.lib.textures[t.texture] ? '' : `no texture ${t.texture} in the project`);
  }

  function palette() {
    const box = el('palette');
    box.textContent = '';
    const m = state.map;
    m.terrains.forEach((t, i) => {
      const row = document.createElement('div');
      row.className = 'item' + (i === state.terrain ? ' selected' : '');
      row.title = `${t.name}: ${t.material ? 'material ' + t.material : 'texture ' + t.texture}${i < 9 ? ` (${i + 1})` : ''}`;
      row.appendChild(thumbnail(i));
      row.appendChild(span('key', t.key));
      const col = document.createElement('div');
      col.className = 'name';
      col.appendChild(span('name', t.name));
      const missing = why(t);
      if (missing) {
        col.appendChild(document.createElement('br'));
        col.appendChild(span('note', missing));
        row.title += '\n' + missing;
      } else if (t.tags && t.tags.length > 0) {
        col.appendChild(document.createElement('br'));
        col.appendChild(span('tags', t.tags.join(', ')));
      }
      row.appendChild(col);
      row.addEventListener('click', () => {
        state.terrain = i;
        if (state.tool === 'eraser' || state.tool === 'pick' || state.tool === 'object') {
          setTool('brush');
        }
        panels();
      });
      box.appendChild(row);
    });
    terrainForm();
  }

  function texturePicker() {
    const sel = el('texturepick');
    const was = sel.value;
    sel.textContent = '';
    for (const name of state.textureNames) {
      const o = document.createElement('option');
      o.value = name;
      o.textContent = name;
      sel.appendChild(o);
    }
    if (state.textureNames.includes(was)) {
      sel.value = was;
    }
    el('addterrain').disabled = state.textureNames.length === 0;
    sel.disabled = state.textureNames.length === 0;
    sel.title = state.textureNames.length === 0 ? 'The project has no texture yet' : 'A texture of the project';
  }

  const splitList = (s) => s.split(/[\s,]+/).filter(Boolean);

  function terrainForm() {
    const m = state.map;
    const t = m.terrains[state.terrain];
    el('terrainform').hidden = !t;
    if (!t) {
      return;
    }
    el('t-name').value = t.name;
    el('t-key').value = t.key;
    el('t-tags').value = (t.tags || []).join(', ');
    el('t-error').textContent = '';
  }

  el('terrainform').addEventListener('submit', (e) => {
    e.preventDefault();
    const m = state.map;
    const i = state.terrain;
    const name = el('t-name').value.trim();
    const key = el('t-key').value;
    const tags = splitList(el('t-tags').value);
    const errs = [];
    const bad = tex.nameError(name);
    if (bad) {
      errs.push(bad);
    } else if (m.terrains.some((t, k) => k !== i && t.name === name)) {
      errs.push(`the terrain name ${name} is taken`);
    }
    const code = key.length === 1 ? key.charCodeAt(0) : 0;
    if (!(code > 0x20 && code < 0x7f && key !== '"' && key !== '\\')) {
      errs.push('the key is one printable ASCII character but space, " and \\');
    } else if (m.terrains.some((t, k) => k !== i && t.key === key)) {
      errs.push(`the key ${key} is taken by ${m.terrains.find((t) => t.key === key).name}`);
    }
    tags.forEach((g, k) => {
      const b = tex.nameError(g);
      if (b) {
        errs.push('tag: ' + b);
      } else if (tags.indexOf(g) !== k) {
        errs.push(`tag ${g} twice`);
      }
    });
    if (errs.length > 0) {
      el('t-error').textContent = errs.join('\n');
      return;
    }
    const t = m.terrains[i];
    t.name = name;
    t.key = key;
    t.tags = tags.length > 0 ? tags : undefined;
    commit();
    panels();
  });

  el('t-delete').addEventListener('click', () => {
    const m = state.map;
    const i = state.terrain;
    if (m.terrains.length <= 1) {
      el('t-error').textContent = 'a map has at least one terrain';
      return;
    }
    const n = tm.uses(m, i);
    const go = () => {
      tm.removeTerrain(state.map, i);
      state.terrain = Math.max(0, i - 1);
      commit();
      rebuild();
      panels();
    };
    if (n === 0) {
      go();
      return;
    }
    confirm(`The terrain ${m.terrains[i].name} paints ${n} cell${n === 1 ? '' : 's'}. Delete it and empty them?`, 'Delete', go);
  });

  el('addterrain').addEventListener('click', () => {
    const name = el('texturepick').value;
    if (!name || !state.map) {
      return;
    }
    const i = tm.addTerrain(state.map, name);
    if (i < 0) {
      note(`A map has at most ${tm.MaxMapTerrains} terrains.`);
      return;
    }
    state.terrain = i;
    commit();
    rebuild();
    panels();
  });

  function layersPanel() {
    const box = el('layers');
    box.textContent = '';
    const m = state.map;
    for (let i = m.layers.length - 1; i >= 0; i--) {
      const l = m.layers[i];
      const hidden = state.hidden.has(l.name);
      const row = document.createElement('div');
      row.className = 'item' + (i === state.layer ? ' selected' : '') + (hidden ? ' hidden' : '');
      row.title = `Paint on ${l.name}`;
      const eye = button(hidden ? 'show' : 'hide', hidden ? 'Show this layer (the file does not change)' : 'Hide this layer in the editor (the file does not change)', () => {
        if (hidden) {
          state.hidden.delete(l.name);
        } else {
          state.hidden.add(l.name);
        }
        rebuild();
        panels();
      });
      row.appendChild(eye);
      row.appendChild(span('name', l.name));
      const up = button('▲', 'Move up: drawn over the layer above', () => moveLayer(i, i + 1));
      up.disabled = i === m.layers.length - 1;
      const down = button('▼', 'Move down', () => moveLayer(i, i - 1));
      down.disabled = i === 0;
      row.appendChild(up);
      row.appendChild(down);
      const del = button('✕', 'Delete this layer', () => {
        if (m.layers.length <= 1) {
          note('A map has at least one layer.');
          return;
        }
        confirm(`Delete the layer ${l.name} and its cells?`, 'Delete', () => {
          state.map.layers.splice(i, 1);
          state.layer = Math.min(state.layer, state.map.layers.length - 1);
          commit();
          rebuild();
          panels();
        });
      });
      row.appendChild(del);
      row.addEventListener('click', () => {
        state.layer = i;
        panels();
      });
      box.appendChild(row);
    }
    const l = m.layers[state.layer];
    el('l-name').value = l.name;
    el('l-z').value = l.z === undefined ? '' : String(l.z);
    el('l-layer').value = l.layer === undefined ? '' : String(l.layer);
    el('l-error').textContent = '';
    el('addlayer').disabled = m.layers.length >= tm.MaxMapLayers;
  }

  function moveLayer(from, to) {
    const m = state.map;
    const [l] = m.layers.splice(from, 1);
    m.layers.splice(to, 0, l);
    if (state.layer === from) {
      state.layer = to;
    } else if (state.layer === to) {
      state.layer = from;
    }
    commit();
    rebuild();
    panels();
  }

  el('addlayer').addEventListener('click', () => {
    const m = state.map;
    if (m.layers.length >= tm.MaxMapLayers) {
      return;
    }
    const name = tm.freeName('layer', m.layers.map((l) => l.name));
    m.layers.push({ name, z: undefined, layer: undefined, cells: new Uint8Array(m.w * m.h) });
    state.layer = m.layers.length - 1;
    commit();
    rebuild();
    panels();
  });

  // number reads a form field: undefined when empty, NaN when not a number.
  function number(id) {
    const s = String(el(id).value).trim();
    if (s === '') {
      return undefined;
    }
    return /^[-+]?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?$/.test(s) ? Number(s) : NaN;
  }

  el('layerform').addEventListener('submit', (e) => {
    e.preventDefault();
    const m = state.map;
    const i = state.layer;
    const name = el('l-name').value.trim();
    const z = number('l-z');
    const order = number('l-layer');
    const errs = [];
    const bad = tex.nameError(name);
    if (bad) {
      errs.push(bad);
    } else if (m.layers.some((l, k) => k !== i && l.name === name)) {
      errs.push(`the layer name ${name} is taken`);
    }
    if (z !== undefined && !(Math.abs(z) <= 1e6)) {
      errs.push('z is a number from -1000000 to 1000000, or empty for 0.1 × its place');
    }
    if (order !== undefined && !(Number.isInteger(order) && order >= tm.MinLayer && order <= tm.MaxLayer)) {
      errs.push(`the draw order is a whole number from ${tm.MinLayer} to ${tm.MaxLayer}`);
    }
    if (errs.length > 0) {
      el('l-error').textContent = errs.join('\n');
      return;
    }
    const l = m.layers[i];
    if (state.hidden.delete(l.name)) {
      state.hidden.add(name);
    }
    l.name = name;
    l.z = z;
    l.layer = order;
    commit();
    rebuild();
    panels();
  });

  // props lines are "key = value": true and false are booleans, what reads as a number is
  // one, anything else a string (quotes keep a string that would read otherwise).
  function writeProp(v) {
    if (typeof v !== 'string') {
      return String(v);
    }
    return v === 'true' || v === 'false' || v !== v.trim() || v.startsWith('"') || v === '' || !isNaN(Number(v)) ? JSON.stringify(v) : v;
  }

  function readProps(text) {
    const out = [];
    const errs = [];
    text.split('\n').forEach((line, n) => {
      if (line.trim() === '') {
        return;
      }
      const i = line.indexOf('=');
      if (i < 0) {
        errs.push(`line ${n + 1}: key = value`);
        return;
      }
      const k = line.slice(0, i).trim();
      const s = line.slice(i + 1).trim();
      let v;
      if (s === 'true' || s === 'false') {
        v = s === 'true';
      } else if (s.startsWith('"')) {
        try {
          v = JSON.parse(s);
        } catch (_) {
          errs.push(`line ${n + 1}: a string in quotes is JSON`);
          return;
        }
      } else if (s !== '' && /^[-+]?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?$/.test(s) && isFinite(Number(s))) {
        v = Number(s);
      } else {
        v = s;
      }
      if (k === '' || tex.utf8Length(k) > 64 || /[. \t]/.test(k)) {
        errs.push(`line ${n + 1}: a property name is 1-64 characters without spaces or '.'`);
      } else if (out.some((p) => p[0] === k)) {
        errs.push(`line ${n + 1}: ${k} twice`);
      } else {
        out.push([k, v]);
      }
    });
    return { props: out, errs };
  }

  function objectForm() {
    const m = state.map;
    const o = m && m.objects[state.selected];
    el('objectform').hidden = !o;
    el('noobject').hidden = !!o;
    if (!o) {
      request();
      return;
    }
    const s = o.size || [1, 1];
    el('o-name').value = o.name;
    el('o-x').value = String(o.at[0]);
    el('o-y').value = String(o.at[1]);
    el('o-w').value = String(s[0]);
    el('o-h').value = String(s[1]);
    el('o-tags').value = (o.tags || []).join(', ');
    el('o-props').value = (o.props || []).map(([k, v]) => `${k} = ${writeProp(v)}`).join('\n');
    el('o-error').textContent = '';
    request();
  }

  el('objectform').addEventListener('submit', (e) => {
    e.preventDefault();
    const m = state.map;
    const i = state.selected;
    const o = m.objects[i];
    if (!o) {
      return;
    }
    const name = el('o-name').value.trim();
    const [x, y, w, h] = ['o-x', 'o-y', 'o-w', 'o-h'].map(number);
    const tags = splitList(el('o-tags').value);
    const { props, errs } = readProps(el('o-props').value);
    const bad = tex.nameError(name);
    if (bad) {
      errs.unshift(bad);
    } else if (m.objects.some((p, k) => k !== i && p.name === name)) {
      errs.unshift(`the object name ${name} is taken`);
    }
    if (![x, y].every(Number.isInteger) || x < 0 || y < 0 || x >= m.w || y >= m.h) {
      errs.push(`at is a cell of the ${m.w} × ${m.h} map`);
    } else if (![w, h].every(Number.isInteger) || w < 1 || h < 1 || x + w > m.w || y + h > m.h) {
      errs.push('the size is at least 1 × 1 and stays on the map');
    }
    tags.forEach((g, k) => {
      const b = tex.nameError(g);
      if (b) {
        errs.push('tag: ' + b);
      } else if (tags.indexOf(g) !== k) {
        errs.push(`tag ${g} twice`);
      }
    });
    if (errs.length > 0) {
      el('o-error').textContent = errs.join('\n');
      return;
    }
    o.name = name;
    o.at = [x, y];
    o.size = w === 1 && h === 1 && !o.size ? undefined : [w, h];
    o.tags = tags.length > 0 ? tags : undefined;
    o.props = props.length > 0 ? props : undefined;
    commit();
    objectForm();
  });

  el('o-delete').addEventListener('click', () => {
    deleteObject();
  });

  function deleteObject() {
    const m = state.map;
    if (!m || !m.objects[state.selected]) {
      return;
    }
    m.objects.splice(state.selected, 1);
    state.selected = -1;
    commit();
    objectForm();
  }

  function mapForm() {
    const m = state.map;
    el('m-w').value = String(m.w);
    el('m-h').value = String(m.h);
    el('m-tile').value = m.tile === undefined ? '' : String(m.tile);
    ['m-ox', 'm-oy', 'm-oz'].forEach((id, i) => {
      el(id).value = m.origin === undefined ? '' : String(m.origin[i]);
    });
    el('m-error').textContent = '';
  }

  el('mapform').addEventListener('submit', (e) => {
    e.preventDefault();
    const m = state.map;
    const [w, h, tile] = ['m-w', 'm-h', 'm-tile'].map(number);
    const origin = ['m-ox', 'm-oy', 'm-oz'].map(number);
    const errs = [];
    if (![w, h].every((v) => Number.isInteger(v) && v >= 1 && v <= tm.MaxMapSize)) {
      errs.push(`the size is 1 to ${tm.MaxMapSize} columns and rows`);
    }
    if (tile !== undefined && !(tile > 0 && isFinite(Math.fround(tile)))) {
      errs.push('the tile is a number above 0, or empty for 1');
    }
    const none = origin.every((v) => v === undefined);
    if (!none && !origin.every((v) => v !== undefined && isFinite(Math.fround(v)))) {
      errs.push('the origin is 3 numbers, or empty for 0, 0, 0');
    }
    if (errs.length > 0) {
      el('m-error').textContent = errs.join('\n');
      return;
    }
    m.tile = tile;
    m.origin = none ? undefined : origin;
    const resized = w !== m.w || h !== m.h;
    if (resized) {
      const dropped = tm.resize(m, w, h);
      if (dropped.length > 0) {
        note(`Off the map now: ${dropped.join(', ')}.`);
      }
      state.selected = -1;
    }
    commit();
    if (resized) {
      rebuild();
      fit();
    }
    panels();
  });

  function panels() {
    if (!state.map) {
      return;
    }
    palette();
    layersPanel();
    objectForm();
    mapForm();
    for (const b of document.querySelectorAll('#tools button')) {
      b.setAttribute('aria-pressed', String(b.dataset.tool === state.tool));
    }
    for (const b of document.querySelectorAll('#sizes button')) {
      b.setAttribute('aria-pressed', String(Number(b.dataset.size) === state.size));
    }
    el('animate').setAttribute('aria-pressed', String(state.animate));
    el('grid').checked = state.grid;
  }

  // confirm asks through VS Code (a webview has no dialogs of its own), then runs go.
  let confirmId = 0;
  function confirm(message, action, go) {
    const id = ++confirmId;
    state.confirms.set(id, go);
    vscode.postMessage({ type: 'confirm', id, message, action });
  }

  // ---------------------------------------------------------------- tools and keys

  function setTool(tool) {
    state.tool = tool;
    save();
    panels();
    request();
  }

  function setSize(size) {
    state.size = size;
    save();
    panels();
    request();
  }

  for (const b of document.querySelectorAll('#tools button')) {
    b.addEventListener('click', () => setTool(b.dataset.tool));
  }
  for (const b of document.querySelectorAll('#sizes button')) {
    b.addEventListener('click', () => setSize(Number(b.dataset.size)));
  }

  function startAnimation() {
    clearInterval(state.timer);
    state.timer = setInterval(() => {
      state.tick++;
      step();
    }, 1000 / state.tickRate);
  }

  // step shows the frames of the current tick, drawing again the cells they change.
  function step() {
    const pic = state.pic;
    if (!pic) {
      return;
    }
    const changed = pic.setTick(state.animate ? state.tick : 0, state.tickRate);
    if (changed.length === 0) {
      return;
    }
    const m = pic.m;
    const cells = [];
    for (let y = 0; y < m.h; y++) {
      for (let x = 0; x < m.w; x++) {
        if (pic.touches(x, y, changed)) {
          cells.push([x, y]);
        }
      }
    }
    const C = 16;
    const chunks = new Set();
    for (const [x, y] of cells) {
      pic.drawCell(x, y, true);
      chunks.add(Math.floor(y / C) * 4096 + Math.floor(x / C));
    }
    for (const k of chunks) {
      const cx = k % 4096;
      const cy = Math.floor(k / 4096);
      offCtx.putImageData(state.image, 0, 0, cx * C * pic.cw, cy * C * pic.ch, Math.min(C, m.w - cx * C) * pic.cw, Math.min(C, m.h - cy * C) * pic.ch);
    }
    request();
  }

  el('animate').addEventListener('click', () => {
    state.animate = !state.animate;
    if (state.animate) {
      startAnimation();
    } else {
      clearInterval(state.timer);
      state.tick = 0;
      step();
    }
    panels();
  });

  el('grid').addEventListener('change', (e) => {
    state.grid = e.target.checked;
    save();
    request();
  });

  const center = () => {
    const v = viewSize();
    return [v.w / 2, v.h / 2];
  };
  el('zoomin').addEventListener('click', () => zoomAt(state.zoom * 1.5, ...center()));
  el('zoomout').addEventListener('click', () => zoomAt(state.zoom / 1.5, ...center()));
  el('fit').addEventListener('click', fit);
  el('openjson').addEventListener('click', () => vscode.postMessage({ type: 'openJson' }));

  const typing = (e) => {
    const t = e.target;
    return t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT');
  };

  window.addEventListener('keydown', (e) => {
    // Ctrl and Cmd are VS Code's: undo, redo, save and the rest go to the document.
    if (e.ctrlKey || e.metaKey || typing(e)) {
      return;
    }
    const k = e.key.toLowerCase();
    if (e.key === ' ') {
      state.space = true;
      el('view').classList.add('panning');
      e.preventDefault();
      return;
    }
    if (!state.map) {
      return;
    }
    if (TOOLS[k] && !e.altKey) {
      setTool(TOOLS[k]);
    } else if (e.key === '[' || e.key === ']') {
      const i = SIZES.indexOf(state.size) + (e.key === ']' ? 1 : -1);
      setSize(SIZES[Math.max(0, Math.min(SIZES.length - 1, i))]);
    } else if (/^[1-9]$/.test(e.key) && Number(e.key) <= state.map.terrains.length) {
      state.terrain = Number(e.key) - 1;
      if (state.tool === 'eraser' || state.tool === 'pick' || state.tool === 'object') {
        state.tool = 'brush';
      }
      panels();
    } else if (k === 'h') {
      state.grid = !state.grid;
      save();
      panels();
      request();
    } else if (e.key === 'Escape') {
      cancelDrag();
      state.selected = -1;
      objectForm();
    } else if ((e.key === 'Delete' || e.key === 'Backspace') && state.selected >= 0) {
      deleteObject();
    } else {
      return;
    }
    e.preventDefault();
  });

  window.addEventListener('keyup', (e) => {
    if (e.key === ' ') {
      state.space = false;
      el('view').classList.remove('panning');
    }
  });

  window.addEventListener('blur', () => {
    state.space = false;
    el('view').classList.remove('panning');
  });

  window.addEventListener('resize', request);

  window.addEventListener('message', (e) => {
    const m = e.data;
    if (!m) {
      return;
    }
    switch (m.type) {
      case 'document':
        documentText(m);
        break;
      case 'assets':
        assets(m);
        break;
      case 'confirmed': {
        const go = state.confirms.get(m.id);
        state.confirms.delete(m.id);
        if (go && m.ok && state.map) {
          go();
        }
        break;
      }
      default:
        break;
    }
  });

  panels();
  vscode.postMessage({ type: 'ready' });
})();
