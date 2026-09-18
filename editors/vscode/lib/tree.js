'use strict';
// The project as the Veduta view shows it: what a person works on, grouped by what it is,
// with the folders they made and nothing the tool writes (out/, the cooked assets, editor
// files). Plain Node, no vscode: the tests run it on folders of their own.

const fs = require('fs');
const path = require('path');

// SECTIONS are the groups of the view, in order. kind is what veduta new makes in it; dir
// is where its files are (under the assets directory when assets is set); ext their
// extension. Scripts sit anywhere a script is searched for.
const SECTIONS = [
  { id: 'project', label: 'Game', icon: 'settings-gear' },
  { id: 'script', label: 'Scripts', icon: 'code', kind: 'script', ext: '.lua' },
  { id: 'scene', label: 'Scenes', icon: 'window', kind: 'scene', assets: true, dir: 'scenes', ext: '.vscene' },
  { id: 'world', label: 'Worlds', icon: 'globe', kind: 'world', assets: true, dir: 'worlds', ext: '.vworld' },
  { id: 'prefab', label: 'Prefabs', icon: 'package', kind: 'prefab', assets: true, dir: 'prefabs', ext: '.vprefab' },
  { id: 'model', label: 'Models', icon: 'symbol-structure', kind: 'model', assets: true, dir: 'models', ext: '.vmodel' },
  { id: 'material', label: 'Materials', icon: 'paintcan', kind: 'material', assets: true, dir: 'materials', ext: '.vmat' },
  { id: 'texture', label: 'Textures', icon: 'symbol-color', kind: 'texture', assets: true, dir: 'textures', ext: '.vtex' },
  { id: 'image', label: 'Images', icon: 'file-media', assets: true, dir: '', ext: '.png' },
  { id: 'scenario', label: 'Scenarios', icon: 'beaker', kind: 'scenario', dir: 'tests/scenarios', ext: '.vscenario', flat: true },
];

// PROJECT_FILES are the Game section: the files of the project itself, with what each is.
const PROJECT_FILES = [
  ['veduta.json', 'settings'],
  ['card.json', 'on the console'],
  ['README.md', ''],
  ['CHANGELOG.md', 'releases'],
];

// SKIP are folders never shown: the tool's output and what git, Go or Node keep.
const SKIP = new Set(['out', 'bin', 'build', 'node_modules']);

// NAME is the engine's rule for an asset's name, and for a script's or a folder's here.
const NAME = /^[a-z0-9][a-z0-9_-]{0,63}$/;

// project reads the fields of veduta.json the view needs, with the engine's defaults.
function project(root) {
  let m = {};
  try {
    m = JSON.parse(fs.readFileSync(path.join(root, 'veduta.json'), 'utf8')) || {};
  } catch (_) {
    // a manifest being written: the defaults
  }
  const clean = (p, d) => (typeof p === 'string' && p !== '' ? p.replace(/\\/g, '/').replace(/^\.\/|\/+$/g, '') : d);
  const assets = clean(m.assets, 'assets');
  return {
    assets,
    cooked: clean(m.cooked, assets + '/.cooked'),
    script: typeof m.script === 'string' ? clean(m.script, '') : '',
    icon: clean(m.icon, ''),
    scene: clean(m.default_scene, 'main'),
    world: clean(m.default_world, ''),
  };
}

// scan lists the project's folders and files as slash paths relative to root, leaving
// out hidden ones, SKIP and the cooked assets.
function scan(root, p) {
  const dirs = [];
  const files = [];
  const walk = (rel) => {
    let entries;
    try {
      entries = fs.readdirSync(path.join(root, rel), { withFileTypes: true });
    } catch (_) {
      return;
    }
    for (const e of entries) {
      const r = rel ? rel + '/' + e.name : e.name;
      if (e.name.startsWith('.') || (e.isDirectory() && (SKIP.has(r) || r === p.cooked))) {
        continue;
      }
      if (e.isDirectory()) {
        dirs.push(r);
        walk(r);
      } else if (e.isFile()) {
        files.push(r);
      }
    }
  };
  walk('');
  return { dirs: dirs.sort(), files: files.sort() };
}

// sectionRoot is the folder of a section's files relative to the project root.
function sectionRoot(s, p) {
  if (s.id === 'script' || s.id === 'project') {
    return '';
  }
  if (s.assets) {
    return s.dir ? p.assets + '/' + s.dir : p.assets;
  }
  return s.dir;
}

// belongs reports whether the file (or folder, with dir) at rel is shown in section s.
function belongs(s, p, rel, dir) {
  const base = sectionRoot(s, p);
  const inside = base === '' || rel.startsWith(base + '/');
  if (!inside) {
    return false;
  }
  if (s.id === 'script') {
    // Scripts are everywhere but the assets and tests folders.
    return !(rel === p.assets || rel.startsWith(p.assets + '/') || rel === 'tests' || rel.startsWith('tests/')) &&
      (dir || rel.endsWith('.lua'));
  }
  if (s.id === 'image') {
    return dir || rel.toLowerCase().endsWith('.png');
  }
  if (s.flat && dir) {
    return false;
  }
  return dir || (rel.endsWith(s.ext) && rel.length > base.length + 1 + s.ext.length);
}

