'use strict';
const test = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const tree = require('../lib/tree');

// game writes a project: files with content, folders ending in / made empty.
function game(files) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'veduta-tree-'));
  for (const [f, data] of Object.entries(files)) {
    const p = path.join(root, ...f.split('/'));
    if (f.endsWith('/')) {
      fs.mkdirSync(p, { recursive: true });
      continue;
    }
    fs.mkdirSync(path.dirname(p), { recursive: true });
    fs.writeFileSync(p, data);
  }
  return root;
}

// show prints a tree one node per line, indented, with its context value and description.
function show(nodes, depth = 0) {
  return nodes.flatMap((n) => [
    '  '.repeat(depth) + n.label + ' [' + tree.contextValue(n) + ']' + (n.description ? ' ' + n.description : ''),
    ...show(n.children, depth + 1),
  ]);
}

test('build', () => {
  const root = game({
    'veduta.json': JSON.stringify({ veduta: 'project/1', name: 'g', script: 'main.lua', icon: 'icon.png' }),
    'card.json': '{}',
    'icon.png': '',
    'README.md': '',
    'CHANGELOG.md': '',
    'CLAUDE.md': '',
    '.gitignore': '',
    '.vscode/settings.json': '{}',
    '.veduta/lua/veduta.d.lua': '',
    'main.lua': '',
    'enemies/slime.lua': '',
    'enemies/boss/king.lua': '',
    'docs/notes.md': '',
    'levels/': '',
    'out/saves/slot.json': '',
    'out/x.lua': '',
    'assets/scenes/main.vscene': '{}',
    'assets/scenes/levels/one.vscene': '{}',
    'assets/scenes/levels/two.vscene': '{}',
    'assets/scenes/stray.json': '{}',
    'assets/prefabs/nature/': '',
    'assets/models/quad.vmodel': '{}',
    'assets/models/.vmodel': '{}',
    'assets/materials/sprite.vmat': '{}',
    'assets/textures/wood.vtex': '{}',
    'assets/textures/wood.png': '',
    'assets/art/hero.png': '',
    'assets/.cooked/models/quad.vda': '',
    'tests/scenarios/start.vscenario': '{}',
    'tests/scenarios/old/': '',
    'tests/golden/start.hash': '',
  });
  const p = tree.project(root);
  const nodes = tree.build(tree.scan(root, p), p);
  assert.deepStrictEqual(show(nodes), [
    'Game [section.project]',
    '  veduta.json [file.project] settings',
    '  card.json [file.project] on the console',
    '  README.md [file.project]',
    '  CHANGELOG.md [file.project] releases',
    '  icon.png [file.project] icon',
    'Scripts [section.script]',
    '  enemies [folder.script]',
    '    boss [folder.script]',
    '      king.lua [file.script]',
    '    slime.lua [file.script]',
    '  levels [folder.script]',
    '  main.lua [file.script] start',
    'Scenes [section.scene]',
    '  levels [folder.scene]',
    '    one.vscene [file.scene]',
    '    two.vscene [file.scene]',
    '  main.vscene [file.scene] start',
    'Worlds [section.world]',
    'Prefabs [section.prefab]',
    '  nature [folder.prefab]',
    'Models [section.model]',
    '  quad.vmodel [file.model]',
    'Materials [section.material]',
    '  sprite.vmat [file.material]',
    'Textures [section.texture]',
    '  wood.vtex [file.texture]',
    'Images [section.image]',
    '  art [folder.image]',
    '    hero.png [file.image]',
    '  textures [folder.image]',
    '    wood.png [file.image]',
    'Scenarios [section.scenario]',
    '  start.vscenario [file.scenario]',
  ]);
  const index = tree.names(nodes);
  assert.strictEqual(index.scene.one, 'assets/scenes/levels/one.vscene');
  assert.strictEqual(index.model.quad, 'assets/models/quad.vmodel');
  assert.strictEqual(index.script, undefined);

  const find = (id) => {
    const walk = (list) => list.reduce((hit, n) => hit || (n.id === id ? n : walk(n.children)), undefined);
    return walk(nodes);
  };
  assert.strictEqual(tree.relIn(find('scene'), p), '');
  assert.strictEqual(tree.relIn(find('scene:assets/scenes/levels'), p), 'levels');
  assert.strictEqual(tree.relIn(find('scene:assets/scenes/levels/one.vscene'), p), 'levels');
  assert.strictEqual(tree.relIn(find('script:enemies/boss'), p), 'enemies/boss');
  assert.strictEqual(tree.relIn(find('script:main.lua'), p), '');
});

test('build follows veduta.json', () => {
  const root = game({
    'veduta.json': JSON.stringify({ veduta: 'project/1', name: 'g', script: 'src/main.lua', assets: 'content', default_world: 'land' }),
    'src/main.lua': '',
    'content/worlds/land.vworld': '{}',
    'content/scenes/main.vscene': '{}',
    'assets/scenes/ignored.vscene': '{}',
  });
  const p = tree.project(root);
  const lines = show(tree.build(tree.scan(root, p), p));
  assert.ok(lines.includes('    main.lua [file.script] start'), lines.join('\n'));
  assert.ok(lines.includes('  land.vworld [file.world] start'), lines.join('\n'));
  assert.ok(lines.includes('  main.vscene [file.scene]'), lines.join('\n'));
  assert.ok(!lines.some((l) => l.includes('ignored')), lines.join('\n'));
});

test('a Go game has no Scripts', () => {
  const root = game({ 'veduta.json': JSON.stringify({ veduta: 'project/1', name: 'g' }), 'go.mod': '', 'game/game.go': '' });
  const p = tree.project(root);
  assert.deepStrictEqual(tree.build(tree.scan(root, p), p).map((n) => n.label),
    ['Game', 'Scenes', 'Worlds', 'Prefabs', 'Models', 'Materials', 'Textures', 'Scenarios']);
});

test('checkName', () => {
  const index = { prefab: { tree: 'assets/prefabs/nature/tree.vprefab' } };
  assert.strictEqual(tree.checkName('house', 'prefab', index), '');
  assert.strictEqual(tree.checkName('tree', 'model', index), '');
  assert.match(tree.checkName('tree', 'prefab', index), /taken by assets\/prefabs\/nature\/tree\.vprefab/);
  for (const bad of ['', 'Tree', '_tree', 'a.b', 'a b', 'x'.repeat(65)]) {
    assert.notStrictEqual(tree.checkName(bad, 'prefab', index), '', bad);
  }
});
