'use strict';
// The Veduta extension: the veduta tool's commands inside VS Code. It holds no game logic
// and no engine: every action runs veduta in the project, as a person would in a terminal.

const vscode = require('vscode');
const cp = require('child_process');
const path = require('path');
const v = require('./lib/veduta');

let problems;
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

async function activate(context) {
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
