'use strict';
// The map editor must read and draw a map as the engine does. testdata/tilemap (the farm
// the engine's own tilemap tests use) is loaded and drawn here in JavaScript and compared,
// pixel by pixel, with the goldens the engine writes (tilemap TestPicture, TestEdgeAtlas);
// the checks are compared with asset/map.go's messages.

const test = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const path = require('path');
const tex = require('../lib/texture');
const tm = require('../lib/tilemap');
const png = require('../lib/png');

const root = path.resolve(__dirname, '..', '..', '..');
const dir = path.join(root, 'testdata', 'tilemap');
const goldenDir = path.join(root, 'testdata', 'golden');
const haveEngine = fs.existsSync(dir);
const skip = haveEngine ? false : 'the engine\'s testdata is not here';

// library compiles the farm's textures as the editor does (tilemap_test.go testLibrary).
function library() {
  const textures = {};
  for (const name of ['grass', 'water', 'sand', 'dirt']) {
    const out = tex.compile(tex.parse(fs.readFileSync(path.join(dir, name + '.vtex'), 'utf8')).src, {});
    assert.deepStrictEqual(out.errors, [], name);
    textures[name] = tm.textureOf(name, out);
  }
  return { textures, materials: {} };
}

function farm() {
  const out = tm.load(fs.readFileSync(path.join(dir, 'farm.vmap'), 'utf8'), 'farm.vmap');
  assert.deepStrictEqual(out.errors, []);
  return out.map;
}

// same compares two RGBA pictures and names the first pixel that differs.
function same(got, file) {
  const want = png.decode(fs.readFileSync(path.join(goldenDir, file)));
  assert.strictEqual(`${got.w}x${got.h}`, `${want.w}x${want.h}`);
  for (let i = 0; i < want.data.length; i++) {
    if (want.data[i] !== got.data[i]) {
      const p = i >> 2;
      assert.fail(`${file}: channel ${i % 4} of pixel (${p % want.w}, ${Math.floor(p / want.w)}) is ${got.data[i]}, want ${want.data[i]}`);
    }
  }
}

test('the edge atlas of the water is the engine\'s', { skip }, () => {
  const water = library().textures.water;
  const a = tm.edgeAtlas(water);
  assert.deepStrictEqual(a.grid, [1, 2], 'a column of the 2 frames');
  assert.strictEqual(a.play, 'flow');
  same(a, 'tilemap_edge_atlas.png');
});

test('the farm is drawn as the engine draws it', { skip }, () => {
  const lib = library();
  const m = farm();
  assert.deepStrictEqual(tm.cellSize(tm.prepare(m, lib)), [16, 16]);
  same(tm.picture(m, lib, 0, 20), 'tilemap_farm_picture.png');
  same(tm.picture(m, lib, 10, 20), 'tilemap_farm_picture_tick10.png');
});

test('drawing some cells again gives the whole picture', { skip }, () => {
  const lib = library();
  const m = farm();
  const p = new tm.Picture(m, lib);
  p.drawAll();
  tm.brush(m, 0, 5, 5, 3, 2);
  p.drawRect(3, 3, 7, 7);
  const whole = tm.picture(m, lib, 0, 20);
  assert.ok(Buffer.from(p.data).equals(Buffer.from(whole.data)));
  // The water moves at tick 10: only the cells by the water are drawn again.
  const changed = p.setTick(10, 20);
  assert.deepStrictEqual(changed, [1]);
  for (let y = 0; y < m.h; y++) {
    for (let x = 0; x < m.w; x++) {
      if (p.touches(x, y, changed)) {
        p.drawRect(x, y, x, y);
      }
    }
  }
  assert.ok(Buffer.from(p.data).equals(Buffer.from(tm.picture(m, lib, 10, 20).data)));
});

// cliffs adds the autotile cliffs (tilemap_test.go cliffsLibrary) to the library.
function cliffs() {
  const lib = library();
  const images = { 'cliff.png': png.decode(fs.readFileSync(path.join(dir, 'cliff.png'))) };
  const out = tex.compile(tex.parse(fs.readFileSync(path.join(dir, 'cliff.vtex'), 'utf8')).src, images);
  assert.deepStrictEqual(out.errors, []);
  assert.strictEqual(out.spec.autotile, true);
  lib.textures.cliff = tm.textureOf('cliff', out);
  const m = tm.load(fs.readFileSync(path.join(dir, 'cliffs.vmap'), 'utf8'), 'cliffs.vmap');
  assert.deepStrictEqual(m.errors, []);
  return { lib, m: m.map };
}

