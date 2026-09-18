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
  ['frames', 'frames', ''],
  ['atlas_part', 'atlas_part', ''],
];

// draw compiles a sample texture the way the preview does: read the source, decode the
// images its layers name, render.
function draw(sample, fit) {
  const text = fs.readFileSync(path.join(assets, 'textures', sample + '.vtex'), 'utf8');
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

// sheetErrors mirror asset/texture/sheet_test.go: the same source, the same messages.
test('a sheet the engine refuses is reported as it reports it', () => {
  const cases = [
    [{ size: [8, 4], grid: [3, 2], layers: [{ type: 'solid', color: '#ffffff' }] },
      ['grid: 3 × 2 frames do not divide the 8 × 4 pixels evenly']],
    [{ size: [8, 4], grid: [0, 2], layers: [{ type: 'solid', color: '#ffffff' }] }, ['grid[0]: 0 out of range [1, 256]']],
    [{ size: [8, 4], grid: [4, 2], frames: [], layers: [{ type: 'solid', color: '#ffffff' }] }, ['frames: not allowed with grid']],
    [{ size: [2, 2], frames: [] }, ['frames: 0 frames, want 1 to 256']],
    [{ size: [2048, 2], frames: [1, 2, 3].map(() => ({ layers: [{ type: 'solid', color: '#ffffff' }] })) },
      ['frames: 3 frames 2048 pixels wide make a sheet of 6144 pixels, want at most 4096']],
    [{ size: [2, 2], frames: [{ layers: [] }] }, ['frames[0].layers: at least one layer is required']],
    [{ size: [2, 2], frames: [{ layers: [{ type: 'solid' }] }] }, ['frames[0].layers[0].color: is required']],
    [{ size: [8, 4], tiling: true, grid: [4, 2], layers: [{ type: 'solid', color: '#ffffff' }] }, ['tiling: a grid of frames cannot tile']],
    [{ size: [8, 4], layers: [{ type: 'solid', color: '#ffffff' }], clips: { walk: { frames: [0], fps: 1 } } }, ['clips: need frames']],
    [{ size: [8, 4], grid: [4, 2], layers: [{ type: 'solid', color: '#ffffff' }], clips: { Walk: { frames: [8, -1], fps: 0 }, hit: { frames: [], fps: 2000, next: 'nope' } }, play: 'run' }, [
      'clips.Walk: clip name "Walk" may only contain',
      'clips.Walk.fps: must be a positive number, got 0',
      'clips.Walk.frames[0]: frame 8 out of range [0, 7]',
      'clips.Walk.frames[1]: frame -1 out of range [0, 7]',
      'clips.hit.fps: 2000 out of range (0, 1000]',
      'clips.hit.frames: at least one frame is required',
      'clips.hit.next: only allowed when loop is false',
      'play: no clip "run"']],
    [{ size: [8, 4], grid: [4, 2], layers: [{ type: 'solid', color: '#ffffff' }], clips: { hit: { frames: [0], fps: 2, loop: false, next: 'nope' } } },
      ['clips.hit.next: no clip "nope"']],
    [{ size: [8, 4], grid: [4, 2], layers: [{ type: 'solid', color: '#ffffff' }], clips: { hit: { frames: [0] } } }, ['clips.hit.fps: is required']],
    [{ size: [15, 16], layers: [{ type: 'solid', color: '#ffffff' }], edge: { priority: 0, width: 9, roughness: 2 } }, [
      'edge: a frame\'s width and height must be even to have an edge, got 15 × 16',
      'edge.priority: 0 out of range [1, 1000]',
      'edge.roughness: 2 out of range [0, 1]',
      'edge.width: 9 out of range [0, 7.5]']],
    [{ size: [16, 16], layers: [{ type: 'solid', color: '#ffffff' }], edge: { width: 0 } }, ['edge.priority: is required', 'edge.width: must be above 0']],
  ];
  for (const [body, wants] of cases) {
    const got = tex.compile(Object.assign({ veduta: 'texture/1' }, body), {}).errors.map((e) => `${e.path}: ${e.msg}`);
    for (const w of wants) {
      assert.ok(got.some((g) => g.startsWith(w)), `${JSON.stringify(body)}\nreports:\n${got.join('\n')}\nwant ${w}`);
    }
  }
});

test('an image rect must lie inside the image', () => {
  const rb = png.decode(fs.readFileSync(path.join(assets, 'textures', 'src', 'rb.png')));
  const one = (rect) => tex.compile({ veduta: 'texture/1', size: [1, 1], mipmaps: false, layers: [{ type: 'image', path: 'textures/src/rb.png', rect }] }, { 'textures/src/rb.png': rb });
  assert.deepStrictEqual(Array.from(one([1, 0, 1, 1]).image.data), [0, 0, 255, 255], 'the right pixel is blue');
  assert.deepStrictEqual(Array.from(one([0, 0, 1, 1]).image.data), [255, 0, 0, 255], 'the left pixel is red');
  for (const [rect, want] of [[[1, 0, 2, 1], 'is not inside the 2 × 1 image'], [[0, 0, 0, 1], 'is not inside'], [[0, 0, 1], 'must be [x, y, width, height]']]) {
    const e = one(rect).errors.map((x) => `${x.path}: ${x.msg}`).join('\n');
    assert.ok(e.includes('layers[0].rect: ') && e.includes(want), `${rect}: ${e}`);
  }
});

test('a sheet has its grid, clips and play, and no mipmaps unless it asks', () => {
  const src = { veduta: 'texture/1', size: [8, 4], grid: [4, 2], layers: [{ type: 'checker', cells: 4, colors: ['#ff0000', '#0000ff'] }],
    clips: { walk: { frames: [0, 1, 2, 3], fps: 8 }, hit: { frames: [5, 6], fps: 12, loop: false, next: 'walk' } }, play: 'walk' };
  const { spec, errors } = tex.validate(src, {});
  assert.deepStrictEqual(errors, []);
  assert.deepStrictEqual(spec.grid, [4, 2]);
  assert.strictEqual(spec.mipmaps, false);
  assert.strictEqual(spec.play, 'walk');
  assert.deepStrictEqual(spec.clips.map((c) => c.name), ['hit', 'walk']);
  assert.strictEqual(tex.validate(Object.assign({}, src, { mipmaps: true }), {}).spec.mipmaps, true);
  // 8 fps at 20 ticks a second: steps at ticks 0, 3, 5, 8 (asset/texture TestClipFrame).
  const walk = { frames: [4, 5, 6], fps: 8, loop: true };
  assert.deepStrictEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9].map((t) => tex.clipFrame(walk, t, 20)), [4, 4, 4, 5, 5, 6, 6, 6, 4, 4]);
  const hit = { frames: [1, 2], fps: 10, loop: false };
  assert.deepStrictEqual([0, 1, 2, 3, 4].map((t) => tex.clipFrame(hit, t, 20)), [1, 1, 2, 2, 2]);
  const e = tex.validate({ veduta: 'texture/1', size: [32, 16], grid: [2, 1], layers: [{ type: 'solid', color: '#ffffff' }], edge: { priority: 5, seed: 3 } }, {}).spec.edge;
  assert.deepStrictEqual(e, { priority: 5, width: 4, roughness: 0.5, seed: 3 }, 'the width is a quarter of the 16 × 16 frame');
});

test('values are written the way the engine writes them', () => {
  const g = (v) => tex.goFloat(Math.fround(v), true);
  assert.deepStrictEqual([0, 7.5, 2000, 1e6, -1e6, 0.1, 0.0001, 0.00001, 123456, 1234567, 3.4e38, 1.5e-7, 0.3, 2.0000002].map(g),
    ['0', '7.5', '2000', '1e+06', '-1e+06', '0.1', '0.0001', '1e-05', '123456', '1.234567e+06', '3.4e+38', '1.5e-07', '0.3', '2.0000002']);
  assert.strictEqual(tex.goQuote('a\x01"\\é\u00ad\u2028'), '"a\\x01\\"\\\\é\\u00ad\\u2028"');
  assert.strictEqual(tex.goQuote('\x7f'), '"\\x7f"');
});
