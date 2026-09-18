'use strict';
// The Veduta extension: the veduta tool's commands inside VS Code. It holds no game logic:
// every action runs veduta in the project, as a person would in a terminal. What it draws
// itself, the texture preview and the map editor, comes from the engine's layer program and
// map picture transliterated in lib/texture.js and lib/tilemap.js, compared with the
// engine's own images by the tests.

const vscode = require('vscode');
const cp = require('child_process');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const v = require('./lib/veduta');
const texture = require('./lib/texture');
const tree = require('./lib/tree');

let problems;
let context; // the extension's own folder, for the files the preview panel loads
let preview = null; // the one texture preview: its panel, the file it shows, its timer
let building = null; // the running build, so saves in a row do not start builds in parallel
let again = false;
let project = null; // the project view (ProjectTree)

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
function task(folder, command, args = []) {
  const definition = args.length > 0 ? { type: 'veduta', command, args } : { type: 'veduta', command };
  const t = new vscode.Task(definition, folder, [command, ...args].join(' '), 'veduta',
    new vscode.ProcessExecution(veduta(), [command, ...args], { cwd: folder.uri.fsPath }), ['$veduta']);
  if (command === 'build') {
    t.group = vscode.TaskGroup.Build;
  } else if (command === 'test') {
    t.group = vscode.TaskGroup.Test;
  }
  t.presentationOptions = { reveal: vscode.TaskRevealKind.Always, clear: true };
  return t;
}

