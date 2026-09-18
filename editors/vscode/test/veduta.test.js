'use strict';
const test = require('node:test');
const assert = require('node:assert');
const v = require('../lib/veduta');

test('program', () => {
  assert.strictEqual(v.program('C:\\tools\\veduta.exe', 'win32', {}), 'C:\\tools\\veduta.exe');
  const installed = 'C:\\Users\\me\\AppData\\Local\\Programs\\veduta\\veduta.exe';
  assert.strictEqual(v.program('', 'win32', { LOCALAPPDATA: 'C:\\Users\\me\\AppData\\Local' }, (p) => p === installed), installed);
  assert.strictEqual(v.program('', 'win32', { LOCALAPPDATA: 'C:\\Users\\me\\AppData\\Local' }, () => false), 'veduta');
  assert.strictEqual(v.program('', 'linux', { LOCALAPPDATA: 'x' }, () => true), 'veduta');
});

test('playCommand', () => {
  assert.strictEqual(v.playCommand('win32'), 'sim');
  assert.strictEqual(v.playCommand('linux'), 'run');
});

test('diagnostics', () => {
  const report = {
    ok: false,
    errors: [{ file: 'main.lua', line: 15, col: 0, msg: "<name> expected near 'local'" }],
    cook_errors: [{ file: 'assets/models/crate.vmodel', line: 3, col: 12, msg: 'parts: required' }],
    vet_errors: [{ file: 'game/game.go', line: 7, col: 2, msg: 'unreachable code' }],
  };
  const d = v.diagnostics(report);
  assert.deepStrictEqual(d.get('main.lua'), [{ line: 14, col: 0, message: "<name> expected near 'local'", source: 'veduta build', severity: 'error' }]);
  assert.deepStrictEqual(d.get('assets/models/crate.vmodel')[0], { line: 2, col: 11, message: 'parts: required', source: 'veduta cook', severity: 'error' });
  assert.strictEqual(d.get('game/game.go')[0].severity, 'warning');
  assert.strictEqual(v.diagnostics({ ok: true, errors: [] }).size, 0);
});

test('isGameFile', () => {
  for (const f of ['main.lua', 'lib/a.lua', 'veduta.json', 'C:\\g\\veduta.json', 'assets/scenes/main.vscene', 'tests/scenarios/start.vscenario', 'assets/maps/farm.vmap', 'game/game.go']) {
    assert.ok(v.isGameFile(f), f);
  }
  for (const f of ['package.json', '.vscode/settings.json', 'README.md', 'notveduta.json']) {
    assert.ok(!v.isGameFile(f), f);
  }
});

test('validName', () => {
  assert.ok(v.validName('mygame'));
  assert.ok(v.validName('cave-of-gems_2'));
  assert.ok(!v.validName('My Game'));
  assert.ok(!v.validName('-x'));
  assert.ok(!v.validName(''));
});

test('launchConfig', () => {
  assert.deepStrictEqual(v.launchConfig({}, '/g'), { type: 'veduta', request: 'launch', name: 'Play', project: '/g', mode: 'play' });
  assert.deepStrictEqual(v.launchConfig({ type: 'veduta', request: 'launch', name: 'S', mode: 'scenario', scenario: 'start', project: '/other' }, '/g'),
    { type: 'veduta', request: 'launch', name: 'S', mode: 'scenario', scenario: 'start', project: '/other' });
});

test('toolProblem', () => {
  assert.strictEqual(v.toolProblem(null, '{"version":"v2.0.0-rc.14","commit":"x"}'), null);
  assert.strictEqual(v.toolProblem(null, '{"version":"v2.0.0"}'), null);
  assert.match(v.toolProblem(null, '{"version":"v2.0.0-rc.13"}'), /needs v2\.0\.0-rc\.14 or later/);
  assert.strictEqual(v.toolProblem(null, '{"version":"dev"}'), null);
  assert.strictEqual(v.toolProblem(null, '{"version":"v10.1.0"}'), null);
  assert.match(v.toolProblem(null, '{"version":"v1.4.1"}'), /v1\.4\.1, which makes Go games/);
  assert.match(v.toolProblem({ code: 'ENOENT' }, ''), /not installed/);
  assert.match(v.toolProblem({ message: 'exit 2' }, 'usage'), /failed: exit 2/);
});

