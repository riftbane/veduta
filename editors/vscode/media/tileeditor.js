'use strict';
// The tile editor: draws the frames of a pixel texture (a .vtex whose one layer is a PNG),
// plain, animated or an autotile. The pixels live here; every gesture is one edit the
// extension reports to VS Code, whose undo and redo call back here, and saving asks for the
// frames, which the extension writes as the PNG and the .vtex.
(function () {
  const vscode = acquireVsCodeApi();
  const ts = globalThis.vedutaTileset;

  const TOOLS = { b: 'pencil', e: 'eraser', l: 'line', r: 'rect', g: 'fill', i: 'pick', m: 'select' };
  const PALETTE = ['#000000', '#1d2b53', '#7e2553', '#008751', '#ab5236', '#5f574f', '#c2c3c7', '#fff1e8',
    '#ff004d', '#ffa300', '#ffec27', '#00e436', '#29adff', '#83769c', '#ff77a8', '#ffccaa', '#00000000'];
  const TRANSPARENT = [0, 0, 0, 0];

  const el = (id) => document.getElementById(id);
  const canvas = el('canvas');
  const ctx = canvas.getContext('2d');
  const off = document.createElement('canvas'); // the frame being drawn, a pixel a pixel
  const offCtx = off.getContext('2d');
  const onion = document.createElement('canvas'); // the frame before it
  const onionCtx = onion.getContext('2d');
  const floatCanvas = document.createElement('canvas'); // what the selection moves
  const floatCtx = floatCanvas.getContext('2d');

  const saved = vscode.getState() || {};
  const state = {
    model: null, // {w, h, frames, autotile, fps}
    tileSize: 0, // an autotile's tiles
    frame: 0,
    tool: saved.tool || 'pencil',
    colors: [saved.primary || [0, 0, 0, 255], saved.secondary || TRANSPARENT.slice()],
    grid: saved.grid !== false,
    onion: !!saved.onion,
    zoom: 0,
    panX: 0,
    panY: 0,
    hover: null,
    drag: null,
    space: false,
    last: null, // the pencil's last point, for Shift+click
    selection: null, // {x, y, w, h}
    floating: null, // {block, x, y, before}: pixels lifted or pasted, not placed yet
    clipboard: null,
    undo: [],
    redo: [],
    playing: false,
    playFrame: 0,
    timer: null,
    pscale: saved.pscale || 2,
  };

  function remember() {
    vscode.setState({ tool: state.tool, primary: state.colors[0], secondary: state.colors[1], grid: state.grid, onion: state.onion, pscale: state.pscale });
  }

  // ---------------------------------------------------------------- base64

  function toBase64(bytes) {
    let s = '';
    for (let i = 0; i < bytes.length; i += 0x8000) {
      s += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
    }
    return btoa(s);
  }

  function fromBase64(s) {
    const bin = atob(s);
    const out = new Uint8ClampedArray(bin.length);
    for (let i = 0; i < bin.length; i++) {
      out[i] = bin.charCodeAt(i);
    }
    return out;
  }

  // ---------------------------------------------------------------- the document

  function load(m) {
    stopPlaying();
    state.undo = [];
    state.redo = [];
    state.selection = null;
    state.floating = null;
    state.drag = null;
    if (m.error) {
      state.model = null;
      el('errortext').textContent = m.error;
      el('error').hidden = false;
      document.body.classList.add('broken');
      request();
      return;
    }
    el('error').hidden = true;
    document.body.classList.remove('broken');
    const first = !state.model || state.model.w !== m.w || state.model.h !== m.h;
    state.model = { w: m.w, h: m.h, frames: m.frames.map(fromBase64), autotile: m.autotile, fps: m.fps };
    state.tileSize = m.autotile ? m.w / 6 : 0;
    state.frame = Math.min(state.frame, state.model.frames.length - 1);
    off.width = onion.width = m.w;
    off.height = onion.height = m.h;
    el('info').textContent = m.info || '';
    el('autosection').hidden = !m.autotile;
    frameChanged();
    panels();
    if (first) {
      fit();
    }
  }

  // data is what saving writes: what moves is written where it is, and keeps moving.
  function data() {
    const m = state.model;
    const f = state.floating;
    const frames = m.frames.map((px, i) => {
      if (!f || f.frame !== i) {
        return toBase64(px);
      }
      const copy = px.slice();
      ts.pasteRect(copy, m.w, m.h, f.block, f.x, f.y, false);
      return toBase64(copy);
    });
    return { w: m.w, h: m.h, autotile: m.autotile, fps: m.fps, frames };
  }

  // push records a change made (and already applied): undo and redo put the model back.
  function push(label, undo, redo) {
    state.undo.push({ undo, redo });
    state.redo = [];
    vscode.postMessage({ type: 'edit', label });
  }

  // dropFloating puts back what was lifted and not placed: undo and redo start from the
  // frames as the last edit left them.
  function dropFloating() {
    const f = state.floating;
    if (f) {
      state.model.frames[f.frame].set(f.before);
      state.floating = null;
    }
  }

  function undo() {
    const e = state.undo.pop();
    if (e) {
      dropFloating();
      e.undo();
      state.redo.push(e);
      afterHistory();
    }
  }

  function redo() {
    const e = state.redo.pop();
    if (e) {
      dropFloating();
      e.redo();
      state.undo.push(e);
      afterHistory();
    }
  }

  function afterHistory() {
    const n = state.model.frames.length;
    state.frame = Math.max(0, Math.min(state.frame, n - 1));
    frameChanged();
    panels();
  }

  // pixelEdit records the change of frame f from before (a copy) to what it is now.
  function pixelEdit(label, f, before) {
    const px = state.model.frames[f];
    const after = px.slice();
    if (after.every((v, i) => v === before[i])) {
      return;
    }
    push(label, () => {
      px.set(before);
      state.frame = f;
    }, () => {
      px.set(after);
      state.frame = f;
    });
    frameChanged();
  }

  // framesEdit replaces the list of frames, and records it.
  function framesEdit(label, frames, select) {
    const m = state.model;
    const before = m.frames;
    const beforeSel = state.frame;
    m.frames = frames;
    state.frame = select;
    push(label, () => {
      m.frames = before;
      state.frame = beforeSel;
    }, () => {
      m.frames = frames;
      state.frame = select;
    });
    frameChanged();
    panels();
  }

  // ---------------------------------------------------------------- drawing

  let pending = false;
  function request() {
    if (!pending) {
      pending = true;
      requestAnimationFrame(draw);
    }
  }

  function current() {
    return state.model.frames[state.frame];
  }

  // frameChanged puts the frame (and the one before it) on the offscreen canvases and
  // redraws what shows it; light, while a stroke goes on, redraws only its thumbnail.
  function frameChanged(light) {
    const m = state.model;
    if (!m) {
      return;
    }
    offCtx.putImageData(new ImageData(new Uint8ClampedArray(current()), m.w, m.h), 0, 0);
    onionCtx.clearRect(0, 0, m.w, m.h);
    if (state.frame > 0) {
      onionCtx.putImageData(new ImageData(new Uint8ClampedArray(m.frames[state.frame - 1]), m.w, m.h), 0, 0);
    }
    request();
    if (light && el('strip').children.length === m.frames.length) {
      thumb(el('strip').children[state.frame].firstChild, current());
    } else {
      strip();
      used();
    }
    preview();
  }

  function thumb(c, px) {
    const m = state.model;
    c.width = m.w;
    c.height = m.h;
    c.getContext('2d').putImageData(new ImageData(new Uint8ClampedArray(px), m.w, m.h), 0, 0);
  }

  function viewSize() {
    const r = el('view').getBoundingClientRect();
    return { w: Math.max(1, r.width), h: Math.max(1, r.height) };
  }

  function fit() {
    const m = state.model;
    if (!m) {
      return;
    }
    const v = viewSize();
    const z = Math.min((v.w - 24) / m.w, (v.h - 24) / m.h);
    state.zoom = Math.max(1, z >= 1 ? Math.floor(z) : z);
    state.panX = Math.round((v.w - m.w * state.zoom) / 2);
    state.panY = Math.round((v.h - m.h * state.zoom) / 2);
    request();
  }

  function zoomAt(z, sx, sy) {
    z = Math.max(1, Math.min(96, Math.round(z)));
    state.panX = Math.round(sx - ((sx - state.panX) * z) / state.zoom);
    state.panY = Math.round(sy - ((sy - state.panY) * z) / state.zoom);
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
    const m = state.model;
    if (!m) {
      return;
    }
    const z = state.zoom;
    const x0 = state.panX;
    const y0 = state.panY;
    const iw = m.w * z;
    const ih = m.h * z;
    // A checkerboard under the image, so transparent pixels read as such.
    ctx.fillStyle = '#6a6a6a';
    ctx.fillRect(x0, y0, iw, ih);
    ctx.fillStyle = '#7c7c7c';
    const sq = Math.max(z / 2, 4);
    for (let r = Math.max(0, Math.floor(-y0 / sq)); r < Math.min(Math.ceil(ih / sq), Math.ceil((v.h - y0) / sq)); r++) {
      for (let c = Math.max(0, Math.floor(-x0 / sq)); c < Math.min(Math.ceil(iw / sq), Math.ceil((v.w - x0) / sq)); c++) {
        if ((r + c) & 1) {
          ctx.fillRect(x0 + c * sq, y0 + r * sq, Math.min(sq, iw - c * sq), Math.min(sq, ih - r * sq));
        }
      }
    }
    ctx.imageSmoothingEnabled = false;
    if (state.onion && state.frame > 0) {
      ctx.globalAlpha = 0.3;
      ctx.drawImage(onion, x0, y0, iw, ih);
      ctx.globalAlpha = 1;
    }
    ctx.drawImage(off, x0, y0, iw, ih);
    const f = state.floating;
    if (f) {
      floatCanvas.width = f.block.w;
      floatCanvas.height = f.block.h;
      floatCtx.putImageData(new ImageData(new Uint8ClampedArray(f.block.data), f.block.w, f.block.h), 0, 0);
      ctx.drawImage(floatCanvas, x0 + f.x * z, y0 + f.y * z, f.block.w * z, f.block.h * z);
    }
    // What a line or a rectangle would draw.
    const d = state.drag;
    if (d && (d.tool === 'line' || d.tool === 'rect') && d.end) {
      const c = state.colors[d.button];
      const pts = d.tool === 'line' ? ts.linePoints(d.start[0], d.start[1], d.end[0], d.end[1])
        : ts.rectPoints(d.start[0], d.start[1], d.end[0], d.end[1], d.filled);
      ctx.fillStyle = c[3] === 0 ? 'rgba(255, 96, 96, 0.5)' : `rgba(${c[0]}, ${c[1]}, ${c[2]}, ${c[3] / 255})`;
      for (const [px, py] of pts) {
        if (px >= 0 && py >= 0 && px < m.w && py < m.h) {
          ctx.fillRect(x0 + px * z, y0 + py * z, z, z);
        }
      }
    }
    if (state.grid && z >= 6) {
      line(0.18, 1, x0, y0, z);
    }
    if (state.tileSize > 0) {
      line(0.7, state.tileSize, x0, y0, z);
      // The lake's middle is not used.
      const t = state.tileSize * z;
      ctx.fillStyle = 'rgba(0, 0, 0, 0.35)';
      ctx.fillRect(x0 + 4 * t, y0 + t, t, t);
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.35)';
      ctx.beginPath();
      ctx.moveTo(x0 + 4 * t, y0 + t);
      ctx.lineTo(x0 + 5 * t, y0 + 2 * t);
      ctx.moveTo(x0 + 5 * t, y0 + t);
      ctx.lineTo(x0 + 4 * t, y0 + 2 * t);
      ctx.stroke();
      ctx.strokeStyle = 'rgba(255, 204, 0, 0.9)';
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(Math.round(x0 + 3 * t), y0);
      ctx.lineTo(Math.round(x0 + 3 * t), y0 + ih);
      ctx.stroke();
      ctx.lineWidth = 1;
    }
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.5)';
    ctx.strokeRect(Math.round(x0) - 0.5, Math.round(y0) - 0.5, Math.round(iw) + 1, Math.round(ih) + 1);
    const s = f ? { x: f.x, y: f.y, w: f.block.w, h: f.block.h } : state.selection;
    if (s) {
      ctx.setLineDash([4, 3]);
      ctx.strokeStyle = '#ffffff';
      ctx.strokeRect(x0 + s.x * z + 0.5, y0 + s.y * z + 0.5, s.w * z - 1, s.h * z - 1);
      ctx.strokeStyle = '#000000';
      ctx.lineDashOffset = 3;
      ctx.strokeRect(x0 + s.x * z + 0.5, y0 + s.y * z + 0.5, s.w * z - 1, s.h * z - 1);
      ctx.setLineDash([]);
      ctx.lineDashOffset = 0;
    }
    if (state.hover && !d && !state.space && state.tool !== 'select') {
      const [hx, hy] = state.hover;
      if (hx >= 0 && hy >= 0 && hx < m.w && hy < m.h) {
        ctx.strokeStyle = state.tool === 'eraser' ? '#ff6060' : '#ffffff';
        ctx.strokeRect(x0 + hx * z + 0.5, y0 + hy * z + 0.5, z - 1, z - 1);
      }
    }
  }

  // line draws the lines every step pixels over the image.
  function line(alpha, step, x0, y0, z) {
    const m = state.model;
    ctx.strokeStyle = `rgba(0, 0, 0, ${alpha})`;
    ctx.lineWidth = 1;
    ctx.beginPath();
    for (let x = step; x < m.w; x += step) {
      const sx = Math.round(x0 + x * z) + 0.5;
      ctx.moveTo(sx, y0);
      ctx.lineTo(sx, y0 + m.h * z);
    }
    for (let y = step; y < m.h; y += step) {
      const sy = Math.round(y0 + y * z) + 0.5;
      ctx.moveTo(x0, sy);
      ctx.lineTo(x0 + m.w * z, sy);
    }
    ctx.stroke();
  }

  // pixelAt turns a pointer position into a pixel of the image (it may be outside).
  function pixelAt(e) {
    const r = canvas.getBoundingClientRect();
    return [Math.floor((e.clientX - r.left - state.panX) / state.zoom), Math.floor((e.clientY - r.top - state.panY) / state.zoom)];
  }

  // ---------------------------------------------------------------- the side panels

  function swatch(c, title) {
    const s = document.createElement('span');
    s.className = 'swatch';
    const i = document.createElement('i');
    i.style.background = `rgba(${c[0]}, ${c[1]}, ${c[2]}, ${c[3] / 255})`;
    s.appendChild(i);
    s.title = title || ts.hex(c);
    s.addEventListener('click', () => setColor(0, c));
    s.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      setColor(1, c);
    });
    return s;
  }

  function setColor(k, c) {
    state.colors[k] = c.slice();
    remember();
    colorPanel();
  }

  function colorPanel() {
    const [p, s] = state.colors;
    const paint = (id, c) => {
      const e = el(id);
      e.innerHTML = '';
      const i = document.createElement('i');
      i.style.background = `rgba(${c[0]}, ${c[1]}, ${c[2]}, ${c[3] / 255})`;
      e.appendChild(i);
      e.title = (id === 'primary' ? 'Left button: ' : 'Right button: ') + ts.hex(c);
    };
    paint('primary', p);
    paint('secondary', s);
    el('colorpick').value = ts.hex([p[0], p[1], p[2], 255]);
    if (document.activeElement !== el('hex')) {
      el('hex').value = ts.hex(p);
    }
    el('alpha').value = String(p[3]);
    el('alphavalue').textContent = String(p[3]);
  }

  function used() {
    const box = el('used');
    box.innerHTML = '';
    if (!state.model) {
      return;
    }
    for (const c of ts.colors(current(), 48)) {
      box.appendChild(swatch(c));
    }
  }

  function strip() {
    const box = el('strip');
    box.innerHTML = '';
    const m = state.model;
    if (!m) {
      return;
    }
    m.frames.forEach((px, i) => {
      const d = document.createElement('div');
      d.className = 'frame' + (i === state.frame ? ' selected' : '');
      const c = document.createElement('canvas');
      c.style.width = `${Math.round((40 * m.w) / m.h)}px`;
      thumb(c, px);
      d.appendChild(c);
      const n = document.createElement('span');
      n.textContent = String(i);
      d.appendChild(n);
      d.addEventListener('click', () => selectFrame(i));
      box.appendChild(d);
    });
    const sel = box.children[state.frame];
    if (sel) {
      sel.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    }
  }

  function panels() {
    document.querySelectorAll('#tools button').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.tool === state.tool)));
    el('grid').checked = state.grid;
    el('onion').checked = state.onion;
    el('selsection').hidden = !(state.selection || state.floating || (state.tool === 'select' && state.clipboard));
    el('play').setAttribute('aria-pressed', String(state.playing));
    el('pscale').value = String(state.pscale);
    const m = state.model;
    if (m) {
      if (document.activeElement !== el('fps')) {
        el('fps').value = String(m.fps);
      }
      el('delframe').disabled = m.frames.length < 2;
      el('left').disabled = state.frame === 0;
      el('right').disabled = state.frame >= m.frames.length - 1;
      el('fps').disabled = m.frames.length < 2;
    }
    colorPanel();
  }

  // preview draws the frame shown (the animation's when it plays): a plain tile 3 × 3
  // times, to see its seams; an autotile as a map draws it.
  function preview() {
    const m = state.model;
    const c = el('preview');
    if (!m) {
      return;
    }
    const px = m.frames[state.playing ? state.playFrame % m.frames.length : state.frame];
    let img;
    if (m.autotile) {
      img = ts.autoPreview(px, state.tileSize);
      el('previewhint').textContent = 'A map painted with the autotile.';
    } else {
      const n = 3;
      const data = new Uint8ClampedArray(m.w * n * m.h * n * 4);
      for (let j = 0; j < m.h * n; j++) {
        for (let k = 0; k < n; k++) {
          const s = (j % m.h) * m.w * 4;
          data.set(px.subarray(s, s + m.w * 4), (j * m.w * n + k * m.w) * 4);
        }
      }
      img = { w: m.w * n, h: m.h * n, data };
      el('previewhint').textContent = 'The tile 3 × 3 times, as cells side by side.';
    }
    const k = state.pscale;
    c.width = img.w;
    c.height = img.h;
    c.style.width = `${img.w * k}px`;
    c.style.height = `${img.h * k}px`;
    c.getContext('2d').putImageData(new ImageData(img.data, img.w, img.h), 0, 0);
  }

  function stopPlaying() {
    clearInterval(state.timer);
    state.timer = null;
    state.playing = false;
  }

  function togglePlay() {
    if (state.playing) {
      stopPlaying();
    } else if (state.model && state.model.frames.length > 1) {
      state.playing = true;
      state.playFrame = state.frame;
      state.timer = setInterval(() => {
        state.playFrame = (state.playFrame + 1) % state.model.frames.length;
        preview();
      }, 1000 / Math.max(0.1, state.model.fps));
    }
    panels();
    preview();
  }

  // ---------------------------------------------------------------- tools

  function setTool(t) {
    if (t !== 'select') {
      commitFloating();
      state.selection = null;
    }
    state.tool = t;
    remember();
    panels();
    request();
  }

  function selectFrame(i) {
    commitFloating();
    state.frame = i;
    frameChanged();
    panels();
  }

  // The gesture on the canvas.
  canvas.addEventListener('contextmenu', (e) => e.preventDefault());

  canvas.addEventListener('pointerdown', (e) => {
    const m = state.model;
    if (!m) {
      return;
    }
    canvas.setPointerCapture(e.pointerId);
    if (e.button === 1 || state.space) {
      state.drag = { tool: 'pan', x: e.clientX, y: e.clientY, panX: state.panX, panY: state.panY };
      return;
    }
    if (e.button !== 0 && e.button !== 2) {
      return;
    }
    const button = e.button === 2 ? 1 : 0;
    const p = pixelAt(e);
    const inside = p[0] >= 0 && p[1] >= 0 && p[0] < m.w && p[1] < m.h;
    let tool = state.tool;
    if (e.altKey) {
      tool = 'pick';
    }
    const f = state.frame;
    switch (tool) {
      case 'pick':
        if (inside) {
          setColor(button, ts.getPixel(current(), m.w, p[0], p[1]));
        }
        return;
      case 'fill':
        if (inside) {
          const before = current().slice();
          ts.flood(current(), m.w, m.h, p[0], p[1], state.colors[button]);
          pixelEdit('Fill', f, before);
        }
        return;
      case 'pencil':
      case 'eraser': {
        const c = tool === 'eraser' ? TRANSPARENT : state.colors[button];
        const before = current().slice();
        const from = e.shiftKey && state.last ? state.last : p;
        for (const [x, y] of ts.linePoints(from[0], from[1], p[0], p[1])) {
          ts.setPixel(current(), m.w, m.h, x, y, c);
        }
        state.drag = { tool, before, color: c, at: p, frame: f };
        state.last = p;
        frameChanged(true);
        return;
      }
      case 'line':
      case 'rect':
        state.drag = { tool, button, start: p, end: p, filled: e.shiftKey, frame: f };
        request();
        return;
      case 'select': {
        const fl = state.floating;
        if (fl && p[0] >= fl.x && p[1] >= fl.y && p[0] < fl.x + fl.block.w && p[1] < fl.y + fl.block.h) {
          state.drag = { tool: 'move', from: p, x: fl.x, y: fl.y };
          return;
        }
        const s = state.selection;
        if (!fl && s && p[0] >= s.x && p[1] >= s.y && p[0] < s.x + s.w && p[1] < s.y + s.h) {
          lift(true);
          state.drag = { tool: 'move', from: p, x: s.x, y: s.y };
          return;
        }
        commitFloating();
        state.selection = null;
        state.drag = { tool: 'select', start: p, end: p };
        panels();
        request();
        return;
      }
      default:
    }
  });

  canvas.addEventListener('pointermove', (e) => {
    const m = state.model;
    if (!m) {
      return;
    }
    const p = pixelAt(e);
    state.hover = p;
    status(p);
    const d = state.drag;
    if (!d) {
      request();
      return;
    }
    switch (d.tool) {
      case 'pan':
        state.panX = d.panX + e.clientX - d.x;
        state.panY = d.panY + e.clientY - d.y;
        break;
      case 'pencil':
      case 'eraser':
        if (p[0] !== d.at[0] || p[1] !== d.at[1]) {
          for (const [x, y] of ts.linePoints(d.at[0], d.at[1], p[0], p[1])) {
            ts.setPixel(m.frames[d.frame], m.w, m.h, x, y, d.color);
          }
          d.at = p;
          state.last = p;
          frameChanged(true);
        }
        break;
      case 'line':
      case 'rect':
        d.end = p;
        d.filled = e.shiftKey;
        break;
      case 'select':
        d.end = p;
        state.selection = clampRect(d.start, d.end);
        break;
      case 'move':
        state.floating.x = d.x + p[0] - d.from[0];
        state.floating.y = d.y + p[1] - d.from[1];
        break;
      default:
    }
    request();
  });

  function endDrag() {
    const d = state.drag;
    state.drag = null;
    if (!d || !state.model) {
      return;
    }
    const m = state.model;
    switch (d.tool) {
      case 'pencil':
      case 'eraser':
        pixelEdit(d.tool === 'eraser' ? 'Erase' : 'Draw', d.frame, d.before);
        break;
      case 'line':
      case 'rect': {
        const px = m.frames[d.frame];
        const before = px.slice();
        const pts = d.tool === 'line' ? ts.linePoints(d.start[0], d.start[1], d.end[0], d.end[1])
          : ts.rectPoints(d.start[0], d.start[1], d.end[0], d.end[1], d.filled);
        for (const [x, y] of pts) {
          ts.setPixel(px, m.w, m.h, x, y, state.colors[d.button]);
        }
        pixelEdit(d.tool === 'line' ? 'Line' : 'Rectangle', d.frame, before);
        break;
      }
      case 'select':
        state.selection = clampRect(d.start, d.end);
        panels();
        break;
      default:
    }
    request();
  }

  canvas.addEventListener('pointerup', endDrag);
  canvas.addEventListener('pointercancel', endDrag);

  canvas.addEventListener('pointerleave', () => {
    state.hover = null;
    status(null);
    request();
  });

  canvas.addEventListener('wheel', (e) => {
    e.preventDefault();
    if (!state.model) {
      return;
    }
    const r = canvas.getBoundingClientRect();
    const step = e.deltaY < 0 ? 1.25 : 0.8;
    zoomAt(Math.max(state.zoom + (e.deltaY < 0 ? 1 : -1), state.zoom * step), e.clientX - r.left, e.clientY - r.top);
  }, { passive: false });

  canvas.addEventListener('auxclick', (e) => e.preventDefault());

  function status(p) {
    const m = state.model;
    if (!p || !m || p[0] < 0 || p[1] < 0 || p[0] >= m.w || p[1] >= m.h) {
      el('pos').textContent = ' ';
      el('pixel').textContent = '';
      el('tile').textContent = '';
      return;
    }
    el('pos').textContent = `${p[0]}, ${p[1]}`;
    el('pixel').textContent = ts.hex(ts.getPixel(current(), m.w, p[0], p[1]));
    if (state.tileSize > 0) {
      const t = ts.tileAt(state.tileSize, p[0], p[1]);
      el('tile').textContent = `${t.name} (${p[0] % state.tileSize}, ${p[1] % state.tileSize})`;
    } else {
      el('tile').textContent = '';
    }
  }

  // clampRect is the rectangle between two pixels, inside the image, or null.
  function clampRect(a, b) {
    const m = state.model;
    const x0 = Math.max(0, Math.min(a[0], b[0]));
    const y0 = Math.max(0, Math.min(a[1], b[1]));
    const x1 = Math.min(m.w - 1, Math.max(a[0], b[0]));
    const y1 = Math.min(m.h - 1, Math.max(a[1], b[1]));
    return x1 < x0 || y1 < y0 ? null : { x: x0, y: y0, w: x1 - x0 + 1, h: y1 - y0 + 1 };
  }

  // ---------------------------------------------------------------- the selection

  // lift takes the selected pixels off the frame to move them (or a copy of them, keeping
  // the frame as it is, when cut is false).
  function lift(cut) {
    const s = state.selection;
    const m = state.model;
    const px = current();
    const before = px.slice();
    const block = ts.copyRect(px, m.w, s.x, s.y, s.w, s.h);
    if (cut) {
      for (let y = s.y; y < s.y + s.h; y++) {
        for (let x = s.x; x < s.x + s.w; x++) {
          ts.setPixel(px, m.w, m.h, x, y, TRANSPARENT);
        }
      }
    }
    state.floating = { block, x: s.x, y: s.y, before, frame: state.frame };
    state.selection = null;
    frameChanged();
    panels();
  }

  // commitFloating places what moves; one edit with its lifting.
  function commitFloating() {
    const f = state.floating;
    if (!f || !state.model) {
      return;
    }
    state.floating = null;
    const m = state.model;
    const px = m.frames[f.frame];
    ts.pasteRect(px, m.w, m.h, f.block, f.x, f.y, false);
    state.selection = clampRect([f.x, f.y], [f.x + f.block.w - 1, f.y + f.block.h - 1]);
    pixelEdit('Move', f.frame, f.before);
    panels();
  }

  function cancelFloating() {
    const f = state.floating;
    if (!f) {
      state.selection = null;
    } else {
      state.model.frames[f.frame].set(f.before);
      state.floating = null;
      frameChanged();
    }
    panels();
    request();
  }

  function selected() {
    const f = state.floating;
    if (f) {
      return f.block;
    }
    const s = state.selection;
    return s ? ts.copyRect(current(), state.model.w, s.x, s.y, s.w, s.h) : null;
  }

  function copy() {
    const b = selected();
    if (b) {
      state.clipboard = b;
      vscode.postMessage({ type: 'clipboard', block: { w: b.w, h: b.h, data: toBase64(b.data) } });
      panels();
    }
  }

  function clearSelection() {
    const m = state.model;
    if (state.floating) {
      const f = state.floating;
      state.floating = null;
      pixelEdit('Clear', f.frame, f.before);
      panels();
      return;
    }
    const s = state.selection;
    if (!s) {
      return;
    }
    const before = current().slice();
    for (let y = s.y; y < s.y + s.h; y++) {
      for (let x = s.x; x < s.x + s.w; x++) {
        ts.setPixel(current(), m.w, m.h, x, y, TRANSPARENT);
      }
    }
    pixelEdit('Clear', state.frame, before);
  }

  function cut() {
    copy();
    clearSelection();
  }

  // paste puts the clipboard over the frame where the selection is (else the top left
  // corner of the view), moving until placed.
  function paste() {
    const b = state.clipboard;
    const m = state.model;
    if (!b || !m) {
      return;
    }
    commitFloating();
    const s = state.selection;
    let x = s ? s.x : Math.max(0, Math.floor(-state.panX / state.zoom));
    let y = s ? s.y : Math.max(0, Math.floor(-state.panY / state.zoom));
    x = Math.min(x, Math.max(0, m.w - b.w));
    y = Math.min(y, Math.max(0, m.h - b.h));
    state.floating = { block: { w: b.w, h: b.h, data: b.data.slice() }, x, y, before: current().slice(), frame: state.frame };
    state.selection = null;
    if (state.tool !== 'select') {
      state.tool = 'select';
      remember();
    }
    panels();
    request();
  }

  // transform flips or turns what is selected (lifting it first).
  function transform(fn) {
    if (!state.floating) {
      if (!state.selection) {
        return;
      }
      lift(true);
    }
    const f = state.floating;
    f.block = fn(f.block);
    request();
  }

  // ---------------------------------------------------------------- frames

  function duplicateFrame() {
    commitFloating();
    const m = state.model;
    const frames = m.frames.slice();
    frames.splice(state.frame + 1, 0, current().slice());
    framesEdit('Copy frame', frames, state.frame + 1);
  }

  function newFrame() {
    commitFloating();
    const m = state.model;
    const frames = m.frames.slice();
    frames.splice(state.frame + 1, 0, new Uint8ClampedArray(m.w * m.h * 4));
    framesEdit('New frame', frames, state.frame + 1);
  }

  function deleteFrame() {
    commitFloating();
    const m = state.model;
    if (m.frames.length < 2) {
      return;
    }
    const frames = m.frames.slice();
    frames.splice(state.frame, 1);
    framesEdit('Delete frame', frames, Math.min(state.frame, frames.length - 1));
  }

  function moveFrame(d) {
    commitFloating();
    const m = state.model;
    const to = state.frame + d;
    if (to < 0 || to >= m.frames.length) {
      return;
    }
    const frames = m.frames.slice();
    [frames[state.frame], frames[to]] = [frames[to], frames[state.frame]];
    framesEdit('Move frame', frames, to);
  }

  function setFps(v) {
    const m = state.model;
    const fps = Number(v);
    if (!m || !(fps > 0 && fps <= 1000) || fps === m.fps) {
      panels();
      return;
    }
    const before = m.fps;
    m.fps = fps;
    push('Frames a second', () => {
      m.fps = before;
    }, () => {
      m.fps = fps;
    });
    if (state.playing) {
      stopPlaying();
      togglePlay();
    }
    panels();
  }

  // ---------------------------------------------------------------- buttons and keys

  document.querySelectorAll('#tools button').forEach((b) => b.addEventListener('click', () => setTool(b.dataset.tool)));
  el('primary').addEventListener('click', () => el('colorpick').click());
  el('secondary').addEventListener('click', () => {
    state.colors.reverse();
    remember();
    colorPanel();
  });
  el('colorpick').addEventListener('input', () => {
    const c = ts.parseHex(el('colorpick').value);
    if (c) {
      c[3] = state.colors[0][3] || 255;
      setColor(0, c);
    }
  });
  el('hex').addEventListener('change', () => {
    const c = ts.parseHex(el('hex').value);
    if (c) {
      setColor(0, c);
    } else {
      colorPanel();
    }
  });
  el('alpha').addEventListener('input', () => {
    const c = state.colors[0].slice();
    c[3] = Number(el('alpha').value);
    setColor(0, c);
  });
  for (const h of PALETTE) {
    el('swatches').appendChild(swatch(ts.parseHex(h), h === '#00000000' ? 'transparent' : h));
  }
  el('grid').addEventListener('change', () => {
    state.grid = el('grid').checked;
    remember();
    request();
  });
  el('onion').addEventListener('change', () => {
    state.onion = el('onion').checked;
    remember();
    request();
  });
  el('zoomin').addEventListener('click', () => {
    const v = viewSize();
    zoomAt(state.zoom * 1.25 + 1, v.w / 2, v.h / 2);
  });
  el('zoomout').addEventListener('click', () => {
    const v = viewSize();
    zoomAt(state.zoom * 0.8, v.w / 2, v.h / 2);
  });
  el('fit').addEventListener('click', fit);
  el('openjson').addEventListener('click', () => vscode.postMessage({ type: 'openJson' }));
  el('copy').addEventListener('click', copy);
  el('cut').addEventListener('click', cut);
  el('paste').addEventListener('click', paste);
  el('clear').addEventListener('click', clearSelection);
  el('fliph').addEventListener('click', () => transform((b) => ts.flipBlock(b, true)));
  el('flipv').addEventListener('click', () => transform((b) => ts.flipBlock(b, false)));
  el('rotate').addEventListener('click', () => transform(ts.rotateBlock));
  el('play').addEventListener('click', togglePlay);
  el('pscale').addEventListener('change', () => {
    state.pscale = Number(el('pscale').value) || 2;
    remember();
    preview();
  });
  el('dupframe').addEventListener('click', duplicateFrame);
  el('newframe').addEventListener('click', newFrame);
  el('delframe').addEventListener('click', deleteFrame);
  el('left').addEventListener('click', () => moveFrame(-1));
  el('right').addEventListener('click', () => moveFrame(1));
  el('fps').addEventListener('change', () => setFps(el('fps').value));
  el('lake').addEventListener('click', () => {
    commitFloating();
    const before = current().slice();
    ts.lakeFromIsland(current(), state.tileSize);
    pixelEdit('Start the lake', state.frame, before);
  });

  // Copy, cut and paste come as the page's own events: VS Code runs them in the webview
  // for Ctrl+C, Ctrl+X and Ctrl+V.
  const typing = (e) => {
    const t = e.target;
    return t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT');
  };
  document.addEventListener('copy', (e) => {
    if (!typing(e) && (state.selection || state.floating)) {
      e.preventDefault();
      copy();
    }
  });
  document.addEventListener('cut', (e) => {
    if (!typing(e) && (state.selection || state.floating)) {
      e.preventDefault();
      cut();
    }
  });
  document.addEventListener('paste', (e) => {
    if (!typing(e) && state.clipboard) {
      e.preventDefault();
      paste();
    }
  });

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
    if (!state.model) {
      return;
    }
    if (TOOLS[k] && !e.altKey && !e.shiftKey) {
      setTool(TOOLS[k]);
    } else if (k === 'x') {
      state.colors.reverse();
      remember();
      colorPanel();
    } else if (k === 'h') {
      state.grid = !state.grid;
      remember();
      panels();
      request();
    } else if (k === 'o') {
      state.onion = !state.onion;
      remember();
      panels();
      request();
    } else if (k === 'p') {
      togglePlay();
    } else if (k === 'd') {
      duplicateFrame();
    } else if (e.key === '[' || e.key === ',') {
      selectFrame(Math.max(0, state.frame - 1));
    } else if (e.key === ']' || e.key === '.') {
      selectFrame(Math.min(state.model.frames.length - 1, state.frame + 1));
    } else if (k === 'f' && (state.selection || state.floating)) {
      transform((b) => ts.flipBlock(b, !e.shiftKey));
    } else if (k === 't' && (state.selection || state.floating)) {
      transform(ts.rotateBlock);
    } else if (e.key === 'Enter') {
      commitFloating();
    } else if (e.key === 'Escape') {
      cancelFloating();
    } else if (e.key === 'Delete' || e.key === 'Backspace') {
      clearSelection();
    } else if (state.floating && e.key.startsWith('Arrow')) {
      const f = state.floating;
      f.x += e.key === 'ArrowLeft' ? -1 : e.key === 'ArrowRight' ? 1 : 0;
      f.y += e.key === 'ArrowUp' ? -1 : e.key === 'ArrowDown' ? 1 : 0;
      request();
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
      case 'load':
        load(m);
        break;
      case 'undo':
        undo();
        break;
      case 'redo':
        redo();
        break;
      case 'getData':
        vscode.postMessage({ type: 'data', id: m.id, data: state.model ? data() : null });
        break;
      case 'clipboard':
        state.clipboard = { w: m.block.w, h: m.block.h, data: fromBase64(m.block.data) };
        panels();
        break;
      default:
        break;
    }
  });

  panels();
  vscode.postMessage({ type: 'ready' });
})();