test('autotiles are drawn as the engine draws them', { skip }, () => {
  const { lib, m } = cliffs();
  assert.deepStrictEqual(tm.cellSize(tm.prepare(m, lib)), [16, 16]);
  same(tm.picture(m, lib, 0, 20), 'tilemap_cliffs_picture.png');
  same(tm.picture(m, lib, 10, 20), 'tilemap_cliffs_picture_tick10.png');
  // Painting redraws the cells around: an autotile cell looks at its neighbours.
  const p = new tm.Picture(m, lib);
  p.drawAll();
  tm.paint(m, 1, 6, 4, 2);
  p.drawRect(5, 3, 7, 5);
  assert.ok(Buffer.from(p.data).equals(Buffer.from(tm.picture(m, lib, 0, 20).data)));
});

test('autoPick', () => {
  assert.deepStrictEqual(tm.autoPick(255), { tiles: [7, 7, 7, 7], whole: true });
  assert.deepStrictEqual(tm.autoPick(0), { tiles: [0, 2, 12, 14], whole: false });
  assert.deepStrictEqual(tm.autoPick(255 & ~2), { tiles: [15, 15, 15, 15], whole: true }, 'only the NE corner missing: the lake\'s SW tile');
});

test('cellEdges', { skip }, () => {
  const lib = library();
  const m = farm();
  const draws = tm.prepare(m, lib);
  const at = (l, x, y) => {
    const out = [];
    tm.cellEdges(m, draws, l, x, y, (u, s, q) => out.push([m.terrains[u - 1].name, s, q]));
    return out;
  };
  // Grass at (2, 1): sand below it, and below-right; the water is further.
  assert.deepStrictEqual(at(0, 2, 1), [['sand', 0, 2], ['sand', 0, 3]]);
  // A lone water cell (16, 6) is a border in every quarter of the grass around it.
  assert.deepStrictEqual(at(0, 17, 7), [['water', 3, 0]], 'the corner by the diagonal');
  // An empty cell of the paths layer under a path.
  assert.deepStrictEqual(at(1, 10, 0).map((e) => e[0]), ['dirt', 'dirt']);
  assert.deepStrictEqual(at(0, 3, 4), [], 'water is the highest: nothing spills on it');
});

