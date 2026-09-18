'use strict';
// The tile editor's model: what it reads from a .vtex and its PNG, what it writes back
// (checked with lib/texture.js, the compiler's checks), the drawing operations and the
// autotile preview (the map's own choice of tiles).

const test = require('node:test');
const assert = require('node:assert');
const zlib = require('zlib');
const fs = require('fs');
const path = require('path');
const ts = require('../lib/tileset');
const tex = require('../lib/texture');
const png = require('../lib/png');

const deflate = (raw) => zlib.deflateSync(raw);

// compiled writes a model as the editor saves it and compiles it as the engine would.
function compiled(model, src, pngPath = 'textures/t.png') {
  const out = ts.write(model, src, pngPath);
  const img = ts.sheet(model);
  const back = png.decode(png.encode(img.w, img.h, img.data, deflate));
  const text = ts.format(out);
  const c = tex.compile(tex.parse(text).src, { [pngPath]: back });
  return { out, text, c, back };
}

test('a PNG written is the pixels read back', () => {
  const w = 7;
  const h = 5;
  const data = new Uint8ClampedArray(w * h * 4).map((_, i) => (i * 53) % 256);
  const back = png.decode(png.encode(w, h, data, deflate));
  assert.deepStrictEqual([back.w, back.h], [w, h]);
  assert.ok(Buffer.from(back.data).equals(Buffer.from(data)));
});

test('new tiles of every kind compile', () => {
  for (const kind of ts.KINDS) {
    const m = ts.blank(kind, 16);
    const src = ts.newSource(kind, m, 'textures/t.png');
    const img = ts.sheet(m);
    const c = tex.compile(tex.parse(ts.format(src)).src, { 'textures/t.png': png.decode(png.encode(img.w, img.h, img.data, deflate)) });
    assert.deepStrictEqual(c.errors, [], kind);
    assert.strictEqual(c.spec.autotile, kind === 'autotile', kind);
  }
  assert.strictEqual(ts.newSource('tile', ts.blank('tile', 16), 'x.png').tiling, true, 'a plain tile repeats');
  assert.deepStrictEqual(ts.newSource('animated', ts.blank('animated', 16), 'x.png').grid, [2, 1]);
  assert.strictEqual(ts.checkSize('autotile', 15), 'an autotile\'s tiles have an even size (a map cuts them in quarters)');
  assert.strictEqual(ts.checkSize('tile', 15), '');
});

test('frames read and written: a grid of any shape, then side by side', () => {
  // A 2 × 2 grid of 2 × 2 frames, each frame filled with its number.
  const W = 4;
  const H = 4;
  const data = new Uint8ClampedArray(W * H * 4);
  for (let y = 0; y < H; y++) {
    for (let x = 0; x < W; x++) {
      const f = (y >> 1) * 2 + (x >> 1);
      data.set([f, f, f, 255], (y * W + x) * 4);
    }
  }
  const src = {
    veduta: 'texture/1', size: [4, 4], layers: [{ type: 'image', path: 'textures/h.png' }], grid: [2, 2],
    clips: { walk: { frames: [0, 1, 2, 3], fps: 8 }, hit: { frames: [3, 2], fps: 12, loop: false, next: 'walk' } }, play: 'walk',
  };
  const m = ts.read(src, { w: W, h: H, data });
  assert.deepStrictEqual([m.w, m.h, m.frames.length, m.fps], [2, 2, 4, 8]);
  assert.deepStrictEqual(m.frames.map((f) => f[0]), [0, 1, 2, 3]);
  m.frames.splice(3, 1); // a frame deleted
  const { out, c } = compiled(m, src, 'textures/h.png');
  assert.deepStrictEqual(c.errors, []);
  assert.deepStrictEqual(out.size, [6, 2]);
  assert.deepStrictEqual(out.grid, [3, 1]);
  assert.deepStrictEqual(out.clips.walk.frames, [0, 1, 2], 'the play clip runs every frame');
  assert.deepStrictEqual(out.clips.hit, { frames: [2], fps: 12, loop: false, next: 'walk' }, 'other clips lose the frames no longer there');
  // One frame left: no grid, no play clip.
  m.frames.splice(1, 2);
  const one = compiled(m, out, 'textures/h.png');
  assert.deepStrictEqual(one.c.errors, []);
  assert.strictEqual(one.out.grid, undefined);
  assert.strictEqual(one.out.play, undefined);
  assert.strictEqual(one.out.clips, undefined, 'hit showed frame 2 only');
});

