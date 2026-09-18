'use strict';
// The map editor's page, run without VS Code: media/mapeditor.js on a stub of the browser,
// with the elements taken from media/mapeditor.html. It checks the wiring the parity tests
// cannot see: the page loads a map it is sent, a gesture sends one edit of the whole text,
// a map that does not load shows why, and VS Code's own keys are left alone.

const test = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const path = require('path');
const vm = require('vm');
const tm = require('../lib/tilemap');

const dir = path.resolve(__dirname, '..');
const page = fs.readFileSync(path.join(dir, 'media', 'mapeditor.html'), 'utf8');
const farmDir = path.resolve(dir, '..', '..', 'testdata', 'tilemap');
const haveEngine = fs.existsSync(farmDir);

function context2d() {
  const calls = [];
  const c = { calls };
  for (const name of ['setTransform', 'clearRect', 'fillRect', 'drawImage', 'strokeRect', 'beginPath', 'moveTo', 'lineTo',
    'stroke', 'fillText', 'setLineDash', 'putImageData']) {
    c[name] = (...args) => calls.push({ name, args });
  }
  return c;
}

function element(tag, attrs = {}) {
  const e = {
    tag,
    textContent: '',
    value: '',
    hidden: false,
    disabled: false,
    checked: attrs.checked || false,
    title: '',
    className: '',
    dataset: attrs.dataset || {},
    style: {},
    width: 0,
    height: 0,
    children: [],
    listeners: {},
    attributes: {},
    classes: new Set(),
    setAttribute(k, v) { this.attributes[k] = v; },
    appendChild(c) { this.children.push(c); },
    addEventListener(name, fn) { (this.listeners[name] = this.listeners[name] || []).push(fn); },
    fire(name, ev = {}) {
      const event = Object.assign({ target: this, preventDefault() { this.prevented = true; }, stopPropagation() {} }, ev);
      (this.listeners[name] || []).forEach((fn) => fn(event));
      return event;
    },
    getBoundingClientRect() { return { left: 0, top: 0, width: 400, height: 300 }; },
    getContext() { return this.ctx || (this.ctx = context2d()); },
    setPointerCapture() {},
    hasPointerCapture() { return false; },
    releasePointerCapture() {},
  };
  e.classList = {
    add: (c) => e.classes.add(c),
    remove: (c) => e.classes.delete(c),
    toggle: (c, on) => (on ? e.classes.add(c) : e.classes.delete(c)),
    contains: (c) => e.classes.has(c),
  };
  Object.defineProperty(e, 'textContent', {
    get() { return this._text || ''; },
    set(v) { this._text = v; if (v === '') { this.children = []; } },
  });
  return e;
}

// browser builds the page's world and returns what the tests poke at.
function browser() {
  const elements = {};
  for (const m of page.matchAll(/id="([^"]+)"/g)) {
    elements[m[1]] = element('div', { checked: m[1] === 'grid' });
  }
  const tools = Array.from(page.matchAll(/data-tool="(\w+)"/g), (m) => element('button', { dataset: { tool: m[1] } }));
  const sizes = Array.from(page.matchAll(/data-size="(\d+)"/g), (m) => element('button', { dataset: { size: m[1] } }));
  const posted = [];
  const window = element('window');
  window.devicePixelRatio = 1;
  const body = element('body');
  const sandbox = {
    window,
    document: {
      body,
      getElementById: (id) => elements[id],
      createElement: (tag) => element(tag),
      querySelectorAll: (sel) => (sel === '#tools button' ? tools : sel === '#sizes button' ? sizes : []),
    },
    ImageData: class {
      constructor(data, w, h) {
        this.data = data;
        this.width = w;
        this.height = h;
      }
    },
    atob: (s) => Buffer.from(s, 'base64').toString('binary'),
    requestAnimationFrame: (fn) => fn(),
    acquireVsCodeApi: () => ({ getState: () => undefined, setState: () => {}, postMessage: (m) => posted.push(JSON.parse(JSON.stringify(m))) }),
    setTimeout,
    clearTimeout,
    setInterval,
    clearInterval,
    TextEncoder,
    Uint8ClampedArray,
  };
  sandbox.globalThis = sandbox;
  const ctx = vm.createContext(sandbox);
  for (const f of [['lib', 'png.js'], ['lib', 'texture.js'], ['lib', 'tilemap.js'], ['media', 'mapeditor.js']]) {
    vm.runInContext(fs.readFileSync(path.join(dir, ...f), 'utf8'), ctx, { filename: f.join('/') });
  }
  const send = (msg) => window.fire('message', { data: msg });
  const key = (k, extra = {}) => window.fire('keydown', Object.assign({ key: k, target: body }, extra));
  const edits = () => posted.filter((m) => m.type === 'edit');
  return { elements, tools, posted, send, key, edits, body };
}