async function runTask(command, args = []) {
  const folder = await projectRoot();
  if (!folder) {
    vscode.window.showWarningMessage('Veduta: open a game project (a folder with veduta.json) first.');
    return;
  }
  await vscode.workspace.saveAll(false);
  return vscode.tasks.executeTask(task(folder, command, args));
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

// openPreview previews the active texture source, or the one given (a file of the project
// view, or its URI).
async function openPreview(target) {
  const uri = target instanceof vscode.Uri ? target : target && target.rel && project && project.uri(target);
  if (uri) {
    await vscode.window.showTextDocument(await vscode.workspace.openTextDocument(uri), { preview: false });
  }
  const doc = previewDoc();
  if (!doc) {
    vscode.window.showWarningMessage('Veduta: open a texture source (a .vtex file) to preview it.');
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

// --------------------------------------------------------------- the map editor
// A custom editor for *.vmap: the webview paints the map (lib/tilemap.js, the engine's
// picture of it) and sends the whole text back, written canonically, after each gesture.
// The text document stays the truth: its undo, redo, dirty state and save are VS Code's
// own, and a change made to it anywhere else is sent to the webview.

const mapEditors = new Set(); // the open map editors: {document, panel, sentAssets, pending, queue}

const lf = (s) => s.replace(/\r\n/g, '\n');

// projectOf finds the project a file belongs to: its root (the folder with veduta.json),
// its assets directory and its tick rate. A file outside a project gets the assets folder
// its path goes through and 20 ticks a second.
function projectOf(file) {
  let dir = path.dirname(file);
  for (;;) {
    if (fs.existsSync(path.join(dir, 'veduta.json'))) {
      let rate = 20;
      try {
        const m = JSON.parse(fs.readFileSync(path.join(dir, 'veduta.json'), 'utf8'));
        if (Number.isInteger(m.tick_rate) && m.tick_rate >= 1 && m.tick_rate <= 1000) {
          rate = m.tick_rate;
        }
      } catch (_) {
        // a manifest being written: the default
      }
      return { root: dir, assets: path.join(dir, ...tree.project(dir).assets.split('/')), tickRate: rate };
    }
    const up = path.dirname(dir);
    if (up === dir) {
      return { root: null, assets: v.assetsDir(file), tickRate: 20 };
    }
    dir = up;
  }
}

// assetIndex lists the textures and materials under an assets directory by name: a name
// is unique across folders. The cooked assets and hidden folders are left out.
const assetIndexes = new Map();
function assetIndex(dir) {
  if (assetIndexes.has(dir)) {
    return assetIndexes.get(dir);
  }
  const index = { textures: new Map(), materials: new Map() };
  const walk = (d) => {
    let entries;
    try {
      entries = fs.readdirSync(d, { withFileTypes: true });
    } catch (_) {
      return;
    }
    for (const e of entries) {
      if (e.name.startsWith('.')) {
        continue;
      }
      const p = path.join(d, e.name);
      if (e.isDirectory()) {
        walk(p);
      } else if (e.name.endsWith('.vtex') && e.name.length > 5) {
        index.textures.set(e.name.slice(0, -5), p);
      } else if (e.name.endsWith('.vmat') && e.name.length > 5) {
        index.materials.set(e.name.slice(0, -5), p);
      }
    }
  };
  walk(dir);
  assetIndexes.set(dir, index);
  return index;
}

// mapAssets gathers what the map's terrains are drawn with: the sources of their textures
// (a material's texture for a terrain with a material), the PNGs those read (never from
// outside the assets directory), every texture name of the project and its tick rate.
function mapAssets(doc) {
  const project = projectOf(doc.fileName);
  const index = assetIndex(project.assets);
  let src = null;
  try {
    src = JSON.parse(doc.getText());
  } catch (_) {
    // a map being written: nothing to draw with yet
  }
  const wanted = new Set();
  const materials = {};
  for (const t of (src && Array.isArray(src.terrains) ? src.terrains : [])) {
    if (!t || typeof t !== 'object') {
      continue;
    }
    if (typeof t.material === 'string' && t.material !== '') {
      const file = index.materials.get(t.material);
      if (!file) {
        continue;
      }
      let name = '';
      try {
        const m = JSON.parse(fs.readFileSync(file, 'utf8'));
        name = typeof m.texture === 'string' ? m.texture : '';
      } catch (_) {
        // a material that does not read has no texture to draw
      }
      materials[t.material] = name;
      if (name) {
        wanted.add(name);
      }
    } else if (typeof t.texture === 'string' && t.texture !== '') {
      wanted.add(t.texture);
    }
  }
  const textures = {};
  const images = {};
  for (const name of [...wanted].sort()) {
    const file = index.textures.get(name);
    if (!file) {
      continue;
    }
    let text;
    try {
      text = fs.readFileSync(file, 'utf8');
    } catch (e) {
      textures[name] = { error: `${name}.vtex: ${e.message}` };
      continue;
    }
    textures[name] = { text, file: path.relative(project.root || project.assets, file).split(path.sep).join('/') };
    for (const p of texture.deps(texture.parse(text).src)) {
      if (texture.badImagePath(p) !== '' || images[p]) {
        continue;
      }
      try {
        images[p] = fs.readFileSync(path.join(project.assets, ...p.split('/'))).toString('base64');
      } catch (e) {
        images[p] = { error: e.code === 'ENOENT' ? 'file not found in the assets directory' : String(e.message) };
      }
    }
  }
  return { type: 'assets', textures, materials, images, textureNames: [...index.textures.keys()].sort(), tickRate: project.tickRate };
}

function mapEditorHtml(webview) {
  const nonce = crypto.randomBytes(16).toString('base64');
  const uri = (...p) => webview.asWebviewUri(vscode.Uri.joinPath(context.extensionUri, ...p));
  const values = {
    cspSource: webview.cspSource,
    nonce,
    css: uri('media', 'mapeditor.css'),
    editor: uri('media', 'mapeditor.js'),
    png: uri('lib', 'png.js'),
    texture: uri('lib', 'texture.js'),
    tilemap: uri('lib', 'tilemap.js'),
  };
  const page = fs.readFileSync(path.join(context.extensionPath, 'media', 'mapeditor.html'), 'utf8');
  return page.replace(/{{(\w+)}}/g, (_, k) => String(values[k]));
}

class MapEditorProvider {
  resolveCustomTextEditor(document, panel) {
    const editor = { document, panel, sentAssets: '', pending: [], queue: Promise.resolve(), timer: null };
    mapEditors.add(editor);
    panel.webview.options = { enableScripts: true, localResourceRoots: [context.extensionUri] };
    panel.webview.html = mapEditorHtml(panel.webview);
    const sendDocument = () => panel.webview.postMessage({ type: 'document', text: document.getText(), file: path.basename(document.fileName) });
    editor.sendAssets = (force) => {
      const a = mapAssets(document);
      const key = JSON.stringify(a);
      if (force || key !== editor.sentAssets) {
        editor.sentAssets = key;
        panel.webview.postMessage(a);
      }
    };
    const subscriptions = [
      panel.webview.onDidReceiveMessage(async (m) => {
        if (!m) {
          return;
        }
        switch (m.type) {
          case 'ready':
            editor.sendAssets(true);
            sendDocument();
            break;
          case 'edit':
            // One edit per gesture, applied in order; its echo is not sent back.
            editor.pending.push(m.text);
            editor.queue = editor.queue.then(async () => {
              if (m.text === lf(document.getText())) {
                return;
              }
              const edit = new vscode.WorkspaceEdit();
              edit.replace(document.uri, document.validateRange(new vscode.Range(0, 0, document.lineCount, 0)), m.text);
              if (!await vscode.workspace.applyEdit(edit)) {
                editor.pending = [];
                sendDocument();
              }
            });
            break;
          case 'openJson':
            vscode.commands.executeCommand('vscode.openWith', document.uri, 'default');
            break;
          case 'confirm': {
            const choice = await vscode.window.showWarningMessage(m.message, { modal: true }, m.action);
            panel.webview.postMessage({ type: 'confirmed', id: m.id, ok: choice === m.action });
            break;
          }
          default:
            break;
        }
      }),
      vscode.workspace.onDidChangeTextDocument((e) => {
        if (e.document.uri.toString() !== document.uri.toString() || e.contentChanges.length === 0) {
          return;
        }
        const text = lf(document.getText()); // a CRLF document gets the edit with its own line ends
        const i = editor.pending.indexOf(text);
        if (i >= 0) {
          editor.pending.splice(0, i + 1);
          editor.sendAssets(false); // a new terrain may need its texture
          return;
        }
        editor.pending = [];
        clearTimeout(editor.timer);
        editor.timer = setTimeout(() => {
          editor.sendAssets(false);
          sendDocument();
        }, 60);
      }),
    ];
    panel.onDidDispose(() => {
      clearTimeout(editor.timer);
      subscriptions.forEach((s) => s.dispose());
      mapEditors.delete(editor);
    });
  }
}

// mapAssetsChanged sends the map editors what a texture, a material, an image or the
// manifest now is, once a burst of file events is over.
let mapAssetsTimer = null;
function mapAssetsChanged() {
  assetIndexes.clear();
  clearTimeout(mapAssetsTimer);
  mapAssetsTimer = setTimeout(() => mapEditors.forEach((e) => e.sendAssets(false)), 200);
}

// mapUri is the map a command is for: the one it was given, else the active editor's.
function mapUri(arg) {
  if (arg instanceof vscode.Uri) {
    return arg;
  }
  if (arg && arg.rel && project) {
    return project.uri(arg);
  }
  const tab = vscode.window.tabGroups.activeTabGroup.activeTab;
  const input = tab && tab.input;
  if (input && input.uri && /\.vmap$/.test(input.uri.path)) {
    return input.uri;
  }
  const editor = vscode.window.activeTextEditor;
  return editor && /\.vmap$/.test(editor.document.fileName) ? editor.document.uri : undefined;
}

function registerMapEditor() {
  const watcher = vscode.workspace.createFileSystemWatcher('**/*.{vtex,vmat,png,PNG}');
  const manifest = vscode.workspace.createFileSystemWatcher('**/veduta.json');
  context.subscriptions.push(
    vscode.window.registerCustomEditorProvider('veduta.mapEditor', new MapEditorProvider(),
      { webviewOptions: { retainContextWhenHidden: true }, supportsMultipleEditorsPerDocument: true }),
    watcher, manifest,
    watcher.onDidCreate(mapAssetsChanged), watcher.onDidChange(mapAssetsChanged), watcher.onDidDelete(mapAssetsChanged),
    manifest.onDidCreate(mapAssetsChanged), manifest.onDidChange(mapAssetsChanged), manifest.onDidDelete(mapAssetsChanged),
    vscode.commands.registerCommand('veduta.openMapAsJson', (arg) => {
      const uri = mapUri(arg);
      return uri && vscode.commands.executeCommand('vscode.openWith', uri, 'default');
    }),
    vscode.commands.registerCommand('veduta.openMapEditor', (arg) => {
      const uri = mapUri(arg);
      if (!uri) {
        vscode.window.showWarningMessage('Veduta: open a map (a .vmap file) first.');
        return undefined;
      }
      return vscode.commands.executeCommand('vscode.openWith', uri, 'veduta.mapEditor');
    }),
  );
}

// --------------------------------------------------------------- the project view
// The Veduta view in the activity bar: the project as lib/tree.js groups it (the files a
// person works on, by what they are, in their folders), rebuilt as files come and go. Its
// menus make new sources with veduta new, so a new file is one the engine accepts.

const KINDS = [
  ['scene', 'Scene'], ['world', 'World'], ['map', 'Map'], ['prefab', 'Prefab'], ['model', 'Model'],
  ['material', 'Material'], ['texture', 'Texture'], ['scenario', 'Scenario'], ['script', 'Script'],
];

class ProjectTree {
  constructor() {
    this.changed = new vscode.EventEmitter();
    this.onDidChangeTreeData = this.changed.event;
    this.root = null;
    this.info = null;
    this.nodes = [];
    this.parents = new Map();
    this.byId = new Map();
    this.files = new Map(); // file rel → node
    this.timer = null;
  }

  async refresh() {
    const folder = await projectRoot();
    this.root = folder ? folder.uri.fsPath : null;
    this.nodes = [];
    if (this.root) {
      this.info = tree.project(this.root);
      this.nodes = tree.build(tree.scan(this.root, this.info), this.info);
    }
    this.parents = new Map();
    this.byId = new Map();
    this.files = new Map();
    const walk = (n, parent) => {
      this.parents.set(n.id, parent);
      this.byId.set(n.id, n);
      if (n.type === 'file') {
        this.files.set(n.rel, n);
      }
      n.children.forEach((c) => walk(c, n));
    };
    this.nodes.forEach((n) => walk(n, undefined));
    this.changed.fire();
  }

  // later rebuilds the tree once a burst of file events is over.
  later() {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => this.refresh(), 200);
  }

  uri(node) {
    return vscode.Uri.file(path.join(this.root, ...node.rel.split('/')));
  }

  getTreeItem(node) {
    if (node.type === 'section') {
      const item = new vscode.TreeItem(node.label, node.children.length > 0
        ? vscode.TreeItemCollapsibleState.Expanded : vscode.TreeItemCollapsibleState.None);
      item.id = node.id;
      item.iconPath = new vscode.ThemeIcon(node.section.icon);
      item.contextValue = tree.contextValue(node);
      item.tooltip = node.rel ? `${node.label}: ${node.rel}` : node.label;
      return item;
    }
    const item = new vscode.TreeItem(node.label, node.type === 'folder'
      ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.None);
    item.id = node.id;
    item.resourceUri = this.uri(node);
    item.contextValue = tree.contextValue(node);
    item.tooltip = node.rel;
    if (node.description) {
      item.description = node.description;
    }
    if (node.type === 'folder') {
      item.iconPath = vscode.ThemeIcon.Folder;
    } else {
      item.iconPath = vscode.ThemeIcon.File;
      item.command = { command: 'vscode.open', title: 'Open', arguments: [item.resourceUri] };
    }
    return item;
  }

  getChildren(node) {
    return node ? node.children : this.nodes;
  }

  getParent(node) {
    return this.parents.get(node.id);
  }
}

// vedutaJSON runs veduta --json in the project and returns its report, or shows the error
// and returns null.
function vedutaJSON(root, args) {
  return new Promise((resolve) => {
    cp.execFile(veduta(), ['--json', ...args], { cwd: root, maxBuffer: 16 << 20 }, (err, stdout, stderr) => {
      let r;
      try {
        r = JSON.parse(stdout);
      } catch (_) {
        const why = err && err.code === 'ENOENT'
          ? 'veduta was not found: install it (install.ps1), or set veduta.path'
          : (stderr || stdout || String(err)).trim();
        vscode.window.showErrorMessage('Veduta: ' + why);
        resolve(null);
        return;
      }
      if (r.ok === false) {
        vscode.window.showErrorMessage('Veduta: ' + r.error);
        resolve(null);
        return;
      }
      resolve(r);
    });
  });
}

// newSource asks for a name and makes a source of the kind with veduta new, in the folder
// of node, then opens it. start is {scene} or {world} for a scenario.
async function newSource(kind, node, start = {}) {
  await project.refresh();
  if (!project.root) {
    vscode.window.showWarningMessage('Veduta: open a game project (a folder with veduta.json) first.');
    return;
  }
  const label = KINDS.find((k) => k[0] === kind)[1];
  const section = tree.SECTIONS.find((s) => s.id === kind);
  // The folder of the node it was asked on (a section, a folder, a file beside which it
  // goes), relative to the kind's own: veduta new's --in.
  const inFolder = node && node.section.id === kind ? tree.relIn(node, project.info) : '';
  const dir = [tree.sectionRoot(section, project.info), inFolder].filter(Boolean).join('/');
  const index = tree.names(project.nodes);
  const name = await vscode.window.showInputBox({
    title: `Veduta: new ${kind}` + (start.scene ? ` starting in scene ${start.scene}` : start.world ? ` starting in world ${start.world}` : ''),
    prompt: `Its name, which is also its file name, in ${dir || 'the project folder'}/`,
    validateInput: (s) => {
      const bad = tree.checkName(s, kind, index);
      if (bad) {
        return bad;
      }
      const file = path.join(project.root, ...dir.split('/').filter(Boolean), s + section.ext);
      return fs.existsSync(file) ? `${path.basename(file)} is there already` : undefined;
    },
  });
  if (!name) {
    return;
  }
  const args = ['new', kind, name];
  if (inFolder) {
    args.push('--in', inFolder);
  }
  if (start.scene) {
    args.push('--scene', start.scene);
  } else if (start.world) {
    args.push('--world', start.world);
  }
  const r = await vedutaJSON(project.root, args);
  if (!r) {
    return;
  }
  await project.refresh();
  const made = project.files.get(r.file);
  const file = vscode.Uri.file(path.join(project.root, ...r.file.split('/')));
  if (kind === 'map') {
    await vscode.commands.executeCommand('vscode.openWith', file, 'veduta.mapEditor', { preview: false }); // it opens painted
  } else {
    await vscode.window.showTextDocument(file, { preview: false });
  }
  if (made && project.view.visible) {
    project.view.reveal(made, { select: true, focus: false }).then(undefined, () => {});
  }
  if (r.files.length > 1) {
    vscode.window.showInformationMessage(`Veduta: ${label.toLowerCase()} ${name} made, with ${r.files.slice(1).join(' and ')} it stands on.`);
  }
}

// pickKind asks which kind of source to make, for the + of the view's title.
async function pickKind() {
  const pick = await vscode.window.showQuickPick(KINDS.map(([kind, label]) => ({ label, kind })), { title: 'Veduta: new', placeHolder: 'What to make' });
  if (pick) {
    await newSource(pick.kind, undefined);
  }
}

async function newFolder(node) {
  const base = node.rel;
  const name = await vscode.window.showInputBox({
    title: 'Veduta: new folder',
    prompt: `In ${base || 'the project folder'}/`,
    validateInput: (s) => {
      if (!tree.NAME.test(s)) {
        return 'a-z, 0-9, - and _, starting with a letter or digit';
      }
      return fs.existsSync(path.join(project.root, ...base.split('/').filter(Boolean), s)) ? `${s} is there already` : undefined;
    },
  });
  if (!name) {
    return;
  }
  const rel = [base, name].filter(Boolean).join('/');
  await vscode.workspace.fs.createDirectory(vscode.Uri.file(path.join(project.root, ...rel.split('/'))));
  await project.refresh();
  const made = project.byId.get(node.section.id + ':' + rel);
  if (made) {
    project.view.reveal(made, { select: true, focus: false, expand: true }).then(undefined, () => {});
  }
}

// rename gives a file or a folder a new name: an asset keeps its extension and its name
// stays free in its kind. What refers to the old name is not changed: the build that
// follows lists it in Problems.
async function rename(node) {
  const file = node.type === 'file';
  const ext = file ? path.extname(node.label) : '';
  const old = file ? node.label.slice(0, node.label.length - ext.length) : node.label;
  const kind = node.section.kind;
  const index = tree.names(project.nodes);
  if (file && index[kind]) {
    delete index[kind][old];
  }
  const dir = node.rel.slice(0, Math.max(0, node.rel.lastIndexOf('/')));
  const name = await vscode.window.showInputBox({
    title: `Veduta: rename ${node.label}`,
    value: old,
    valueSelection: [0, old.length],
    validateInput: (s) => {
      const bad = file && kind ? tree.checkName(s, kind, index) : (tree.NAME.test(s) ? '' : 'a-z, 0-9, - and _, starting with a letter or digit');
      if (bad) {
        return bad;
      }
      return s !== old && fs.existsSync(path.join(project.root, ...dir.split('/').filter(Boolean), s + ext)) ? `${s + ext} is there already` : undefined;
    },
  });
  if (!name || name === old) {
    return;
  }
  const to = vscode.Uri.file(path.join(project.root, ...dir.split('/').filter(Boolean), name + ext));
  const edit = new vscode.WorkspaceEdit();
  edit.renameFile(project.uri(node), to, { overwrite: false });
  if (!await vscode.workspace.applyEdit(edit)) {
    vscode.window.showErrorMessage(`Veduta: ${node.rel} was not renamed.`);
    return;
  }
  await project.refresh();
  if (vscode.workspace.getConfiguration('veduta').get('buildOnSave')) {
    build(true);
  }
}

async function remove(node) {
  const what = node.type === 'folder' ? `${node.rel} and everything in it` : node.rel;
  const ok = await vscode.window.showWarningMessage(`Delete ${what}?`, { modal: true }, 'Move to Trash');
  if (ok !== 'Move to Trash') {
    return;
  }
  try {
    await vscode.workspace.fs.delete(project.uri(node), { recursive: true, useTrash: true });
  } catch (e) {
    vscode.window.showErrorMessage(`Veduta: ${node.rel} was not deleted: ${e.message}`);
    return;
  }
  await project.refresh();
  if (vscode.workspace.getConfiguration('veduta').get('buildOnSave')) {
    build(true);
  }
}

function playNode(node) {
  return runTask(v.playCommand(process.platform), [node.section.id === 'world' ? '--world' : '--scene', node.name]);
}

async function debugScenario(node) {
  const folder = await projectRoot();
  return vscode.debug.startDebugging(folder, { type: 'veduta', request: 'launch', name: `Scenario ${node.name}`, mode: 'scenario', scenario: node.name });
}

function registerProjectView() {
  project = new ProjectTree();
  project.view = vscode.window.createTreeView('veduta.project', { treeDataProvider: project, showCollapseAll: true });
  const watcher = vscode.workspace.createFileSystemWatcher('**/*');
  const changed = (uri) => {
    if (project.root && uri.fsPath.startsWith(project.root)) {
      project.later();
    }
  };
  context.subscriptions.push(
    project.view,
    watcher,
    watcher.onDidCreate(changed),
    watcher.onDidDelete(changed),
    watcher.onDidChange((uri) => {
      if (path.basename(uri.fsPath) === 'veduta.json') {
        changed(uri);
      }
    }),
    vscode.workspace.onDidChangeWorkspaceFolders(() => project.later()),
    vscode.commands.registerCommand('veduta.view.refresh', () => project.refresh()),
    vscode.commands.registerCommand('veduta.view.new', pickKind),
    vscode.commands.registerCommand('veduta.view.newFolder', newFolder),
    vscode.commands.registerCommand('veduta.view.rename', rename),
    vscode.commands.registerCommand('veduta.view.delete', remove),
    vscode.commands.registerCommand('veduta.view.play', playNode),
    vscode.commands.registerCommand('veduta.view.newScenarioHere', (node) => newSource('scenario', undefined, { [node.section.id]: node.name })),
    vscode.commands.registerCommand('veduta.view.runScenario', (node) => runTask('simulate', ['--scenario', node.name])),
    vscode.commands.registerCommand('veduta.view.debugScenario', debugScenario),
    vscode.commands.registerCommand('veduta.view.reveal', (node) => vscode.commands.executeCommand('revealFileInOS', project.uri(node))),
    ...KINDS.map(([kind, label]) => vscode.commands.registerCommand('veduta.view.new' + label, (node) => newSource(kind, node))),
  );
  project.refresh();
}

async function activate(ctx) {
  context = ctx;
  problems = vscode.languages.createDiagnosticCollection('veduta');
  context.subscriptions.push(problems);
  // First: a map opened before the extension started waits for its editor.
  registerMapEditor();

  const isProject = !!(await projectRoot());
  vscode.commands.executeCommand('setContext', 'veduta.project', isProject);
  // A tool installed after VS Code started is not on the PATH its terminals inherit.
  const dir = v.terminalDir(veduta(), process.platform, process.env);
  if (dir) {
    context.environmentVariableCollection.append('PATH', (process.platform === 'win32' ? ';' : ':') + dir);
  }

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
        return folder && t.definition.command ? task(folder, t.definition.command, t.definition.args || []) : undefined;
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
  registerProjectView();
  if (isProject && vscode.workspace.getConfiguration('veduta').get('buildOnSave')) {
    build(true);
  }
  checkTool();
  // For the integration tests: the project view's tree.
  return { project: () => project };
}

// checkTool warns once per window when the veduta tool is missing or too old for this
// extension.
function checkTool() {
  cp.execFile(veduta(), ['--json', 'version'], { timeout: 15000 }, (err, stdout) => {
    const problem = v.toolProblem(err, stdout);
    if (problem) {
      vscode.window.showWarningMessage(problem, 'How to install').then((choice) => {
        if (choice) {
          vscode.env.openExternal(vscode.Uri.parse(v.INSTALL_URL));
        }
      });
      return;
    }
    // Tool and extension are released together but updated apart: an extension older
    // than the tool's lacks what the tool's docs describe, so it offers to catch up.
    const own = context.extension.packageJSON.version;
    const theirs = v.extensionBehind(own, stdout);
    if (theirs) {
      vscode.window.showWarningMessage(`Veduta: this extension is ${own}, and your veduta comes with ${theirs}.`, 'Update the Extension').then((choice) => {
        if (choice) {
          updateExtension(theirs);
        }
      });
    }
  });
}

// updateExtension installs the extension released with the tool: veduta extension fetches
// it (checksum verified) and VS Code installs it, then the window reloads to run it.
async function updateExtension(version) {
  const out = path.join(context.globalStorageUri.fsPath, `veduta-vscode-${version}.vsix`);
  fs.mkdirSync(context.globalStorageUri.fsPath, { recursive: true });
  const r = await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: `Veduta: fetching the extension ${version}` },
    () => vedutaJSON(context.globalStorageUri.fsPath, ['extension', '--out', out]));
  if (!r) {
    return;
  }
  try {
    await vscode.commands.executeCommand('workbench.extensions.installExtension', vscode.Uri.file(r.file));
  } catch (e) {
    vscode.window.showErrorMessage(`Veduta: the extension was not installed: ${e.message}. Install ${r.file} with Extensions → … → Install from VSIX.`);
    return;
  }
  const choice = await vscode.window.showInformationMessage(`Veduta: extension ${version} installed.`, 'Reload Window');
  if (choice) {
    vscode.commands.executeCommand('workbench.action.reloadWindow');
  }
}

function deactivate() {}

module.exports = { activate, deactivate };
