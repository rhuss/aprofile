#!/usr/bin/env node
// Live preview for Antora and MkDocs guide projects (FR-030, research R9a).
//
//   node tools/guide/preview-watch.mjs [--internal] [--port N] [--no-open] [--root DIR]
//
// Builds the shareable (or, with --internal, the internal) view into a staging
// directory and swaps it into the served directory only when the build succeeds, so a
// broken page never replaces the last good site; the error is printed instead.
// chokidar watches the content sources; changes are debounced (300 ms), and changes
// that arrive during a build queue exactly one follow-up build. browser-sync serves the
// site and reloads the browser after every successful build (its client is injected
// into responses served as text/html, which Antora's .html pages are).
// Antora: the playbook comes from tools/guide/playbook.py, so a linked git worktree works too,
// and branding applies: the watcher also follows ui/ (which holds the overlay directory
// ui/supplemental/), the branding/ directory, and the logo and favicon files named in
// guide.yaml's branding block (re-read whenever guide.yaml changes, so newly named files
// join the watch; FR-023). These paths may be missing when the preview starts: a
// ui/supplemental/ or a logo file created while it runs also triggers a rebuild. A
// branding problem fails the build like any other error and keeps the last good site;
// branding warnings (an overlay file that shadows a generated one) are printed after a
// successful build too.
// MkDocs: `mkdocs build --strict` into the staging dir (mkdocs serve would stop or show a
// broken page instead of keeping the last good site). The format is read from
// tools/guide/adapter.yaml.
import { createRequire } from 'node:module';
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { basename, dirname, join, resolve, sep } from 'node:path';

const USAGE = 'usage: preview-watch.mjs [--internal] [--port N] [--no-open] [--root DIR]';

function parseArgs(argv) {
  const opts = { internal: false, port: 8000, open: true, root: '.' };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--internal') opts.internal = true;
    else if (arg === '--no-open') opts.open = false;
    else if (arg === '--port' || arg === '--root') {
      if (i + 1 >= argv.length || argv[i + 1].startsWith('--')) { console.error(`${arg} requires a value`); process.exit(2); }
      const value = argv[++i];
      if (arg === '--port') {
        opts.port = Number(value);
        if (!Number.isInteger(opts.port) || opts.port <= 0) { console.error(`bad port: ${value}`); process.exit(2); }
      } else opts.root = value;
    } else if (arg === '-h' || arg === '--help') { console.log(USAGE); process.exit(0); }
    else { console.error(`unknown argument: ${arg}\n${USAGE}`); process.exit(2); }
  }
  return opts;
}

const opts = parseArgs(process.argv.slice(2));
const root = resolve(opts.root);
const view = opts.internal ? 'internal' : 'shareable';
const require = createRequire(join(root, 'package.json'));
let chokidar; let browserSync;
try {
  chokidar = require('chokidar');
  browserSync = require('browser-sync');
} catch (err) {
  console.error(`[preview] missing Node packages in ${root} (run npm install): ${err.message}`);
  process.exit(1);
}
let yaml = null;
try {
  yaml = require('js-yaml');
} catch {
  // Without js-yaml the logo and favicon paths are not watched; ui/ and branding/ still are.
}

