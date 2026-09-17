'use strict';
// The Veduta extension: the veduta tool's commands inside VS Code. It holds no game logic:
// every action runs veduta in the project, as a person would in a terminal. The one thing it
// draws itself is the texture preview, from the engine's layer program transliterated in
// lib/texture.js and compared with the engine's own images by the tests.

const vscode = require('vscode');
const cp = require('child_process');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const v = require('./lib/veduta');
const texture = require('./lib/texture');

let problems;
let context; // the extension's own folder, for the files the preview panel loads
let preview = null; // the one texture preview: its panel, the file it shows, its timer
let building = null; // the running build, so saves in a row do not start builds in parallel
let again = false;

function veduta() {
  const setting = vscode.workspace.getConfiguration('veduta').get('path');
  return v.program(setting, process.platform, process.env);
}

// projectRoot is the workspace folder holding veduta.json: the active editor's, else the
// first one that has it.
async function projectRoot() {
  const folders = vscode.workspace.workspaceFolders || [];
  const active = vscode.window.activeTextEditor && vscode.workspace.getWorkspaceFolder(vscode.window.activeTextEditor.document.uri);
  for (const f of active ? [active, ...folders] : folders) {
    try {
      await vscode.workspace.fs.stat(vscode.Uri.joinPath(f.uri, 'veduta.json'));
      return f;
    } catch (_) {
      // not a project
    }
  }
  return undefined;
}

// task is a veduta command run in the terminal panel, where its output shows and it can be
// stopped.
function task(folder, command) {
  const definition = { type: 'veduta', command };
  const t = new vscode.Task(definition, folder, command, 'veduta',
    new vscode.ProcessExecution(veduta(), [command], { cwd: folder.uri.fsPath }), ['$veduta']);
  if (command === 'build') {
    t.group = vscode.TaskGroup.Build;
  } else if (command === 'test') {
    t.group = vscode.TaskGroup.Test;
  }
  t.presentationOptions = { reveal: vscode.TaskRevealKind.Always, clear: true };
  return t;
}

async function runTask(command) {
  const folder = await projectRoot();
  if (!folder) {
    vscode.window.showWarningMessage('Veduta: open a game project (a folder with veduta.json) first.');
    return;
  }
  await vscode.workspace.saveAll(false);
  return vscode.tasks.executeTask(task(folder, command));
}

// build runs veduta --json build and puts its errors in Problems.
async function build(quiet) {
  const folder = await projectRoot();
  if (!folder) {
    return;
  }
  if (building) {
    again = true;
    return building;
  }
  building = new Promise((resolve) => {
    cp.execFile(veduta(), ['--json', 'build'], { cwd: folder.uri.fsPath, maxBuffer: 16 << 20 }, (err, stdout, stderr) => {
      let report;
      try {
        report = JSON.parse(stdout);
      } catch (_) {
        const why = err && err.code === 'ENOENT'
          ? 'veduta was not found: install it (install.ps1), or set veduta.path'
          : (stderr || stdout || String(err)).trim();
        vscode.window.showErrorMessage('Veduta: build did not run: ' + why);
        resolve();
        return;
      }
      problems.clear();
      for (const [file, list] of v.diagnostics(report)) {
        const uri = vscode.Uri.file(path.resolve(folder.uri.fsPath, file));
        problems.set(uri, list.map((d) => {
          const range = new vscode.Range(d.line, d.col, d.line, d.col === 0 ? Number.MAX_SAFE_INTEGER : d.col + 1);
          const diag = new vscode.Diagnostic(range, d.message,
            d.severity === 'warning' ? vscode.DiagnosticSeverity.Warning : vscode.DiagnosticSeverity.Error);
          diag.source = d.source;
          return diag;
        }));
      }
      if (!quiet) {
        if (report.ok) {
          vscode.window.setStatusBarMessage('$(check) Veduta: build ok', 3000);
        } else {
          vscode.window.showErrorMessage('Veduta: the build failed; see Problems.');
        }
      }
      resolve();
    });
  }).finally(() => {
    building = null;
    if (again) {
      again = false;
      build(true);
    }
  });
  return building;
}

