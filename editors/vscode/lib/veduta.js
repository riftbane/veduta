'use strict';
// What the extension decides without VS Code: which program, which command, which errors.
// Kept apart so that node --test checks it.

const fs = require('fs');
const path = require('path');

// program returns the veduta program to run: the setting, else the one install.ps1 puts in
// %LOCALAPPDATA%\Programs\veduta when it is there (VS Code started before the installer
// does not see the new PATH), else veduta on PATH.
function program(setting, platform, env, exists = fs.existsSync) {
  if (setting) {
    return setting;
  }
  if (platform === 'win32' && env.LOCALAPPDATA) {
    const installed = path.win32.join(env.LOCALAPPDATA, 'Programs', 'veduta', 'veduta.exe');
    if (exists(installed)) {
      return installed;
    }
  }
  return 'veduta';
}

// terminalDir returns the folder of the veduta program found by program when the PATH VS
// Code started with lacks it (installed after VS Code started), for VS Code's terminals to
// get it too; '' when there is nothing to add.
function terminalDir(prog, platform, env) {
  const p = platform === 'win32' ? path.win32 : path.posix;
  if (!p.isAbsolute(prog)) {
    return '';
  }
  const dir = p.dirname(prog);
  const sep = platform === 'win32' ? ';' : ':';
  const key = Object.keys(env).find((k) => k.toUpperCase() === 'PATH');
  const norm = (d) => (platform === 'win32' ? d.replace(/[\\/]+$/, '').toLowerCase() : d.replace(/\/+$/, ''));
  const onPath = (key ? env[key] : '').split(sep).some((d) => d && norm(d) === norm(dir));
  return onPath ? '' : dir;
}

// playCommand is how a game is played here: the simulator window on Windows, the
// framebuffer player elsewhere.
function playCommand(platform) {
  return platform === 'win32' ? 'sim' : 'run';
}

// diagnostics turns the report of veduta --json build into located problems, by file
// relative to the project. Lines count from 1 in the report and from 0 in VS Code; a column
// of 0 means the column is not known.
function diagnostics(report) {
  const byFile = new Map();
  const add = (list, source, severity) => {
    for (const e of list || []) {
      if (!e || !e.file) {
        continue;
      }
      const line = Math.max((e.line || 1) - 1, 0);
      const col = Math.max((e.col || 1) - 1, 0);
      if (!byFile.has(e.file)) {
        byFile.set(e.file, []);
      }
      byFile.get(e.file).push({ line, col, message: e.msg, source, severity });
    }
  };
  add(report.cook_errors, 'veduta cook', 'error');
  add(report.errors, 'veduta build', 'error');
  add(report.vet_errors, 'go vet', 'warning');
  return byFile;
}

// isGameFile reports whether saving a file should build the game: a script or a source
// file of the engine's formats.
function isGameFile(name) {
  return /\.lua$/.test(name) || /(^|[\\/])veduta\.json$/.test(name) ||
    /\.v(model|tex|mat|scene|scenario|prefab|world|map)$/.test(name) || /\.go$/.test(name);
}

// validName is the engine's rule for a game's name, so the new game dialog can refuse a
// name before veduta init does.
function validName(name) {
  return /^[a-z0-9][a-z0-9_-]{0,63}$/.test(name);
}

// isTextureFile reports whether a file is a texture source, the one format the preview
// panel draws.
function isTextureFile(name) {
  return /\.vtex$/.test(name);
}

// assetsDir returns the directory an image layer's path is relative to: the assets folder
// of the project the file belongs to (the one beside veduta.json), else the assets folder
// the file's own path goes through, else its folder.
function assetsDir(file, exists = fs.existsSync) {
  let dir = path.dirname(file);
  for (;;) {
    if (exists(path.join(dir, 'veduta.json'))) {
      return path.join(dir, 'assets');
    }
    const up = path.dirname(dir);
    if (up === dir) {
      break;
    }
    dir = up;
  }
  const parts = file.split(/[\\/]/);
  const i = parts.lastIndexOf('assets');
  if (i < 0) {
    return path.dirname(file);
  }
  const head = parts.slice(0, i + 1);
  // A path that started at the root keeps its leading separator.
  return head[0] === '' ? path.sep + path.join(...head.slice(1)) : path.join(...head);
}

// launchConfig completes a debug configuration: the game's folder when it names none, and
// play when it names no mode.
function launchConfig(config, root) {
  const c = Object.assign({ type: 'veduta', request: 'launch', name: 'Play' }, config);
  if (!c.project) {
    c.project = root;
  }
  if (!c.mode) {
    c.mode = 'play';
  }
  return c;
}

// INSTALL_URL is where the warnings about the tool send a person.
const INSTALL_URL = 'https://github.com/riftbane/veduta/wiki/Installation';

// toolProblem says what is wrong with the veduta tool this extension would run, from the
// error and output of veduta --json version, or returns null. The extension is made for
// engine v2 (Lua games): a v1 tool, which the stable channel installs until v2.0.0 is
// released, makes Go games and has no debugger, so it gets a warning, as does no tool at all.
function toolProblem(err, stdout) {
  if (err && err.code === 'ENOENT') {
    return 'Veduta: the veduta tool is not installed, or not on PATH. Install it (beta channel), then restart VS Code.';
  }
  let version;
  try {
    version = JSON.parse(stdout).version;
  } catch (_) {
    return err ? `Veduta: veduta version failed: ${err.message}` : null;
  }
  const m = /^v(\d+)\./.exec(version || '');
  if (m && Number(m[1]) < 2) {
    return `Veduta: the veduta tool is ${version}, which makes Go games; this extension needs v2 (Lua games). Reinstall it from the beta channel.`;
  }
  if (m && older(version, MIN_TOOL)) {
    return `Veduta: the veduta tool is ${version}; this extension needs ${MIN_TOOL} or later (the Project view makes files with veduta new, and maps with veduta new map). Update it: install.ps1 again on Windows, veduta update elsewhere.`;
  }
  return null;
}

// extensionBehind returns the version of the extension released with the tool, from the
// output of veduta --json version, when this extension (own) is older than it; else null.
// A development build of the tool has no release to take the extension from.
function extensionBehind(own, stdout) {
  let info;
  try {
    info = JSON.parse(stdout);
  } catch (_) {
    return null;
  }
  if (!info || typeof info.extension !== 'string' || !/^v\d/.test(info.version || '')) {
    return null;
  }
  return older('v' + own, 'v' + info.extension) ? info.extension : null;
}

// MIN_TOOL is the oldest veduta this extension works with: the first with maps (veduta new
// map, the .vmap format the map editor writes).
const MIN_TOOL = 'v2.0.0-rc.11';

// older reports whether version a comes before b; both are vX.Y.Z with an optional -rc.N,
// and a release comes after its candidates.
function older(a, b) {
  const parse = (s) => {
    const m = /^v(\d+)\.(\d+)\.(\d+)(?:-rc\.(\d+))?/.exec(s) || [];
    return [Number(m[1]) || 0, Number(m[2]) || 0, Number(m[3]) || 0, m[4] === undefined ? Infinity : Number(m[4])];
  };
  const x = parse(a);
  const y = parse(b);
  for (let i = 0; i < 4; i++) {
    if (x[i] !== y[i]) {
      return x[i] < y[i];
    }
  }
  return false;
}

module.exports = { program, terminalDir, playCommand, diagnostics, isGameFile, isTextureFile, assetsDir, launchConfig, validName, toolProblem, older, extensionBehind, INSTALL_URL };