const adapterFile = join(root, 'tools', 'guide', 'adapter.yaml');
const format = existsSync(adapterFile)
  ? ((readFileSync(adapterFile, 'utf8').match(/^format:\s*["']?(\w+)/m) || [])[1] || 'antora')
  : 'antora';
if (!['antora', 'mkdocs'].includes(format)) {
  console.error(`[preview] the watcher supports the antora and mkdocs formats, not ${format}`);
  process.exit(2);
}

const previewDir = join(root, 'build', 'preview');
const served = join(previewDir, view);
const staging = join(previewDir, `.staging-${view}`);
const log = (msg) => console.log(`[preview] ${msg}`);

// The running build, if any. It runs in its own process group so stopping the preview
// also stops uv, npx, Antora, or MkDocs underneath it.
let activeChild = null;

function run(command, env = {}) {
  return new Promise((resolveRun) => {
    const child = spawn('bash', ['-c', command], { cwd: root, env: { ...process.env, ...env }, detached: true });
    activeChild = child;
    let out = '';
    child.stdout.on('data', (d) => { out += d; });
    child.stderr.on('data', (d) => { out += d; });
    child.on('close', (code) => { activeChild = null; resolveRun({ code, out }); });
    child.on('error', (err) => { activeChild = null; resolveRun({ code: 1, out: String(err) }); });
  });
}

function stopBuild() {
  if (!activeChild) return;
  try { process.kill(-activeChild.pid, 'SIGTERM'); } catch { /* already gone */ }
}

// One build into the staging dir; swapped in only on success.
async function buildOnce() {
  const started = Date.now();
  log(`building the ${view} view...`);
  rmSync(staging, { recursive: true, force: true });
  const command = format === 'mkdocs'
    ? 'uvx --quiet --with-requirements requirements.txt mkdocs build --strict '
      + `-f ${opts.internal ? 'mkdocs-internal.yml' : 'mkdocs.yml'} -d "$STAGING"`
    : 'set -o pipefail; '
      + `playbook=$(uv run --quiet tools/guide/playbook.py path ${opts.internal ? '--internal' : ''}) || exit 1; `
      + 'attrs=$(uv run --quiet tools/guide/attrs.py export --format antora) || exit 1; '
      + 'eval "npx --no-install antora --log-failure-level=warn $attrs --to-dir \\"$STAGING\\" \\"$playbook\\""';
  const { code, out } = await run(command, { STAGING: staging });
  const seconds = ((Date.now() - started) / 1000).toFixed(1);
  if (code !== 0 || !existsSync(join(staging, 'index.html'))) {
    console.error(`[preview] build FAILED after ${seconds}s; keeping the last good site. Error:`);
    console.error(out.trim().split('\n').slice(-25).join('\n'));
    rmSync(staging, { recursive: true, force: true });
    return false;
  }
  const old = join(previewDir, `.old-${view}`);
  rmSync(old, { recursive: true, force: true });
  if (existsSync(served)) renameSync(served, old);
  renameSync(staging, served);
  rmSync(old, { recursive: true, force: true });
  log(`build ok in ${seconds}s`);
  // Branding warnings (tools/guide/playbook.py prints them to stderr), for example an
  // overlay file that shadows a generated one (FR-020).
  out.split('\n').filter((line) => line.includes(': warning: ')).forEach((line) => log(line.trim()));
  return true;
}

// The branding inputs of an Antora guide: ui/ (with the overlay directory
// ui/supplemental/), the branding/ directory, and the logo and favicon files named in
// guide.yaml, inside the guide project. Any of them may be missing.
function brandingInputs() {
  const found = ['ui', 'branding'].map((p) => join(root, p));
  if (yaml) {
    try {
      const guide = yaml.load(readFileSync(join(root, 'guide.yaml'), 'utf8')) || {};
      const block = guide.branding;
      if (block && typeof block === 'object') {
        for (const key of ['logo', 'favicon']) {
          if (typeof block[key] === 'string' && block[key].trim()) found.push(resolve(root, block[key]));
        }
      }
    } catch {
      // An unreadable guide.yaml fails the next build, which reports it.
    }
  }
  return found.filter((p) => p.startsWith(root + sep));
}

// How to watch the branding inputs. chokidar reports at most one missing entry per
// directory, so missing inputs are not handed to it: an existing input is watched as is;
// for a missing one, its topmost missing ancestor is found. When that sits in the guide
// root, the root watcher (depth 0) reports it once it appears (rootNames); deeper down,
// the nearest existing directory is watched as a whole.
function watchPlan(inputs) {
  const paths = [];
  const rootNames = [];
  for (const input of inputs) {
    if (existsSync(input)) { paths.push(input); continue; }
    let top = input;
    while (!existsSync(dirname(top))) top = dirname(top);
    if (dirname(top) === root) rootNames.push(basename(top));
    else paths.push(dirname(top));
  }
  return { paths, rootNames };
}

let building = false;
let pending = false;
let timer = null;
let bs = null;

async function trigger() {
  if (building) { pending = true; return; }
  building = true;
  try {
    do {
      pending = false;
      if (await buildOnce() && bs) bs.reload();
    } while (pending);
  } finally {
    building = false;
  }
}

function schedule(event, path) {
  log(`${event}: ${path.startsWith(root) ? path.slice(root.length + 1) : path}`);
  clearTimeout(timer);
  timer = setTimeout(trigger, 300);
}

async function main() {
  mkdirSync(previewDir, { recursive: true });
  building = true;
  const ok = await buildOnce();
  building = false;
  if (!ok && !existsSync(served)) {
    mkdirSync(served, { recursive: true });
    writeFileSync(join(served, 'index.html'), '<!DOCTYPE html><html><head><meta charset="utf-8"><title>Preview</title></head>'
      + '<body><h1>The first build failed</h1><p>See the terminal for the error. The page reloads after the next successful build.</p></body></html>');
  }

  const sources = format === 'mkdocs'
    ? ['docs', 'mkdocs.yml', 'mkdocs-internal.yml', 'requirements.txt', 'tools/guide/mkdocs_macros.py']
    : ['content', 'site.yml', 'site-internal.yml'].concat(opts.internal ? ['internal'] : []);
  const watched = sources.concat(['baseline.yaml', 'guide.yaml'])
    .map((p) => join(root, p))
    .filter((p) => existsSync(p));
  // Branding inputs (Antora): paths in the main watch, and root entries still missing.
  const brandWatched = new Set();
  const rootNames = new Set();
  const planBranding = () => {
    if (format !== 'antora') return [];
    const plan = watchPlan(brandingInputs());
    plan.rootNames.forEach((name) => rootNames.add(name));
    const added = plan.paths.filter((p) => !brandWatched.has(p));
    added.forEach((p) => brandWatched.add(p));
    return added;
  };
  watched.push(...planBranding());
  const options = {
    ignoreInitial: true,
    ignored: (p) => /(^|[/\\])(node_modules|build|\.git|\.guide)([/\\]|$)/.test(p.slice(root.length)),
    awaitWriteFinish: { stabilityThreshold: 100, pollInterval: 50 },
  };
  const watcher = chokidar.watch(watched, options);
  watcher.on('all', (event, path) => {
    // A changed guide.yaml may name new logo or favicon files.
    if (path === join(root, 'guide.yaml')) {
      const added = planBranding();
      if (added.length) watcher.add(added);
    }
    schedule(event, path);
  });
  // Reports a missing branding input of the guide root (ui/, branding/, or the top
  // directory of a named logo or favicon file) once it appears; it then joins the watch.
  const rootWatcher = format === 'antora' ? chokidar.watch(root, { ...options, depth: 0 }) : null;
  if (rootWatcher) {
    rootWatcher.on('all', (event, path) => {
      if (dirname(path) !== root || !rootNames.has(basename(path))) return;
      if ((event === 'add' || event === 'addDir') && !brandWatched.has(path)) {
        brandWatched.add(path);
        watcher.add(path);
      }
      schedule(event, path);
    });
  }
  if (pending) trigger();

  bs = browserSync.create('guide-preview');
  bs.init({
    server: { baseDir: served },
    port: opts.port,
    open: opts.open ? 'local' : false,
    notify: false,
    ui: false,
    ghostMode: false,
    logLevel: 'silent',
    files: false,
  }, () => {
    const url = bs.getOption('urls').get('local');
    log(`serving the ${view} view at ${url} (Ctrl-C to stop)`);
  });

  const stop = () => {
    log('stopping');
    stopBuild();
    Promise.allSettled([watcher.close(), rootWatcher && rootWatcher.close()])
      .finally(() => { bs.exit(); process.exit(0); });
  };
  process.on('SIGINT', stop);
  process.on('SIGTERM', stop);
}

main().catch((err) => {
  console.error(`[preview] ${err.stack || err}`);
  process.exit(1);
});