async function newGame() {
  const kind = await vscode.window.showQuickPick([
    { label: 'Lua', description: 'the default: no Go, nothing to build', go: false },
    { label: 'Go', description: 'needs Go 1.25 or newer', go: true },
  ], { title: 'Veduta: new game', placeHolder: 'Written in' });
  if (!kind) {
    return;
  }
  const parent = await vscode.window.showOpenDialog({ canSelectFolders: true, canSelectFiles: false, openLabel: 'Make the game in this folder', title: 'Veduta: where the game goes' });
  if (!parent || parent.length === 0) {
    return;
  }
  const name = await vscode.window.showInputBox({
    title: 'Veduta: the game\'s name',
    prompt: 'Lower case letters, digits, - and _: the folder and the release archives are named after it',
    validateInput: (s) => (v.validName(s) ? undefined : 'a-z, 0-9, - and _, starting with a letter or digit'),
  });
  if (!name) {
    return;
  }
  const dir = path.join(parent[0].fsPath, name);
  const args = ['init', dir];
  if (kind.go) {
    args.push('--go');
  }
  await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: `Veduta: making ${name}` }, () => new Promise((resolve) => {
    cp.execFile(veduta(), args, (err, stdout, stderr) => {
      if (err) {
        const why = err.code === 'ENOENT' ? 'veduta was not found: install it (install.ps1), or set veduta.path' : (stderr || stdout || String(err)).trim();
        vscode.window.showErrorMessage('Veduta: ' + why);
      } else {
        vscode.commands.executeCommand('vscode.openFolder', vscode.Uri.file(dir));
      }
      resolve();
    });
  }));
}

// --------------------------------------------------------------- the texture preview
// A panel beside the editor that draws the texture its JSON describes, redrawn while the
// file is typed in. It runs no engine: lib/texture.js is the engine's layer program in
// JavaScript, checked against the engine's golden images.

// previewHtml builds the page of the panel, with the nonce its scripts need.
function previewHtml(webview) {
  const nonce = crypto.randomBytes(16).toString('base64');
  const uri = (...p) => webview.asWebviewUri(vscode.Uri.joinPath(context.extensionUri, ...p));
  const values = {
    cspSource: webview.cspSource,
    nonce,
    css: uri('media', 'preview.css'),
    preview: uri('media', 'preview.js'),
    png: uri('lib', 'png.js'),
    texture: uri('lib', 'texture.js'),
  };
  const page = fs.readFileSync(path.join(context.extensionPath, 'media', 'preview.html'), 'utf8');
  return page.replace(/{{(\w+)}}/g, (_, k) => String(values[k]));
}

// sendSource gives the panel the text of the document and the images its layers name,
// read from the project's assets directory. A path the engine would refuse is left out:
// the panel reports it, and nothing outside the assets directory is ever read.
function sendSource(doc) {
  if (!preview || !doc) {
    return;
  }
  const text = doc.getText();
  const dir = v.assetsDir(doc.fileName);
  const images = {};
  for (const p of texture.deps(texture.parse(text).src)) {
    if (texture.badImagePath(p) !== '') {
      continue;
    }
    try {
      images[p] = fs.readFileSync(path.join(dir, p)).toString('base64');
    } catch (e) {
      images[p] = { error: e.code === 'ENOENT' ? 'file not found in the assets directory' : String(e.message) };
    }
  }
  preview.uri = doc.uri;
  preview.panel.title = path.basename(doc.fileName);
  preview.panel.webview.postMessage({ type: 'source', file: path.basename(doc.fileName), text, images });
}

// previewDoc is the texture source a preview would show: the active editor's, else the
// one the panel already shows.
function previewDoc() {
  const editor = vscode.window.activeTextEditor;
  if (editor && v.isTextureFile(editor.document.fileName)) {
    return editor.document;
  }
  if (preview && preview.uri) {
    return vscode.workspace.textDocuments.find((d) => d.uri.toString() === preview.uri.toString());
  }
  return undefined;
}

function openPreview() {
  const doc = previewDoc();
  if (!doc) {
    vscode.window.showWarningMessage('Veduta: open a texture source (a .tex.json file) to preview it.');
    return;
  }
  if (preview) {
    preview.panel.reveal(vscode.ViewColumn.Beside, true);
    sendSource(doc);
    return;
  }
  const panel = vscode.window.createWebviewPanel('veduta.texture', path.basename(doc.fileName),
    { viewColumn: vscode.ViewColumn.Beside, preserveFocus: true },
    { enableScripts: true, localResourceRoots: [context.extensionUri] });
  preview = { panel, uri: doc.uri, timer: null };
  panel.webview.html = previewHtml(panel.webview);
  // The page asks for the source once it can receive it, and again whenever VS Code
  // rebuilds it (a hidden panel is thrown away and reloaded when it comes back).
  panel.webview.onDidReceiveMessage((m) => {
    if (m && m.type === 'ready') {
      sendSource(previewDoc());
    }
  });
  panel.onDidDispose(() => {
    if (preview) {
      clearTimeout(preview.timer);
    }
    preview = null;
  });
}