function farmAssets() {
  const textures = {};
  for (const name of ['grass', 'water', 'sand', 'dirt']) {
    textures[name] = { text: fs.readFileSync(path.join(farmDir, name + '.vtex'), 'utf8'), file: `assets/textures/${name}.vtex` };
  }
  return { type: 'assets', textures, materials: {}, images: {}, textureNames: ['dirt', 'grass', 'sand', 'water', 'rock'], tickRate: 20 };
}

// open loads the farm into a page; at(x, y) is where the middle of cell (x, y) is on the
// 400 × 300 view, the map fitted in it.
function open() {
  const b = browser();
  const text = fs.readFileSync(path.join(farmDir, 'farm.vmap'), 'utf8');
  b.send(farmAssets());
  b.send({ type: 'document', text, file: 'farm.vmap' });
  const z = Math.min((400 - 16) / 320, (300 - 16) / 288);
  const px = (400 - 320 * z) / 2;
  const py = (300 - 288 * z) / 2;
  b.at = (x, y) => ({ clientX: px + (x + 0.5) * 16 * z, clientY: py + (y + 0.5) * 16 * z, button: 0, pointerId: 1 });
  b.text = text;
  b.last = () => {
    const e = b.edits();
    return tm.load(e[e.length - 1].text, 'farm.vmap').map;
  };
  return b;
}

test('the page asks for the document once it is loaded', () => {
  assert.deepStrictEqual(browser().posted, [{ type: 'ready' }]);
});

test('a map it is sent is drawn, with its terrains and layers', { skip: !haveEngine }, () => {
  const b = open();
  assert.strictEqual(b.elements.error.hidden, true);
  assert.deepStrictEqual(b.elements.palette.children.map((c) => c.children[2].children[0].textContent), ['grass', 'water', 'sand', 'dirt']);
  assert.deepStrictEqual(b.elements.layers.children.map((c) => c.children[1].textContent), ['paths', 'ground'], 'top first');
  assert.ok(b.elements.canvas.ctx.calls.some((c) => c.name === 'drawImage'), 'the map is drawn');
  assert.strictEqual(b.elements['m-w'].value, '20');
  assert.strictEqual(b.edits().length, 0, 'loading edits nothing');
});

test('a brush stroke is one edit of the whole text', { skip: !haveEngine }, () => {
  const b = open();
  b.key('2'); // water
  const c = b.elements.canvas;
  c.fire('pointerdown', b.at(0, 0));
  c.fire('pointermove', b.at(1, 0));
  c.fire('pointermove', b.at(3, 0));
  assert.strictEqual(b.edits().length, 0, 'nothing is sent before the pointer is up');
  c.fire('pointerup', b.at(3, 0));
  assert.strictEqual(b.edits().length, 1);
  const m = b.last();
  assert.deepStrictEqual([0, 1, 2, 3, 4].map((x) => tm.get(m, 0, x, 0)), [2, 2, 2, 2, 1], 'no gap between the pointer\'s steps');
  assert.strictEqual(b.edits()[0].text, tm.format(m), 'canonical text');
  // The echo of its own edit changes nothing.
  b.send({ type: 'document', text: b.edits()[0].text, file: 'farm.vmap' });
  assert.strictEqual(b.edits().length, 1);
});

test('rectangle, fill, eraser and eyedropper', { skip: !haveEngine }, () => {
  const b = open();
  const c = b.elements.canvas;
  b.key('r');
  b.key('3'); // sand
  c.fire('pointerdown', b.at(15, 15));
  c.fire('pointermove', b.at(17, 16));
  c.fire('pointerup', b.at(17, 16));
  let m = b.last();
  assert.strictEqual(tm.rows(m, 0)[16].slice(15, 18), 'sss');
  b.key('f');
  b.key('4'); // dirt, on the ground: the 4-connected grass from the corner
  c.fire('pointerdown', b.at(0, 0));
  c.fire('pointerup', b.at(0, 0));
  m = b.last();
  assert.strictEqual(tm.rows(m, 0)[0], 'dddddddddddddddddddd');
  assert.strictEqual(tm.rows(m, 0)[4][3], '~', 'the water is not grass');
  b.key('e');
  c.fire('pointerdown', b.at(19, 17));
  c.fire('pointerup', b.at(19, 17));
  assert.strictEqual(tm.get(b.last(), 0, 19, 17), 0);
  b.key('i');
  c.fire('pointerdown', b.at(3, 4));
  c.fire('pointerup', b.at(3, 4));
  assert.ok(b.elements.palette.children[1].className.includes('selected'), 'the water is picked');
});

