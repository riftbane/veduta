'use strict';
// Runs inside VS Code, on a project veduta init made, whose main.lua is then broken.
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const vscode = require('vscode');

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function until(what, ok, ms = 60000) {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    if (await ok()) {
      return;
    }
    await sleep(250);
  }
  throw new Error('timed out waiting for ' + what);
}

function problemsOf(file) {
  return vscode.languages.getDiagnostics(vscode.Uri.file(file)).filter((d) => d.source === 'veduta build');
}

async function run() {
  const root = vscode.workspace.workspaceFolders[0].uri.fsPath;
  const main = path.join(root, 'main.lua');
  const good = fs.readFileSync(main, 'utf8');

  const ext = vscode.extensions.getExtension('riftbane.veduta');
  assert.ok(ext, 'the extension is installed');
  await ext.activate();
  const commands = await vscode.commands.getCommands(true);
  for (const c of ['veduta.newGame', 'veduta.play', 'veduta.test', 'veduta.build', 'veduta.deploy', 'veduta.previewTexture',
    'veduta.view.new', 'veduta.view.newPrefab', 'veduta.view.newMap', 'veduta.view.newFolder', 'veduta.view.rename', 'veduta.view.delete',
    'veduta.view.play', 'veduta.openMapEditor', 'veduta.openMapAsJson']) {
    assert.ok(commands.includes(c), c + ' is registered');
  }

  // The Project view: the game veduta init made, by section; a folder and a prefab made
  // from its menus (the input boxes answered here) are on disk, open, and in the view.
  const project = ext.exports.project();
  await vscode.commands.executeCommand('veduta.view.refresh');
  assert.deepStrictEqual(project.nodes.map((n) => n.label),
    ['Game', 'Scripts', 'Scenes', 'Worlds', 'Maps', 'Prefabs', 'Models', 'Materials', 'Textures', 'Scenarios']);
  const node = (id) => project.byId.get(id);
  assert.strictEqual(node('scene:assets/scenes/main.vscene').description, 'start');
  const input = vscode.window.showInputBox;
  const answers = [];
  vscode.window.showInputBox = async (o) => {
    const [value, check] = answers.shift();
    assert.strictEqual(o.validateInput(value) || '', '', `${value} is refused: ${o.validateInput(value)}`);
    if (check) {
      assert.match(o.validateInput(check[0]), check[1]);
    }
    return value;
  };
  try {
    answers.push(['nature', ['Nature', /a-z/]]);
    await vscode.commands.executeCommand('veduta.view.newFolder', node('prefab'));
    assert.ok(node('prefab:assets/prefabs/nature'), 'the folder is in the view');
    answers.push(['tree']);
    await vscode.commands.executeCommand('veduta.view.newPrefab', node('prefab:assets/prefabs/nature'));
    const prefab = path.join(root, 'assets', 'prefabs', 'nature', 'tree.vprefab');
    assert.ok(fs.readFileSync(prefab, 'utf8').includes('"prefab/1"'), 'veduta new wrote the prefab');
    assert.strictEqual(vscode.window.activeTextEditor.document.uri.fsPath, prefab);
    assert.strictEqual(vscode.window.activeTextEditor.document.languageId, 'json');
    assert.ok(node('prefab:assets/prefabs/nature/tree.vprefab'), 'the prefab is in the view');
    answers.push(['oak', ['start', /taken by/]]);
    await vscode.commands.executeCommand('veduta.view.newScenarioHere', node('scene:assets/scenes/main.vscene'));
    assert.ok(fs.readFileSync(path.join(root, 'tests', 'scenarios', 'oak.vscenario'), 'utf8').includes('"scene": "main"'));
    answers.push(['pine']);
    await vscode.commands.executeCommand('veduta.view.rename', node('prefab:assets/prefabs/nature/tree.vprefab'));
    assert.ok(fs.existsSync(path.join(root, 'assets', 'prefabs', 'nature', 'pine.vprefab')), 'renamed');
    await until('the renamed prefab in the view', () => node('prefab:assets/prefabs/nature/pine.vprefab'));

    // A map from veduta new map opens in the map editor; Open as JSON and back.
    answers.push(['farm']);
    await vscode.commands.executeCommand('veduta.view.newMap', node('map'));
    const map = path.join(root, 'assets', 'maps', 'farm.vmap');
    assert.ok(fs.readFileSync(map, 'utf8').includes('"map/1"'), 'veduta new wrote the map');
    const mapTab = (custom) => vscode.window.tabGroups.all.flatMap((g) => g.tabs).find((t) => (custom
      ? t.input instanceof vscode.TabInputCustom && t.input.viewType === 'veduta.mapEditor'
      : t.input instanceof vscode.TabInputText) && t.input.uri.fsPath === map);
    await until('the map editor', () => mapTab(true) !== undefined);
    assert.ok(node('map:assets/maps/farm.vmap'), 'the map is in the view');
    await vscode.commands.executeCommand('veduta.openMapAsJson', vscode.Uri.file(map));
    await until('the map as JSON', () => vscode.window.activeTextEditor && vscode.window.activeTextEditor.document.uri.fsPath === map);
    assert.strictEqual(vscode.window.activeTextEditor.document.languageId, 'json');
    await vscode.commands.executeCommand('veduta.openMapEditor', vscode.Uri.file(map));
    await until('the map editor again', () => { const t = mapTab(true); return t && t.isActive; });
  } finally {
    vscode.window.showInputBox = input;
  }
  await vscode.commands.executeCommand('workbench.action.closeAllEditors');

  // A broken script: Build puts the error in Problems, at its line.
  const lines = good.split('\n').length;
  fs.writeFileSync(main, good + '\nfunction game.update(\n');
  await vscode.commands.executeCommand('veduta.build');
  await until('the syntax error in Problems', () => problemsOf(main).length > 0);
  const d = problemsOf(main)[0];
  assert.ok(d.range.start.line >= lines - 1, `error at line ${d.range.start.line + 1}, the file had ${lines} lines`);
  assert.strictEqual(d.severity, vscode.DiagnosticSeverity.Error);

  // Fixed and saved in the editor: the build on save clears it.
  const doc = await vscode.workspace.openTextDocument(main);
  const editor = await vscode.window.showTextDocument(doc);
  await editor.edit((e) => e.replace(new vscode.Range(0, 0, doc.lineCount, 0), good));
  await doc.save();
  await until('Problems to clear', () => problemsOf(main).length === 0);

  // The tasks are there, and Test runs the scenarios to a pass.
  const tasks = await vscode.tasks.fetchTasks({ type: 'veduta' });
  const names = tasks.map((t) => t.definition.command).sort();
  assert.deepStrictEqual(names, ['build', 'deploy', 'run', 'test']);
  let exit;
  const done = vscode.tasks.onDidEndTaskProcess((e) => {
    if (e.execution.task.definition.command === 'test') {
      exit = e.exitCode;
    }
  });
  await vscode.commands.executeCommand('veduta.test');
  await until('veduta test to end', () => exit !== undefined, 120000);
  done.dispose();
  assert.strictEqual(exit, 0, 'veduta test passes');

  // The debugger: a breakpoint in game.draw stops a scenario there, the stack says so, and
  // the session runs to its end once continued.
  const drawLine = good.split('\n').findIndex((l) => l.includes('hud.text'));
  assert.ok(drawLine > 0, 'main.lua draws its title');
  vscode.debug.addBreakpoints([new vscode.SourceBreakpoint(new vscode.Location(vscode.Uri.file(main), new vscode.Position(drawLine, 0)))]);
  let stopped = false;
  let ended = false;
  const tracker = vscode.debug.registerDebugAdapterTrackerFactory('veduta', {
    createDebugAdapterTracker: () => ({
      onDidSendMessage: (m) => {
        if (m.type === 'event' && m.event === 'stopped') {
          stopped = true;
        }
      },
    }),
  });
  const ends = vscode.debug.onDidTerminateDebugSession(() => { ended = true; });
  const started = await vscode.debug.startDebugging(vscode.workspace.workspaceFolders[0],
    { type: 'veduta', request: 'launch', name: 'Scenario', mode: 'scenario', scenario: 'start' });
  assert.ok(started, 'the debug session starts');
  await until('the breakpoint to stop the game', () => stopped);
  const session = vscode.debug.activeDebugSession;
  const trace = await session.customRequest('stackTrace', { threadId: 1 });
  assert.strictEqual(trace.stackFrames[0].line, drawLine + 1);
  assert.ok(trace.stackFrames[0].source.path.endsWith('main.lua'));
  vscode.debug.removeBreakpoints(vscode.debug.breakpoints);
  await session.customRequest('continue', { threadId: 1 });
  await until('the session to end', () => ended, 120000);
  tracker.dispose();
  ends.dispose();

  // The texture preview: the command opens a panel beside the source it draws, and the
  // panel survives the file being typed in.
  const texFile = path.join(root, 'assets', 'textures', 'probe.vtex');
  fs.mkdirSync(path.dirname(texFile), { recursive: true });
  fs.writeFileSync(texFile, JSON.stringify({
    veduta: 'texture/1',
    size: [16, 16],
    layers: [{ type: 'rect', xy: [2, 2], size: [12, 12], color: '#ff8800', corner: 3 }],
  }, null, 2) + '\n');
  const texDoc = await vscode.workspace.openTextDocument(texFile);
  await vscode.window.showTextDocument(texDoc);
  await vscode.commands.executeCommand('veduta.previewTexture');
  const previewTab = () => vscode.window.tabGroups.all
    .flatMap((g) => g.tabs)
    .find((t) => t.input instanceof vscode.TabInputWebview && t.label.includes('probe.vtex'));
  await until('the preview panel', () => previewTab() !== undefined);
  const texEditor = await vscode.window.showTextDocument(texDoc);
  await texEditor.edit((e) => e.insert(new vscode.Position(2, 0), '  "tiling": true,\n'));
  await sleep(500);
  assert.ok(previewTab(), 'the preview stays open while the source is written');
  await vscode.window.tabGroups.close(previewTab());
}

module.exports = { run };