test('the checks report what asset/map.go reports', () => {
  const bad = (src, file = 'm.vmap') => tm.load(typeof src === 'string' ? src : JSON.stringify(src), file).errors.map(tm.describe);
  const base = () => ({ veduta: 'map/1', size: [3, 2], terrains: [{ key: '.', name: 'grass', texture: 'grass' }], layers: [{ name: 'ground', rows: ['...', '...'] }] });
  assert.deepStrictEqual(bad(base()), []);
  const cases = [
    [(m) => { m.size = [0, 2000]; }, ['size[0]: 0 out of range [1, 1024]', 'size[1]: 2000 out of range [1, 1024]',
      'layers[0].rows: 2 rows, want 1 (the map\'s size)', 'layers[0].rows[0]: 3 characters, want 1 (the map\'s size; a space is an empty cell)']],
    [(m) => { m.size = [3]; m.layers[0].rows = ['.']; }, ['size: want [columns, rows], got 1 numbers']],
    [(m) => { delete m.size; m.layers[0].rows = ['.']; }, ['size: is required ([columns, rows])']],
    [(m) => { m.tile = 0; }, ['tile: must be a positive number, got 0']],
    [(m) => { m.origin = [1, 2]; }, ['origin: want 3 numbers, got 2']],
    [(m) => { m.terrains = []; m.layers[0].rows = ['   ', '   ']; }, ['terrains: at least one terrain is required']],
    [(m) => { m.terrains.push({ key: '.', name: 'Grass', texture: 'x', material: 'y', tags: ['a', 'a', 'B'] }); }, [
      'terrains[1].key: duplicate key "." (first used by terrains[0])',
      'terrains[1].name: name "Grass" may only contain a-z, 0-9, \'_\' and \'-\' (not first)',
      'terrains[1].material: not allowed with texture: a terrain is drawn with one or the other',
      'terrains[1].tags[1]: duplicate "a"',
      'terrains[1].tags[2]: name "B" may only contain a-z, 0-9, \'_\' and \'-\' (not first)']],
    [(m) => { m.terrains.push({ key: ' ', name: 'grass' }); }, [
      'terrains[1].key: must be one printable ASCII character but space, \'"\' and \'\\\', got " "',
      'terrains[1].name: duplicate terrain name "grass" (first used by terrains[0])',
      'terrains[1]: needs a texture or a material']],
    [(m) => { m.terrains[0].key = 'ab'; }, ['terrains[0].key: must be one printable ASCII character but space, \'"\' and \'\\\', got "ab"']],
    [(m) => { m.layers[0].rows = ['...', '.x', '...']; m.layers[0].layer = 2000; m.layers[0].z = 2e6; }, [
      'layers[0].layer: 2000 out of range [-1000, 1000]',
      'layers[0].z: 2e+06 out of range [-1e+06, 1e+06]',
      'layers[0].rows: 3 rows, want 2 (the map\'s size)',
      'layers[0].rows[1]: 2 characters, want 3 (the map\'s size; a space is an empty cell)']],
    [(m) => { m.layers[0].rows = ['.x.', '..é']; }, [
      'layers[0].rows[0]: character "x" at column 1 is not a terrain\'s key (a space is an empty cell)',
      'layers[0].rows[1]: 4 characters, want 3 (the map\'s size; a space is an empty cell)']],
    [(m) => { m.layers.push({ name: 'ground', rows: ['   ', '   '] }); }, ['layers[1].name: duplicate layer name "ground" (first used by layers[0])']],
    [(m) => {
      m.objects = [{ name: 'a', at: [3, 0] }, { name: 'a', at: [1, 1], size: [3, 1], tags: ['x'], props: { 'a.b': 1, ok: null, n: 'x' } }, { name: 'c' }];
    }, [
      'objects[0].at: cell [3 0] is outside the 3 × 2 map',
      'objects[1].name: duplicate object name "a" (first used by objects[0])',
      'objects[1].size: [3 1] cells from [1 1] leave the 3 × 2 map (each at least 1)',
      'objects[1].props.a.b: a property name is 1-64 characters without spaces or \'.\', got "a.b"',
      'objects[1].props.ok: must be a string, a number or a boolean',
      'objects[2].at: is required ([column, row] of its top-left cell)']],
  ];
  for (const [edit, wants] of cases) {
    const m = base();
    edit(m);
    const got = bad(m).map((s) => s.replace(/^\d+:\d+: /, ''));
    assert.deepStrictEqual(got.filter((g) => wants.includes(g)), wants, got.join('\n'));
  }
  // Decoding: the first problem, located.
  assert.deepStrictEqual(bad('{\n  "veduta": "map/1",\n  "size": [3, 2.5]\n}'), ['3:15: size[1]: cannot use JSON number 2.5 as int']);
  assert.deepStrictEqual(bad('{"veduta": "map/1", "size": [3, 2], "glow": 1}'), ['1:37: glow: unknown field']);
  assert.deepStrictEqual(bad('{"veduta": "map/1", "terrains": [{"key": 1}]}'), ['1:42: terrains[0].key: cannot use JSON number as string']);
  assert.deepStrictEqual(bad('{"veduta": "map/1", "layers": {}}'), ['1:31: layers: cannot use JSON object as []asset.MapLayerSource']);
  assert.deepStrictEqual(bad('{"veduta": "map/1", "size": [1, 1], "size": [2, 2]}'), ['1:37: duplicate key "size"']);
  assert.deepStrictEqual(bad('{"veduta": "texture/1"}'), ['1:12: veduta: header is "texture/1", want "map/1"']);
  assert.deepStrictEqual(bad('{"size": [1, 1]}'), ['1:1: missing "veduta" header (want "map/1")']);
  assert.deepStrictEqual(bad('[1]'), ['1:1: source must be a JSON object']);
  assert.deepStrictEqual(bad('{\n  "veduta": "map/1",\n  "size": [1 1]\n}'), ['3:14: invalid JSON: invalid character \'1\' after array element']);
  assert.deepStrictEqual(bad('{"veduta": "map/1"'), ['1:19: unexpected end of JSON input']);
  assert.deepStrictEqual(bad(base(), 'Farm.vmap'), ['file name: map name "Farm" may only contain a-z, 0-9, \'_\' and \'-\' (not first)']);
  // An error is located at the value it names.
  const text = JSON.stringify(Object.assign(base(), { tile: -1 }), null, 2);
  const e = tm.load(text, 'm.vmap').errors[0];
  assert.strictEqual(text.split('\n')[e.line - 1].slice(e.col - 1), '"tile": -1'.slice(8));
});

test('the new map template is written as it is', () => {
  const file = path.join(root, 'template', 'new', 'map.vmap');
  if (!fs.existsSync(file)) {
    return;
  }
  const text = fs.readFileSync(file, 'utf8');
  const out = tm.load(text, 'map.vmap');
  assert.deepStrictEqual(out.errors, []);
  assert.strictEqual(tm.format(out.map), text);
});

