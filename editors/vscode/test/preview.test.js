'use strict';
// The preview page, run without VS Code: media/preview.js on a stub of the browser it
// lives in, with the elements taken from media/preview.html itself. It checks the wiring
// the golden test cannot see — that the page finds every element it names, draws the
// picture it is sent, and shows the errors of a source the engine would refuse.

const test = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const dir = path.resolve(__dirname, '..');
const page = fs.readFileSync(path.join(dir, 'media', 'preview.html'), 'utf8');

// element is the little of an HTML element the page uses.
function element(tag) {
  return {
    tag,
    textContent: '',
    hidden: false,
    checked: false,
    title: '',
    style: {},
    width: 0,
    height: 0,
    clientWidth: 400,
    clientHeight: 300,
    classes: new Set(),
    listeners: {},
    children: [],
    classList: {
      add(c) { this.owner.classes.add(c); },
      remove(c) { this.owner.classes.delete(c); },
    },
    attributes: {},
    setAttribute(k, v) { this.attributes[k] = v; },
    appendChild(c) { this.children.push(c); },
    addEventListener(name, fn) { this.listeners[name] = fn; },
    getBoundingClientRect() { return { left: 0, top: 0 }; },
    getContext() { return this.ctx || (this.ctx = context2d()); },
  };
}

// context2d records what the page draws.
function context2d() {
  const calls = [];
  const c = { calls };
  for (const name of ['setTransform', 'clearRect', 'fillRect', 'drawImage', 'strokeRect', 'beginPath',
    'moveTo', 'lineTo', 'stroke', 'save', 'restore', 'translate', 'fillText', 'setLineDash', 'putImageData']) {
    c[name] = (...args) => calls.push({ name, args });
  }
  return c;
}

// browser builds the page's world: one element per id in preview.html, the two libraries
// and preview.js itself, and returns what the test needs to poke at it.
function browser() {
  const elements = {};
  for (const m of page.matchAll(/id="([^"]+)"/g)) {
    const e = element('div');
    e.classList.owner = e;
    elements[m[1]] = e;
  }
  const posted = [];
  const window = {
    listeners: {},
    devicePixelRatio: 1,
    addEventListener(name, fn) { this.listeners[name] = fn; },
  };
  const sandbox = {
    window,
    document: {
      body: element('body'),
      getElementById: (id) => elements[id],
      createElement: (tag) => {
        const e = element(tag);
        e.classList.owner = e;
        return e;
      },
    },
    ImageData: class {
      constructor(data, w, h) {
        this.data = data;
        this.width = w;
        this.height = h;
      }
    },
    atob: (s) => Buffer.from(s, 'base64').toString('binary'),
    getComputedStyle: () => ({ getPropertyValue: () => '#888888' }),
    acquireVsCodeApi: () => ({
      getState: () => undefined,
      setState: () => {},
      postMessage: (m) => posted.push(m),
    }),
    setTimeout,
    clearTimeout,
  };
  sandbox.globalThis = sandbox;
  const ctx = vm.createContext(sandbox);
  for (const f of [['lib', 'png.js'], ['lib', 'texture.js'], ['media', 'preview.js']]) {
    vm.runInContext(fs.readFileSync(path.join(dir, ...f), 'utf8'), ctx, { filename: f.join('/') });
  }
  const send = (msg) => window.listeners.message({ data: msg });
  return { elements, posted, send };
}

const source = (layers, extra) => JSON.stringify(Object.assign({ veduta: 'texture/1', size: [16, 16], layers }, extra));

test('the page asks for its source once it is loaded', () => {
  // The messages are built inside the page's own world, so compare their contents.
  assert.deepStrictEqual(JSON.parse(JSON.stringify(browser().posted)), [{ type: 'ready' }]);
});

test('a source it is sent is drawn, with its size and its layers', () => {
  const b = browser();
  b.send({
    type: 'source',
    file: 'probe.tex.json',
    text: source([{ type: 'solid', color: '#204080' }, { type: 'circle', center: [8, 8], radius: 5, color: '#ff0000' }]),
    images: {},
  });
  assert.strictEqual(b.elements.name.textContent, 'probe.tex.json');
  assert.strictEqual(b.elements.size.textContent, '16x16');
  assert.strictEqual(b.elements.error.hidden, true);
  assert.deepStrictEqual(b.elements.layers.children.map((c) => c.textContent), ['0 solid', '1 circle']);
  const ctx = b.elements.canvas.ctx;
  assert.ok(ctx.calls.some((c) => c.name === 'putImageData' || c.name === 'drawImage'), 'the texture is drawn');
  assert.ok(ctx.calls.some((c) => c.name === 'fillText'), 'the ruler is written');
  assert.strictEqual(b.elements.repeatbox.hidden, true);
});

test('a tiling texture offers the repeat view', () => {
  const b = browser();
  b.send({ type: 'source', file: 'a.tex.json', text: source([{ type: 'solid', color: '#204080' }], { tiling: true }), images: {} });
  assert.strictEqual(b.elements.repeatbox.hidden, false);
});

test('a source the engine would refuse shows why, and keeps the last picture', () => {
  const b = browser();
  b.send({ type: 'source', file: 'a.tex.json', text: source([{ type: 'solid', color: '#204080' }]), images: {} });
  b.send({ type: 'source', file: 'a.tex.json', text: source([{ type: 'solid' }]), images: {} });
  assert.strictEqual(b.elements.error.hidden, false);
  assert.match(b.elements.error.textContent, /layers\[0\]\.color: is required/);
  assert.ok(b.elements.view.classes.has('stale'), 'the picture on screen is marked out of date');
});

test('text that is not JSON yet is reported, not thrown', () => {
  const b = browser();
  b.send({ type: 'source', file: 'a.tex.json', text: '{ "veduta": ', images: {} });
  assert.strictEqual(b.elements.error.hidden, false);
  assert.ok(b.elements.error.textContent.length > 0);
});

test('an image layer draws the PNG the extension sends', () => {
  const b = browser();
  const png = fs.readFileSync(path.resolve(dir, '..', '..', 'testdata', 'textures', 'src', 'rb.png'));
  b.send({
    type: 'source',
    file: 'a.tex.json',
    text: source([{ type: 'image', path: 'textures/src/rb.png', fit: 'stretch' }]),
    images: { 'textures/src/rb.png': png.toString('base64') },
  });
  assert.strictEqual(b.elements.error.hidden, true);
  assert.strictEqual(b.elements.size.textContent, '16x16');
});

test('the page names only elements the html has', () => {
  const used = new Set(Array.from(fs.readFileSync(path.join(dir, 'media', 'preview.js'), 'utf8').matchAll(/el\('([^']+)'\)/g), (m) => m[1]));
  const ids = new Set(Array.from(page.matchAll(/id="([^"]+)"/g), (m) => m[1]));
  for (const id of used) {
    assert.ok(ids.has(id), `preview.html has #${id}`);
  }
});

test('the html asks for exactly the values the extension fills in', () => {
  const placeholders = new Set(Array.from(page.matchAll(/{{(\w+)}}/g), (m) => m[1]));
  const extension = fs.readFileSync(path.join(dir, 'extension.js'), 'utf8');
  const provided = extension.slice(extension.indexOf('const values = {'), extension.indexOf('const page = '));
  for (const k of placeholders) {
    assert.ok(new RegExp(`\\b${k}\\s*[:,]`).test(provided), `the extension provides ${k}`);
  }
});