test('what the editor does not draw is refused or kept', () => {
  assert.match(ts.read({ veduta: 'texture/1', size: [4, 4], layers: [{ type: 'solid', color: '#ffffff' }] }, null).error, /layer program/);
  assert.match(ts.read({ veduta: 'texture/1', size: [4, 4], layers: [{ type: 'image', path: 'a.png', rect: [0, 0, 2, 2] }] }, null).error, /"rect"/);
  assert.match(ts.read({ veduta: 'texture/1', size: [4, 4], layers: [{ type: 'image', path: 'a.png' }] }, { w: 8, h: 4, data: new Uint8ClampedArray(128) }).error,
    /the PNG is 8 × 4 pixels, the texture's size 4 × 4/);
  const src = { veduta: 'texture/1', size: [16, 16], mipmaps: false, layers: [{ type: 'image', path: 'textures/g.png', blend: 'normal' }], edge: { priority: 3 } };
  const m = ts.read(src, null);
  const { out, c } = compiled(m, src, 'textures/g.png');
  assert.deepStrictEqual(c.errors, []);
  assert.deepStrictEqual(out.edge, { priority: 3 });
  assert.strictEqual(out.mipmaps, false);
  assert.deepStrictEqual(out.layers, [{ type: 'image', path: 'textures/g.png', blend: 'normal' }]);
  // An autotile drops the edge it cannot have.
  m.autotile = true;
  m.w = 96;
  m.h = 48;
  m.frames = [new Uint8ClampedArray(96 * 48 * 4)];
  const a = compiled(m, src, 'textures/g.png');
  assert.deepStrictEqual(a.c.errors, []);
  assert.strictEqual(a.out.edge, undefined);
});

test('the file is written the one way', () => {
  const m = ts.blank('animated', 8);
  assert.strictEqual(ts.format(ts.newSource('animated', m, 'textures/water.png')), [
    '{',
    '  "veduta": "texture/1",',
    '  "size": [16, 8],',
    '  "layers": [',
    '    { "type": "image", "path": "textures/water.png" }',
    '  ],',
    '  "grid": [2, 1],',
    '  "clips": { "loop": { "frames": [0, 1], "fps": 6 } },',
    '  "play": "loop"',
    '}',
    '',
  ].join('\n'));
});

test('drawing', () => {
  const w = 6;
  const h = 4;
  const px = new Uint8ClampedArray(w * h * 4);
  const red = [255, 0, 0, 255];
  assert.deepStrictEqual(ts.linePoints(0, 0, 3, 1), [[0, 0], [1, 0], [2, 1], [3, 1]]);
  assert.strictEqual(ts.rectPoints(0, 0, 2, 2, false).length, 8);
  assert.strictEqual(ts.rectPoints(2, 2, 0, 0, true).length, 9);
  for (const [x, y] of ts.rectPoints(0, 0, 3, 3, false)) {
    ts.setPixel(px, w, h, x, y, red);
  }
  // Inside the square: 2 × 2 pixels; outside it, the rest of the frame.
  assert.strictEqual(ts.flood(px, w, h, 1, 1, [0, 255, 0, 255]), 4);
  assert.strictEqual(ts.flood(px, w, h, 5, 0, [0, 0, 255, 255]), 8);
  assert.deepStrictEqual(ts.getPixel(px, w, 5, 3), [0, 0, 255, 255]);
  assert.strictEqual(ts.flood(px, w, h, 5, 0, [0, 0, 255, 255]), 0, 'the same color: nothing');
  const b = ts.copyRect(px, w, 0, 0, 2, 1);
  assert.deepStrictEqual(Array.from(ts.flipBlock(b, true).data.slice(0, 4)), [255, 0, 0, 255]);
  const r = ts.rotateBlock(ts.copyRect(px, w, 0, 0, 3, 2));
  assert.deepStrictEqual([r.w, r.h], [2, 3]);
  // The top left pixel goes to the top right.
  assert.deepStrictEqual(Array.from(r.data.slice(4, 8)), [255, 0, 0, 255]);
  assert.deepStrictEqual(Array.from(r.data.slice(0, 4)), [255, 0, 0, 255], 'the pixel below it, red too, comes top left');
  ts.pasteRect(px, w, h, { w: 1, h: 1, data: new Uint8ClampedArray([0, 0, 0, 0]) }, 5, 3, true);
  assert.deepStrictEqual(ts.getPixel(px, w, 5, 3), [0, 0, 255, 255], 'a transparent pixel over: nothing');
  ts.pasteRect(px, w, h, { w: 2, h: 1, data: new Uint8ClampedArray([1, 2, 3, 4, 5, 6, 7, 8]) }, 5, 3, false);
  assert.deepStrictEqual(ts.getPixel(px, w, 5, 3), [1, 2, 3, 4], 'cut at the side');
  assert.deepStrictEqual(ts.colors(px)[0], [255, 0, 0, 255], 'red is the most used');
  assert.deepStrictEqual(ts.parseHex('#abc'), [170, 187, 204, 255]);
  assert.deepStrictEqual(ts.parseHex('11223344'), [17, 34, 51, 68]);
  assert.strictEqual(ts.parseHex('#12345'), null);
  assert.strictEqual(ts.hex([17, 34, 51, 255]), '#112233');
});

test('the autotile preview is a map painted with it', () => {
  const root = path.resolve(__dirname, '..', '..', '..');
  const file = path.join(root, 'testdata', 'tilemap', 'cliff.png');
  if (!fs.existsSync(file)) {
    return;
  }
  const img = png.decode(fs.readFileSync(file));
  const m = ts.read({ veduta: 'texture/1', size: [img.w, img.h], layers: [{ type: 'image', path: 'cliff.png' }], grid: [2, 1], autotile: true }, img);
  const p = ts.autoPreview(m.frames[0], 16, ['....', '.##.', '.##.', '....']);
  // A 2 × 2 island (off the map counts as the terrain: the empty cells keep it an island):
  // its four corners, whole.
  assert.deepStrictEqual([p.w, p.h], [64, 64]);
  const tile = (t, x, y) => Array.from(m.frames[0].slice(((Math.floor(t / 6) * 16 + y) * 96 + (t % 6) * 16 + x) * 4, ((Math.floor(t / 6) * 16 + y) * 96 + (t % 6) * 16 + x) * 4 + 4));
  const at = (x, y) => Array.from(p.data.slice((y * 64 + x) * 4, (y * 64 + x) * 4 + 4));
  assert.deepStrictEqual(at(16 + 3, 16 + 3), tile(0, 3, 3), 'the island\'s NW tile, top left');
  assert.deepStrictEqual(at(32 + 12, 32 + 12), tile(14, 12, 12), 'its SE tile, bottom right');
  assert.deepStrictEqual(at(1, 1), [0, 0, 0, 0], 'an empty cell');
  assert.strictEqual(ts.tileAt(16, 70, 20).name, 'lake middle (not used)');
  // The lake from the island: its top side is the island's bottom side.
  const px = m.frames[0].slice();
  ts.lakeFromIsland(px, 16);
  assert.deepStrictEqual(Array.from(px.slice((0 * 96 + 64) * 4, (0 * 96 + 64) * 4 + 4)), tile(13, 0, 0));
});
