#!/usr/bin/env node
// Screenshots and render checks for cc-guide (ported from the SAW guide's scripts/shoot.mjs).
//   node tools/guide/shoot.mjs page <url-or-file> <out.png> [--offline] [--width 1280]
//   node tools/guide/shoot.mjs svg <in.svg> <out.png> [--scale 2]
// "page" exits 1 when an image fails to load or the page throws.
// "svg" renders an exported diagram at the given device scale (2x for diagram review)
// and exits 1 with a clear message when the input holds no <svg> element.
// Exit 2 on usage errors (unknown mode, missing input, option without a value).
import { chromium } from 'playwright';
import { existsSync, readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';

const usage = 'usage: shoot.mjs page|svg <input> <output.png> [--offline] [--width N] [--scale N]';
const [mode, input, output, ...rest] = process.argv.slice(2);

const fail = (message, code) => {
  console.error(message);
  process.exit(code);
};

const opt = (name, fallback) => {
  const i = rest.indexOf(`--${name}`);
  if (i === -1) return fallback;
  if (i + 1 >= rest.length || rest[i + 1].startsWith('--')) fail(`--${name} requires a value`, 2);
  return rest[i + 1];
};

const num = (name, fallback) => {
  const value = Number(opt(name, fallback));
  if (!Number.isFinite(value) || value <= 0) fail(`--${name} must be a positive number`, 2);
  return value;
};

if (!mode || !input || !output) fail(usage, 2);
if (mode !== 'page' && mode !== 'svg') fail(`unknown mode: ${mode}\n${usage}`, 2);
const isUrl = /^[a-z]+:\/\//.test(input);
if (!isUrl && !existsSync(input)) fail(`input not found: ${input}`, 2);

const browser = await chromium.launch();
try {
  if (mode === 'page') {
    const context = await browser.newContext({
      viewport: { width: num('width', 1280), height: 900 },
      offline: rest.includes('--offline'),
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (e) => errors.push(String(e)));
    const url = isUrl ? input : pathToFileURL(resolve(input)).href;
    await page.goto(url, { waitUntil: 'load' });
    await page.screenshot({ path: output, fullPage: true });
    const broken = await page.evaluate(() =>
      [...document.images].filter((i) => !i.complete || i.naturalWidth === 0).map((i) => i.src));
    if (broken.length) { console.error(`broken images: ${broken.join(', ')}`); process.exitCode = 1; }
    if (errors.length) { console.error(`page errors: ${errors.join(' | ')}`); process.exitCode = 1; }
  } else {
    const scale = num('scale', 2);
    const markup = readFileSync(input, 'utf8');
    if (!/<svg[\s>]/i.test(markup)) {
      console.error(`no <svg> element found in ${input}`);
      process.exitCode = 1;
    } else {
      const context = await browser.newContext({ deviceScaleFactor: scale });
      const page = await context.newPage();
      await page.setContent(`<html><body style="margin:0;background:#fff">${markup}</body></html>`);
      const svg = await page.$('svg');
      if (!svg) {
        console.error(`no <svg> element found in ${input}`);
        process.exitCode = 1;
      } else {
        await svg.screenshot({ path: output });
      }
    }
  }
} finally {
  await browser.close();
}
