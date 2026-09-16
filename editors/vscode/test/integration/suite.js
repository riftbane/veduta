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
  for (const c of ['veduta.newGame', 'veduta.play', 'veduta.test', 'veduta.build', 'veduta.deploy']) {
    assert.ok(commands.includes(c), c + ' is registered');
  }

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
}

module.exports = { run };