test('objects are made, edited and deleted', { skip: !haveEngine }, () => {
  const b = open();
  const c = b.elements.canvas;
  b.key('o');
  c.fire('pointerdown', b.at(2, 2));
  c.fire('pointermove', b.at(4, 3));
  c.fire('pointerup', b.at(4, 3));
  let m = b.last();
  const o = m.objects[m.objects.length - 1];
  assert.deepStrictEqual([o.name, o.at, o.size], ['object_1', [2, 2], [3, 2]]);
  assert.strictEqual(b.elements.objectform.hidden, false);
  b.elements['o-name'].value = 'pond';
  b.elements['o-tags'].value = 'water, spawn';
  b.elements['o-props'].value = 'fish = 3\nname = carp\nfrozen = false\ncode = "12"';
  b.elements.objectform.fire('submit');
  m = b.last();
  const p = m.objects[m.objects.length - 1];
  assert.deepStrictEqual([p.name, p.tags, p.props], ['pond', ['water', 'spawn'], [['fish', 3], ['name', 'carp'], ['frozen', false], ['code', '12']]]);
  b.elements['o-name'].value = 'door'; // taken
  b.elements.objectform.fire('submit');
  assert.match(b.elements['o-error'].textContent, /taken/);
  const n = b.edits().length;
  b.key('Delete');
  assert.strictEqual(b.edits().length, n + 1);
  assert.ok(!b.last().objects.some((x) => x.name === 'pond'));
});

test('terrains and layers are added and removed', { skip: !haveEngine }, () => {
  const b = open();
  b.elements.texturepick.value = 'grass';
  b.elements.addterrain.fire('click');
  let m = b.last();
  assert.deepStrictEqual(m.terrains.map((t) => [t.key, t.name, t.texture]).pop(), ['#', 'grass_2', 'grass']);
  // Deleting the water, which paints cells, asks first.
  b.key('2');
  b.elements['t-delete'].fire('click');
  const ask = b.posted[b.posted.length - 1];
  assert.strictEqual(ask.type, 'confirm');
  assert.match(ask.message, /water paints \d+ cells/);
  b.send({ type: 'confirmed', id: ask.id, ok: true });
  m = b.last();
  assert.ok(!m.terrains.some((t) => t.name === 'water'));
  b.elements.addlayer.fire('click');
  m = b.last();
  assert.deepStrictEqual(m.layers.map((l) => l.name), ['ground', 'paths', 'layer']);
  b.elements['l-name'].value = 'roofs';
  b.elements['l-z'].value = '2';
  b.elements.layerform.fire('submit');
  m = b.last();
  assert.deepStrictEqual([m.layers[2].name, m.layers[2].z], ['roofs', 2]);
  b.elements['m-w'].value = '10';
  b.elements.mapform.fire('submit');
  m = b.last();
  assert.strictEqual(m.w, 10);
  assert.deepStrictEqual(m.objects.map((o) => o.name), [], 'the door at column 10 and the field are off the map');
});

test('a map that does not load shows why, and is not edited', { skip: !haveEngine }, () => {
  const b = open();
  b.send({ type: 'document', text: b.text.replace('"size": [20, 18]', '"size": [20, 0]'), file: 'farm.vmap' });
  assert.strictEqual(b.elements.error.hidden, false);
  assert.match(b.elements.errors.textContent, /^3:16: size\[1\]: 0 out of range \[1, 1024\]/);
  assert.ok(b.body.classes.has('broken'));
  b.send({ type: 'document', text: b.text, file: 'farm.vmap' });
  assert.strictEqual(b.elements.error.hidden, true);
  b.elements.openjson.fire('click');
  assert.strictEqual(b.posted[b.posted.length - 1].type, 'openJson');
});

test('VS Code keeps its own keys', { skip: !haveEngine }, () => {
  const b = open();
  for (const k of [{ key: 'z', ctrlKey: true }, { key: 's', ctrlKey: true }, { key: 'y', ctrlKey: true }, { key: 'z', metaKey: true, shiftKey: true }]) {
    assert.ok(!b.key(k.key, k).prevented, JSON.stringify(k));
  }
  assert.ok(b.key('b').prevented, 'B is the brush');
  const input = { tagName: 'INPUT' };
  assert.ok(!b.key('b', { target: input }).prevented, 'typing in a field is typing');
});

test('the page names only elements the html has', () => {
  const used = new Set(Array.from(fs.readFileSync(path.join(dir, 'media', 'mapeditor.js'), 'utf8').matchAll(/el\('([^']+)'\)/g), (m) => m[1]));
  const ids = new Set(Array.from(page.matchAll(/id="([^"]+)"/g), (m) => m[1]));
  for (const id of used) {
    assert.ok(ids.has(id), `mapeditor.html has #${id}`);
  }
});

test('the html asks for exactly the values the extension fills in', () => {
  const placeholders = new Set(Array.from(page.matchAll(/{{(\w+)}}/g), (m) => m[1]));
  const extension = fs.readFileSync(path.join(dir, 'extension.js'), 'utf8');
  const from = extension.indexOf('function mapEditorHtml');
  const provided = extension.slice(extension.indexOf('const values = {', from), extension.indexOf('const page = ', from));
  for (const k of placeholders) {
    assert.ok(new RegExp(`\\b${k}\\s*[:,]`).test(provided), `the extension provides ${k}`);
  }
});
