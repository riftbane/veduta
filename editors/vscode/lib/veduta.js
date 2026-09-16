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
    /\.(model|tex|mat|scene|scenario|prefab|world)\.json$/.test(name) || /\.go$/.test(name);
}

// validName is the engine's rule for a game's name, so the new game dialog can refuse a
// name before veduta init does.
function validName(name) {
  return /^[a-z0-9][a-z0-9_-]{0,63}$/.test(name);
}

module.exports = { program, playCommand, diagnostics, isGameFile, validName };