// build turns a scan into the view's tree: a node per section, then folders and files.
// Every node has an id (stable across rebuilds, so VS Code keeps what is expanded), a
// type (section, folder or file), its section and, but for sections, rel: its path
// relative to the root. A file of an asset kind has its asset name.
function build(scanned, p) {
  const nodes = [];
  for (const s of SECTIONS) {
    const sec = { id: s.id, type: 'section', section: s, label: s.label, rel: sectionRoot(s, p), children: [] };
    if (s.id === 'project') {
      for (const [f, what] of PROJECT_FILES.concat(p.icon ? [[p.icon, 'icon']] : [])) {
        if (scanned.files.includes(f)) {
          sec.children.push({ id: 'project:' + f, type: 'file', section: s, label: f, rel: f, description: what, children: [] });
        }
      }
      nodes.push(sec);
      continue;
    }
    const byRel = new Map([[sec.rel, sec]]);
    const parentOf = (rel) => {
      const i = rel.lastIndexOf('/');
      return byRel.get(i < 0 ? '' : rel.slice(0, i));
    };
    for (const d of scanned.dirs) {
      if (d !== sec.rel && belongs(s, p, d, true)) {
        const parent = parentOf(d);
        if (parent) {
          const node = { id: s.id + ':' + d, type: 'folder', section: s, label: d.slice(d.lastIndexOf('/') + 1), rel: d, children: [] };
          parent.children.push(node);
          byRel.set(d, node);
        }
      }
    }
    for (const f of scanned.files) {
      if (!belongs(s, p, f, false)) {
        continue;
      }
      const parent = parentOf(f);
      if (!parent) {
        continue;
      }
      const label = f.slice(f.lastIndexOf('/') + 1);
      const node = { id: s.id + ':' + f, type: 'file', section: s, label, rel: f, children: [] };
      if (s.kind && s.id !== 'script') {
        node.name = label.slice(0, -s.ext.length);
      }
      if (s.id === 'script' && f === p.script) {
        node.description = 'start';
      } else if ((s.id === 'scene' && !p.world && node.name === p.scene) || (s.id === 'world' && node.name === p.world)) {
        node.description = 'start';
      }
      parent.children.push(node);
    }
    // A script is searched for everywhere, so a folder whose files are none of them is
    // left out of Scripts, and one without images out of Images; a folder with nothing
    // in it yet stays (a person made it to put things in), as do an asset kind's folders.
    const prune = (n) => {
      n.children = n.children.filter((c) => c.type === 'file' || prune(c) ||
        (s.id !== 'image' && (s.id !== 'script' || !scanned.files.some((f) => f.startsWith(c.rel + '/')))));
      return n.children.length > 0;
    };
    prune(sec);
    const order = (n) => {
      n.children.sort((a, b) => (a.type === b.type ? (a.label < b.label ? -1 : a.label > b.label ? 1 : 0) : a.type === 'folder' ? -1 : 1));
      n.children.forEach(order);
    };
    order(sec);
    if ((s.id === 'image' && sec.children.length === 0) || (s.id === 'script' && !p.script)) {
      continue; // no images, or a Go game: no section
    }
    nodes.push(sec);
  }
  return nodes;
}

// names indexes the asset names of a tree by kind, each with its file: an asset's name
// is its file name whatever its folder, so a new one must be free in the whole section.
function names(nodes) {
  const out = {};
  const walk = (n) => {
    if (n.type === 'file' && n.name !== undefined) {
      (out[n.section.kind] = out[n.section.kind] || {})[n.name] = n.rel;
    }
    n.children.forEach(walk);
  };
  nodes.forEach(walk);
  return out;
}

// relIn returns rel relative to the section root of a node (the --in veduta new takes).
function relIn(node, p) {
  const base = sectionRoot(node.section, p);
  let dir = node.type === 'folder' ? node.rel : node.type === 'file' ? node.rel.slice(0, Math.max(0, node.rel.lastIndexOf('/'))) : base;
  if (base !== '' && (dir === base || dir.startsWith(base + '/'))) {
    dir = dir.slice(base.length + (dir === base ? 0 : 1));
  } else if (base !== '') {
    dir = '';
  }
  return dir;
}

// contextValue is what the menus of the view match on: "<type>.<section>".
function contextValue(node) {
  return node.type + '.' + node.section.id;
}

// checkName says what is wrong with a new name for a node's kind, or returns ''.
function checkName(name, kind, index) {
  if (!NAME.test(name)) {
    return 'a-z, 0-9, - and _, starting with a letter or digit, at most 64';
  }
  const taken = index && index[kind] && index[kind][name];
  if (taken) {
    return `the ${kind} name "${name}" is taken by ${taken}`;
  }
  return '';
}

module.exports = { SECTIONS, NAME, project, scan, build, sectionRoot, names, relIn, contextValue, checkName };