test('the example of docs/map.md and the farm round-trip', { skip }, () => {
  const doc = fs.readFileSync(path.join(root, 'docs', 'map.md'), 'utf8');
  const example = /```json\n(\{\n  "veduta": "map\/1"[\s\S]*?)```/.exec(doc)[1];
  for (const text of [example, fs.readFileSync(path.join(dir, 'farm.vmap'), 'utf8')]) {
    const a = tm.load(text, 'farm.vmap');
    assert.deepStrictEqual(a.errors, []);
    const once = tm.format(a.map);
    const b = tm.load(once, 'farm.vmap');
    assert.deepStrictEqual(b.errors, []);
    assert.strictEqual(tm.format(b.map), once, 'formatting is stable');
    assert.deepStrictEqual(JSON.parse(once), JSON.parse(text), 'the same map');
  }
  const once = tm.format(tm.load(example, 'farm.vmap').map);
  assert.ok(once.includes('    { "key": "s", "name": "sand", "texture": "sand" },\n'), 'a terrain per line');
  assert.ok(once.includes('    { "name": "paths", "rows": [\n      "        d   ",\n'), 'a row per line');
  assert.ok(once.includes('    { "name": "house_door", "at": [8, 0], "tags": ["door"], "props": { "to": "house", "x": 3, "y": 7 } },\n'));
});

test('cell operations', () => {
  const m = tm.load(JSON.stringify({ veduta: 'map/1', size: [5, 4], terrains: [{ key: '.', name: 'a', texture: 'a' }, { key: '~', name: 'b', texture: 'b' }],
    layers: [{ name: 'g', rows: ['.....', '.~~..', '.~...', '.....'] }, { name: 'p', z: 2, layer: 1, rows: ['     ', '     ', '     ', '     '] }],
    objects: [{ name: 'door', at: [4, 3] }, { name: 'field', at: [1, 1], size: [3, 2], props: { k: true } }] }), 'm.vmap').map;
  const show = (l) => tm.rows(m, l).join('|');
  assert.deepStrictEqual(tm.brush(m, 1, 0, 0, 2, 1), [[0, 0], [1, 0], [0, 1], [1, 1]]);
  assert.strictEqual(show(1), '..   |..   |     |     ');
  assert.strictEqual(tm.brush(m, 1, 2, 2, 3, 2).length, 9);
  assert.strictEqual(show(1), '..   |.~~~ | ~~~ | ~~~ ');
  assert.strictEqual(tm.paint(m, 1, 9, 9, 1).length, 0, 'off the map');
  assert.strictEqual(tm.rect(m, 1, 4, 3, 3, 2, 0).length, 2);
  assert.strictEqual(show(1), '..   |.~~~ | ~~  | ~~  ');
  // The fill stays on its layer and follows sides, not corners.
  assert.strictEqual(tm.flood(m, 0, 1, 1, 1).length, 3);
  assert.strictEqual(show(0), '.....|.....|.....|.....');
  assert.strictEqual(tm.flood(m, 0, 0, 0, 2).length, 20);
  assert.strictEqual(tm.flood(m, 0, 0, 0, 2).length, 0, 'same terrain: nothing');
  // Resize keeps the top-left, empties new cells, cuts objects and drops those off it.
  assert.deepStrictEqual(tm.resize(m, 3, 5), ['door']);
  assert.strictEqual(show(0), '~~~|~~~|~~~|~~~|   ');
  assert.deepStrictEqual(m.objects.map((o) => [o.name, o.size]), [['field', [2, 2]]]);
  const text = tm.format(m);
  assert.deepStrictEqual(tm.load(text, 'm.vmap').errors, []);
  assert.ok(text.includes('    { "name": "p", "z": 2, "layer": 1, "rows": [\n'));
  assert.ok(text.includes('"props": { "k": true }'));
  // Terrains: added with a free name and key, removed with their cells.
  assert.strictEqual(tm.addTerrain(m, 'a'), 2);
  assert.deepStrictEqual([m.terrains[2].name, m.terrains[2].key], ['a_2', '#']);
  assert.strictEqual(tm.uses(m, 1), 12 + 6);
  tm.paint(m, 0, 0, 4, 3);
  tm.removeTerrain(m, 1);
  assert.strictEqual(show(0), '   |   |   |   |#  ');
  assert.deepStrictEqual(tm.load(tm.format(m), 'm.vmap').errors, []);
  assert.strictEqual(new Set(tm.KEYS).size, tm.MaxMapTerrains);
});
