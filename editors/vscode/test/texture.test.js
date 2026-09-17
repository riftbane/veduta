'use strict';
// The preview must draw what the engine draws. Every sample texture of the engine's
// testdata is compiled here in JavaScript and compared, pixel by pixel, with
// testdata/golden/texture_base_<name>.png, which the engine's own renderer writes
// (asset/texture, TestGoldenBase). A change on either side fails here.

const test = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const path = require('path');
const tex = require('../lib/texture');
const png = require('../lib/png');

const root = path.resolve(__dirname, '..', '..', '..');
const assets = path.join(root, 'testdata');
const goldenDir = path.join(assets, 'golden');
const haveEngine = fs.existsSync(path.join(assets, 'textures'));

// samples are the engine's sample textures, with the fit an image layer is given when the
// golden was rendered with another one (asset/texture/golden_test.go, baseSamples).
const samples = [
  ['solid', 'solid', ''],
  ['noise', 'noise', ''],
  ['stripes', 'stripes', ''],
  ['rect', 'rect', ''],
  ['circle', 'circle', ''],
  ['gradient', 'gradient', ''],
  ['checker', 'checker', ''],
  ['image', 'image', ''],
  ['image_cover', 'image', 'cover'],
  ['image_stretch', 'image', 'stretch'],
  ['example', 'example', ''],
  ['crate_wood', 'crate_wood', ''],
];

// draw compiles a sample texture the way the preview does: read the source, decode the
// images its layers name, render.
function draw(sample, fit) {
  const text = fs.readFileSync(path.join(assets, 'textures', sample + '.tex.json'), 'utf8');
  const { src, error } = tex.parse(text);
  assert.strictEqual(error, null, 'the source parses');
  if (fit) {
    src.layers[0].fit = fit;
  }
  const images = {};
  for (const p of tex.deps(src)) {
    try {
      images[p] = png.decode(fs.readFileSync(path.join(assets, p)));
    } catch (e) {
      images[p] = { error: String(e.message || e) };
    }
  }
  const out = tex.compile(src, images);
  assert.deepStrictEqual(out.errors, [], 'the source compiles');
  return out.image;
}

for (const [golden, sample, fit] of samples) {
  test(`draws ${golden} like the engine`, { skip: haveEngine ? false : 'the engine\'s testdata is not here' }, () => {
    const want = png.decode(fs.readFileSync(path.join(goldenDir, 'texture_base_' + golden + '.png')));
    const got = draw(sample, fit);
    assert.strictEqual(`${got.w}x${got.h}`, `${want.w}x${want.h}`);
    let worst = 0;
    let at = -1;
    for (let i = 0; i < want.data.length; i++) {
      const d = Math.abs(want.data[i] - got.data[i]);
      if (d > worst) {
        worst = d;
        at = i;
      }
    }
    const x = (at >> 2) % want.w;
    const y = (at >> 2 - 0) / want.w | 0;
    assert.strictEqual(worst, 0, worst === 0 ? '' : `channel ${at % 4} of pixel (${x}, ${y}) is off by ${worst}`);
  });
}

test('deps lists the images a source reads, valid or not', () => {
  assert.deepStrictEqual(tex.deps({ layers: [{ type: 'image', path: 'a.png' }, { type: 'solid' }, { type: 'image', path: 'a.png' }] }), ['a.png']);
  assert.deepStrictEqual(tex.deps({}), []);
  assert.deepStrictEqual(tex.deps(null), []);
});

test('image paths must stay in the assets directory', () => {
  assert.strictEqual(tex.badImagePath('textures/src/logo.png'), '');
  assert.ok(tex.badImagePath('../secret.png').includes('must not leave'));
  assert.ok(tex.badImagePath('/tmp/a.png').includes('relative'));
  assert.ok(tex.badImagePath('textures\\a.png').includes('forward slashes'));
  assert.ok(tex.badImagePath('a.jpg').includes('.png'));
  assert.ok(tex.badImagePath('').includes('required'));
});

test('a source the engine refuses is reported, not drawn', () => {
  const bad = (src) => tex.compile(src, {}).errors.map((e) => `${e.path}: ${e.msg}`);
  assert.deepStrictEqual(bad({ size: [8, 8], layers: [{ type: 'solid', color: '#fff000' }] })[0],
    ': missing "veduta" header (want "texture/1")');
  assert.ok(bad({ veduta: 'texture/1', layers: [{ type: 'solid', color: '#ffffff' }] })[0].startsWith('size: is required'));
  assert.ok(bad({ veduta: 'texture/1', size: [8, 8], layers: [{ type: 'solid' }] })[0].startsWith('layers[0].color: is required'));
  assert.ok(bad({ veduta: 'texture/1', size: [8, 8], layers: [{ type: 'rect', xy: [0, 0], size: [2, 2], color: '#fff', seed: 3 }] })
    .some((m) => m === 'layers[0].seed: not used by layer type rect'));
  assert.ok(bad({ veduta: 'texture/1', size: [0, 8], layers: [{ type: 'solid', color: '#ffffff' }] })[0].startsWith('size[0]:'));
  assert.ok(bad({ veduta: 'texture/1', size: [8, 8], layers: [] })[0].startsWith('layers: at least one'));
  assert.ok(bad({ veduta: 'texture/1', size: [8, 8], glow: true, layers: [{ type: 'solid', color: '#ffffff' }] })
    .some((m) => m === 'glow: unknown field'));
});

test('a missing image is an error, not a blank picture', () => {
  const out = tex.compile({ veduta: 'texture/1', size: [4, 4], layers: [{ type: 'image', path: 'nope.png' }] }, {});
  assert.strictEqual(out.image, null);
  assert.ok(out.errors[0].msg.includes('file not found'));
});

test('a valid source draws its pixels', () => {
  const out = tex.compile({ veduta: 'texture/1', size: [2, 1], layers: [{ type: 'solid', color: '#ff000080' }] }, {});
  assert.deepStrictEqual(out.errors, []);
  assert.deepStrictEqual(Array.from(out.image.data), [255, 0, 0, 128, 255, 0, 0, 128]);
});