test('extensionBehind', () => {
  const out = (version, extension) => JSON.stringify({ version, extension });
  assert.strictEqual(v.extensionBehind('0.7.0', out('v2.0.0-rc.10', '0.8.0')), '0.8.0');
  assert.strictEqual(v.extensionBehind('0.8.0', out('v2.0.0-rc.10', '0.8.0')), null);
  assert.strictEqual(v.extensionBehind('0.9.0', out('v2.0.0-rc.10', '0.8.0')), null);
  assert.strictEqual(v.extensionBehind('0.7.0', out('dev', '0.8.0')), null, 'a dev tool has no release');
  assert.strictEqual(v.extensionBehind('0.7.0', JSON.stringify({ version: 'v2.0.0-rc.9' })), null, 'a tool that does not say');
  assert.strictEqual(v.extensionBehind('0.7.0', 'usage'), null);
});

test('older', () => {
  assert.ok(v.older('v2.0.0-rc.8', 'v2.0.0-rc.9'));
  assert.ok(v.older('v2.0.0-rc.9', 'v2.0.0'));
  assert.ok(v.older('v1.9.9', 'v2.0.0-rc.1'));
  assert.ok(!v.older('v2.0.0', 'v2.0.0-rc.9'));
  assert.ok(!v.older('v2.0.0-rc.10', 'v2.0.0-rc.9'));
  assert.ok(!v.older('v2.1.0-rc.1', 'v2.0.0'));
  assert.ok(!v.older('v2.0.0-rc.9', 'v2.0.0-rc.9'));
});

test('isTextureFile', () => {
  for (const f of ['assets/textures/crate.vtex', 'C:\\g\\assets\\textures\\ui\\panel.vtex']) {
    assert.ok(v.isTextureFile(f), f);
  }
  for (const f of ['assets/models/crate.vmodel', 'veduta.json', 'crate.vtex.bak']) {
    assert.ok(!v.isTextureFile(f), f);
  }
});

test('assetsDir', () => {
  const project = (p) => p === '/g/veduta.json';
  assert.strictEqual(v.assetsDir('/g/assets/textures/ui/panel.vtex', project), '/g/assets');
  assert.strictEqual(v.assetsDir('/g/elsewhere/panel.vtex', project), '/g/assets');
  // No project above it: the assets folder its own path goes through.
  assert.strictEqual(v.assetsDir('/x/game/assets/textures/panel.vtex', () => false), '/x/game/assets');
  assert.strictEqual(v.assetsDir('/x/loose/panel.vtex', () => false), '/x/loose');
});

test('terminals get the folder of a tool installed after VS Code started', () => {
  const exe = 'C:\\Users\\me\\AppData\\Local\\Programs\\veduta\\veduta.exe';
  const dir = 'C:\\Users\\me\\AppData\\Local\\Programs\\veduta';
  assert.strictEqual(v.terminalDir(exe, 'win32', { Path: 'C:\\Windows;C:\\Windows\\System32' }), dir);
  assert.strictEqual(v.terminalDir(exe, 'win32', { Path: 'C:\\Windows;c:\\users\\me\\appdata\\local\\programs\\veduta\\' }), '');
  assert.strictEqual(v.terminalDir(exe, 'win32', {}), dir);
  assert.strictEqual(v.terminalDir('veduta', 'win32', { Path: '' }), '');
  assert.strictEqual(v.terminalDir('/opt/veduta/veduta', 'linux', { PATH: '/usr/bin:/opt/veduta/' }), '');
  assert.strictEqual(v.terminalDir('/opt/veduta/veduta', 'linux', { PATH: '/usr/bin' }), '/opt/veduta');
});