// previewChanged redraws the panel a moment after a keystroke, so it follows the file
// being written instead of waiting for a save.
function previewChanged(doc) {
  if (!preview || !preview.uri || doc.uri.toString() !== preview.uri.toString()) {
    return;
  }
  clearTimeout(preview.timer);
  preview.timer = setTimeout(() => sendSource(doc), 120);
}

async function activate(ctx) {
  context = ctx;
  problems = vscode.languages.createDiagnosticCollection('veduta');
  context.subscriptions.push(problems);

  const isProject = !!(await projectRoot());
  vscode.commands.executeCommand('setContext', 'veduta.project', isProject);

  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  status.text = '$(play) Veduta';
  status.tooltip = `Play the game without the debugger: veduta ${v.playCommand(process.platform)} (F5 plays it with the debugger)`;
  status.command = 'veduta.play';
  if (isProject) {
    status.show();
  }
  context.subscriptions.push(status);

  context.subscriptions.push(
    vscode.commands.registerCommand('veduta.newGame', newGame),
    vscode.commands.registerCommand('veduta.play', () => runTask(v.playCommand(process.platform))),
    vscode.commands.registerCommand('veduta.test', () => runTask('test')),
    vscode.commands.registerCommand('veduta.deploy', () => runTask('deploy')),
    vscode.commands.registerCommand('veduta.build', () => build(false)),
    vscode.commands.registerCommand('veduta.previewTexture', openPreview),
    vscode.tasks.registerTaskProvider('veduta', {
      provideTasks: async () => {
        const folder = await projectRoot();
        if (!folder) {
          return [];
        }
        return [v.playCommand(process.platform), 'test', 'build', 'deploy'].map((c) => task(folder, c));
      },
      resolveTask: async (t) => {
        const folder = t.scope && t.scope.uri ? t.scope : await projectRoot();
        return folder && t.definition.command ? task(folder, t.definition.command) : undefined;
      },
    }),
    // F5 with no launch.json plays the game under the debugger; veduta dap is the adapter.
    vscode.debug.registerDebugConfigurationProvider('veduta', {
      resolveDebugConfiguration: async (folder, config) => {
        const root = folder || await projectRoot();
        if (!config.type && !config.request && !config.name) {
          if (!root) {
            vscode.window.showWarningMessage('Veduta: open a game project (a folder with veduta.json) first.');
            return undefined;
          }
          return v.launchConfig({}, root.uri.fsPath);
        }
        return v.launchConfig(config, root ? root.uri.fsPath : '');
      },
    }),
    vscode.debug.registerDebugAdapterDescriptorFactory('veduta', {
      createDebugAdapterDescriptor: () => new vscode.DebugAdapterExecutable(veduta(), ['dap']),
    }),
    vscode.workspace.onDidChangeTextDocument((e) => previewChanged(e.document)),
    vscode.window.onDidChangeActiveTextEditor((editor) => {
      if (preview && editor && v.isTextureFile(editor.document.fileName)) {
        sendSource(editor.document);
      }
    }),
    vscode.workspace.onDidSaveTextDocument((doc) => {
      if (vscode.workspace.getConfiguration('veduta').get('buildOnSave') && v.isGameFile(doc.fileName)) {
        build(true);
      }
    }),
  );
  if (isProject && vscode.workspace.getConfiguration('veduta').get('buildOnSave')) {
    build(true);
  }
  checkTool();
}

// checkTool warns once per window when the veduta tool is missing or too old for this
// extension.
function checkTool() {
  cp.execFile(veduta(), ['--json', 'version'], { timeout: 15000 }, (err, stdout) => {
    const problem = v.toolProblem(err, stdout);
    if (!problem) {
      return;
    }
    vscode.window.showWarningMessage(problem, 'How to install').then((choice) => {
      if (choice) {
        vscode.env.openExternal(vscode.Uri.parse(v.INSTALL_URL));
      }
    });
  });
}

function deactivate() {}

module.exports = { activate, deactivate };
