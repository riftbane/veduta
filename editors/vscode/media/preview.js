'use strict';
// The texture preview: it draws what the JSON in the editor beside it describes, with a
// grid to measure the picture by. The pixels come from lib/texture.js, which is the
// engine's layer program; this file only shows them and reads coordinates back.
(function () {
  const vscode = acquireVsCodeApi();
  const tex = globalThis.vedutaTexture;
  const png = globalThis.vedutaPng;

  const MARGIN = 22; // the ruler above and to the left of the picture
  const LIVE_PIXELS = 1 << 20; // drawn without asking; bigger textures wait for a click
  const ZOOMS = [1, 2, 3, 4, 6, 8, 12, 16, 24, 32];
  const STEPS = [1, 2, 4, 8, 16, 32, 64, 128, 256, 512];

  const el = (id) => document.getElementById(id);
  const canvas = el('canvas');
  const ctx = canvas.getContext('2d');
  const off = document.createElement('canvas');
  const offCtx = off.getContext('2d');

  const saved = vscode.getState() || {};
  const state = {
    file: '',
    spec: null,
    image: null,
    skip: [],
    zoom: saved.zoom || 0, // 0: fit the panel
    grid: saved.grid !== false,
    repeat: !!saved.repeat,
    force: false,
    measure: null,
    dragging: false,
  };
  el('grid').checked = state.grid;
  el('repeat').checked = state.repeat;

  function save() {
    vscode.setState({ zoom: state.zoom, grid: state.grid, repeat: state.repeat });
  }

  // decoded keeps the pictures of the image layers, so typing does not decode the same
  // PNG again on every keystroke.
  const decoded = new Map();

  function imageOf(path, base64) {
    const was = decoded.get(path);
    if (was && was.base64 === base64) {
      return was.img;
    }
    let img;
    try {
      img = png.decode(bytesOf(base64));
    } catch (e) {
      img = { error: String((e && e.message) || e) };
    }
    decoded.set(path, { base64, img });
    return img;
  }

  function bytesOf(base64) {
    const s = atob(base64);
    const out = new Uint8Array(s.length);
    for (let i = 0; i < s.length; i++) {
      out[i] = s.charCodeAt(i);
    }
    return out;
  }

  // update takes a new source from the extension and draws it, keeping the picture that
  // is on screen when the source does not compile.
  function update(msg) {
    state.file = msg.file;
    el('name').textContent = msg.file;
    const parsed = tex.parse(msg.text);
    if (!parsed.src) {
      fail([{ path: '', msg: parsed.error }]);
      return;
    }
    const images = {};
    for (const p of Object.keys(msg.images || {})) {
      const v = msg.images[p];
      if (!v || v.error) {
        images[p] = { error: (v && v.error) || 'not read' };
        continue;
      }
      images[p] = imageOf(p, v);
    }
    const { spec, errors } = tex.validate(parsed.src, images);
    if (errors.length > 0) {
      fail(errors);
      return;
    }
    el('error').hidden = true;
    el('view').classList.remove('stale');
    if (state.skip.length !== spec.layers.length) {
      state.skip = spec.layers.map(() => false);
    }
    state.spec = spec;
    layerButtons();
    el('repeatbox').hidden = !spec.tiling;
    if (!spec.tiling) {
      state.repeat = false;
      el('repeat').checked = false;
    }
    redraw();
  }

  // fail shows why the engine would refuse this source; the last picture stays, dimmed.
  function fail(errors) {
    const box = el('error');
    const lines = errors.slice(0, 8).map((e) => (e.path ? e.path + ': ' : '') + e.msg);
    if (errors.length > 8) {
      lines.push(`and ${errors.length - 8} more`);
    }
    box.textContent = lines.join('\n');
    box.hidden = false;
    el('view').classList.add('stale');
  }

  // redraw renders the texture again (the source or the hidden layers changed) and draws it.
  function redraw() {
    const spec = state.spec;
    if (!spec) {
      return;
    }
    if (spec.w * spec.h * Math.max(spec.frames.length, 1) > LIVE_PIXELS && !state.force) {
      state.image = null;
      note(`${spec.w}x${spec.h} is big to draw while you type.`, 'Draw it anyway', () => {
        state.force = true;
        redraw();
      });
      el('size').textContent = `${spec.w}x${spec.h}`;
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      return;
    }
    el('note').hidden = true;
    state.image = tex.render(spec, state.skip);
    off.width = state.image.w;
    off.height = state.image.h;
    offCtx.putImageData(new ImageData(state.image.data, state.image.w, state.image.h), 0, 0);
    const g = spec.grid;
    el('size').textContent = `${state.image.w}x${state.image.h}` + (g[0] > 0 ? `, ${g[0]}x${g[1]} frames of ${state.image.w / g[0]}x${state.image.h / g[1]}` : '');
    draw();
  }

  function note(text, label, onClick) {
    const box = el('note');
    box.textContent = text + ' ';
    const b = document.createElement('button');
    b.textContent = label;
    b.addEventListener('click', onClick);
    box.appendChild(b);
    box.hidden = false;
  }

  function layerButtons() {
    const box = el('layers');
    box.textContent = '';
    state.spec.layers.forEach((l, i) => {
      const b = document.createElement('button');
      b.textContent = `${i} ${l.type}`;
      b.title = 'Hide this layer in the preview';
      b.setAttribute('aria-pressed', String(!state.skip[i]));
      b.addEventListener('click', () => {
        state.skip[i] = !state.skip[i];
        b.setAttribute('aria-pressed', String(!state.skip[i]));
        redraw();
      });
      box.appendChild(b);
    });
  }

  function tiles() {
    return state.repeat ? 2 : 1;
  }

  // scale is the pixels on screen per texel: the chosen zoom, or the largest whole number
  // that fits the panel.
  function scale() {
    if (state.zoom > 0) {
      return state.zoom;
    }
    if (!state.image) {
      return 1;
    }
    const view = el('view');
    const n = tiles();
    const z = Math.min(
      (view.clientWidth - 18 - MARGIN) / (state.image.w * n),
      (view.clientHeight - 18 - MARGIN) / (state.image.h * n));
    return Math.max(1, Math.min(32, Math.floor(z)));
  }

  // step is the grid spacing in texels: the smallest of STEPS that keeps its lines at
  // least 24 pixels apart.
  function step(z) {
    for (const s of STEPS) {
      if (s * z >= 24) {
        return s;
      }
    }
    return STEPS[STEPS.length - 1];
  }

  function draw() {
    const img = state.image;
    if (!img) {
      return;
    }
    const z = scale();
    const n = tiles();
    const w = img.w * z * n;
    const h = img.h * z * n;
    const dpr = window.devicePixelRatio || 1;
    canvas.style.width = `${w + MARGIN}px`;
    canvas.style.height = `${h + MARGIN}px`;
    canvas.width = Math.round((w + MARGIN) * dpr);
    canvas.height = Math.round((h + MARGIN) * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w + MARGIN, h + MARGIN);
    el('zoom').textContent = `${z}x`;

    // The gray checkerboard under the picture, the same one the engine's own image sheets
    // use, so transparency reads as transparency.
    for (let y = 0; y < h; y += 8) {
      for (let x = 0; x < w; x += 8) {
        ctx.fillStyle = ((x / 8 + y / 8) & 1) ? '#989898' : '#707070';
        ctx.fillRect(MARGIN + x, MARGIN + y, Math.min(8, w - x), Math.min(8, h - y));
      }
    }
    ctx.imageSmoothingEnabled = false;
    for (let ty = 0; ty < n; ty++) {
      for (let tx = 0; tx < n; tx++) {
        ctx.drawImage(off, MARGIN + tx * img.w * z, MARGIN + ty * img.h * z, img.w * z, img.h * z);
      }
    }
    if (state.grid) {
      grid(z, w, h, img);
    }
    if (state.measure) {
      const m = state.measure;
      ctx.strokeStyle = '#ffcc00';
      ctx.lineWidth = 1;
      ctx.setLineDash([3, 3]);
      ctx.strokeRect(MARGIN + m.x * z + 0.5, MARGIN + m.y * z + 0.5, m.w * z - 1, m.h * z - 1);
      ctx.setLineDash([]);
    }
  }

  // grid draws the ruler: a faint line per texel when they are far enough apart, a
  // stronger line and a number every step texels, and the outline of the texture itself.
  function grid(z, w, h, img) {
    const s = step(z);
    ctx.save();
    ctx.translate(MARGIN, MARGIN);
    if (z >= 8) {
      ctx.strokeStyle = 'rgba(127, 127, 127, 0.25)';
      ctx.beginPath();
      for (let x = 1; x < img.w * tiles(); x++) {
        ctx.moveTo(x * z + 0.5, 0);
        ctx.lineTo(x * z + 0.5, h);
      }
      for (let y = 1; y < img.h * tiles(); y++) {
        ctx.moveTo(0, y * z + 0.5);
        ctx.lineTo(w, y * z + 0.5);
      }
      ctx.stroke();
    }
    ctx.strokeStyle = 'rgba(127, 127, 127, 0.6)';
    ctx.beginPath();
    for (let x = s; x < img.w * tiles(); x += s) {
      ctx.moveTo(x * z + 0.5, 0);
      ctx.lineTo(x * z + 0.5, h);
    }
    for (let y = s; y < img.h * tiles(); y += s) {
      ctx.moveTo(0, y * z + 0.5);
      ctx.lineTo(w, y * z + 0.5);
    }
    ctx.stroke();
    ctx.strokeStyle = 'rgba(127, 127, 127, 0.95)';
    ctx.strokeRect(0.5, 0.5, w - 1, h - 1);
    ctx.restore();

    ctx.fillStyle = getComputedStyle(document.body).getPropertyValue('--vscode-descriptionForeground') || '#888';
    ctx.font = '10px monospace';
    ctx.textBaseline = 'bottom';
    for (let x = 0; x <= img.w * tiles(); x += s) {
      if (x * z <= w) {
        ctx.textAlign = x === 0 ? 'left' : 'center';
        ctx.fillText(String(state.repeat ? x % img.w : x), MARGIN + x * z, MARGIN - 4);
      }
    }
    ctx.textAlign = 'right';
    ctx.textBaseline = 'middle';
    for (let y = 0; y <= img.h * tiles(); y += s) {
      if (y * z <= h) {
        ctx.fillText(String(state.repeat ? y % img.h : y), MARGIN - 4, MARGIN + y * z + (y === 0 ? 4 : 0));
      }
    }
  }

  // texelAt turns a mouse position into a texel of the texture, or null outside it.
  function texelAt(e) {
    const img = state.image;
    if (!img) {
      return null;
    }
    const z = scale();
    const r = canvas.getBoundingClientRect();
    const x = Math.floor((e.clientX - r.left - MARGIN) / z);
    const y = Math.floor((e.clientY - r.top - MARGIN) / z);
    if (x < 0 || y < 0 || x >= img.w * tiles() || y >= img.h * tiles()) {
      return null;
    }
    return { x, y, tx: x % img.w, ty: y % img.h };
  }

  function hex(n) {
    return n.toString(16).padStart(2, '0');
  }

  canvas.addEventListener('mousemove', (e) => {
    const p = texelAt(e);
    if (!p) {
      el('pos').textContent = ' ';
      el('color').textContent = '';
      el('swatch').style.background = 'transparent';
      return;
    }
    const d = state.image.data;
    const k = (p.ty * state.image.w + p.tx) * 4;
    el('pos').textContent = `${p.tx}, ${p.ty}`;
    el('color').textContent = `#${hex(d[k])}${hex(d[k + 1])}${hex(d[k + 2])}${hex(d[k + 3])}`;
    el('swatch').style.background = `rgba(${d[k]}, ${d[k + 1]}, ${d[k + 2]}, ${d[k + 3] / 255})`;
    if (state.dragging && state.anchor) {
      const a = state.anchor;
      state.measure = {
        x: Math.min(a.x, p.x), y: Math.min(a.y, p.y),
        w: Math.abs(p.x - a.x) + 1, h: Math.abs(p.y - a.y) + 1,
      };
      const m = state.measure;
      el('measure').textContent = `${m.w}x${m.h} at ${m.x % state.image.w}, ${m.y % state.image.h}`;
      draw();
    }
  });

  canvas.addEventListener('mousedown', (e) => {
    const p = texelAt(e);
    if (!p) {
      return;
    }
    state.dragging = true;
    state.anchor = p;
    state.measure = { x: p.x, y: p.y, w: 1, h: 1 };
    el('measure').textContent = `1x1 at ${p.tx}, ${p.ty}`;
    draw();
  });

  window.addEventListener('mouseup', () => {
    state.dragging = false;
  });

  canvas.addEventListener('wheel', (e) => {
    if (!e.ctrlKey) {
      return;
    }
    e.preventDefault();
    const z = scale();
    let i = ZOOMS.indexOf(z);
    if (i < 0) {
      i = ZOOMS.findIndex((v) => v > z);
    }
    setZoom(ZOOMS[Math.max(0, Math.min(ZOOMS.length - 1, i + (e.deltaY < 0 ? 1 : -1)))]);
  }, { passive: false });

  function setZoom(z) {
    state.zoom = z;
    save();
    draw();
  }

  el('zoomin').addEventListener('click', () => {
    const z = scale();
    setZoom(ZOOMS.find((v) => v > z) || ZOOMS[ZOOMS.length - 1]);
  });
  el('zoomout').addEventListener('click', () => {
    const z = scale();
    const smaller = ZOOMS.filter((v) => v < z);
    setZoom(smaller.length ? smaller[smaller.length - 1] : 1);
  });
  el('fit').addEventListener('click', () => {
    state.zoom = 0;
    save();
    draw();
  });
  el('grid').addEventListener('change', (e) => {
    state.grid = e.target.checked;
    save();
    draw();
  });
  el('repeat').addEventListener('change', (e) => {
    state.repeat = e.target.checked;
    save();
    draw();
  });
  window.addEventListener('resize', () => {
    if (state.zoom === 0) {
      draw();
    }
  });
  window.addEventListener('message', (e) => {
    if (e.data && e.data.type === 'source') {
      update(e.data);
    }
  });
  vscode.postMessage({ type: 'ready' });
})();
