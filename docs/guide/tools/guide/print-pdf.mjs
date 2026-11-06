#!/usr/bin/env node
// One PDF of the whole guide from one print HTML document (FR-030a to FR-030f).
//
//   node tools/guide/print-pdf.mjs --html <print.html> --guide-root <dir>
//        --view shareable|internal --out <file.pdf>
//        [--page-heading <css>] [--section-heading <css>] [--hide <css>] [--allow-broken-links]
//
// Every adapter produces a self-contained print HTML document per view (Antora: the
// html-single export; MkDocs: the print-site page; gfm: Pandoc over the navigation
// order). This script adds a title page (guide title, baseline date, pinned source
// revisions) and a linked table of contents, rewrites links between pages to anchors
// inside the document, applies print CSS, and prints with page numbers in the footer
// (and INTERNAL in every footer for the internal view).
//
// Branded guides (a `branding` block in guide.yaml, Antora only): the title page starts
// with the guide's logo on a band in the site header's color (#151515), so a logo made
// for the dark header stays legible (FR-025). The logo comes from the rendered branding
// output, build/branding/<view>/branding.json and the logo file it names (written by
// tools/guide/playbook.py during the site build), and is embedded as a base64 data URL
// in an <img>, never as markup (FR-026). Without that output the print stops with
// "branding output missing for the <view> view; run make check". Without a branding
// block the title page is unchanged (FR-027).
//
// Neutral theme (`theme: neutral`, specs/003-neutral-branding): branding.json also names
// the vendored font directory and its faces. The print then sets body text in Red Hat
// Text, headings, the title page, and the table of contents in Red Hat Display, and code
// in Red Hat Mono, loading the fonts through file: URLs (remote fonts stay blocked), and
// draws a red rule under the title band. A missing font file stops the print with the
// same "branding output missing" message. Without the theme the print CSS is unchanged.
//
// Page detection: --page-heading selects the heading that starts each guide page and
// --section-heading the navigation section headings. Without them the Antora
// html-single structure is detected (section = .sect1 > h2, page = .sect2 > h3);
// otherwise every h1 in the document is a page.
//
// Exit status: 0 PDF written; 1 the PDF could not be produced (nothing is left
// behind: the PDF is written to a temporary file and renamed at the end); 2 usage.
import { createRequire } from 'node:module';
import { existsSync, readFileSync, renameSync, rmSync } from 'node:fs';
import { basename, dirname, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const USAGE = 'usage: print-pdf.mjs --html <file> --guide-root <dir> --view shareable|internal --out <file.pdf> '
  + '[--page-heading <css>] [--section-heading <css>] [--hide <css>] [--allow-broken-links]';

function parseArgs(argv) {
  const opts = { allowBrokenLinks: false };
  const valued = { '--html': 'html', '--guide-root': 'root', '--view': 'view', '--out': 'out',
    '--page-heading': 'pageHeading', '--section-heading': 'sectionHeading', '--hide': 'hide' };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--allow-broken-links') { opts.allowBrokenLinks = true; continue; }
    if (arg === '-h' || arg === '--help') { console.log(USAGE); process.exit(0); }
    const key = valued[arg];
    if (!key) { console.error(`unknown argument: ${arg}\n${USAGE}`); process.exit(2); }
    if (i + 1 >= argv.length || argv[i + 1].startsWith('--')) {
      console.error(`${arg} requires a value`); process.exit(2);
    }
    opts[key] = argv[++i];
  }
  for (const k of ['html', 'root', 'view', 'out']) {
    if (!opts[k]) { console.error(USAGE); process.exit(2); }
  }
  if (!['shareable', 'internal'].includes(opts.view)) {
    console.error(`--view must be shareable or internal (got ${opts.view})`); process.exit(2);
  }
  return opts;
}

function webUrl(repo) {
  let url = String(repo);
  if (url.startsWith('git@github.com:')) url = `https://github.com/${url.split(':', 2)[1]}`;
  else if (!/^[a-z][a-z0-9+.-]*:\/\//.test(url) && /^[\w.-]+\/[\w.-]+$/.test(url)) url = `https://github.com/${url}`;
  return url.replace(/\/+$/, '').replace(/\.git$/, '');
}

function escapeHtml(text) {
  return String(text).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

const PRINT_CSS = `
@page { size: A4; margin: 18mm 16mm 20mm 16mm; }
@page guide-wide { size: A4 landscape; }
html, body { background: #fff !important; }
body { font-family: "Noto Serif", "DejaVu Serif", Georgia, serif; font-size: 10.5pt; line-height: 1.45; color: #222; }
#header, #footer, #toc, a.anchor, .md-header, .md-footer, .md-sidebar, .md-search { display: none !important; }
#content, .md-content, main { max-width: none !important; margin: 0 !important; padding: 0 !important; }
h1, h2, h3, h4, h5 { break-after: avoid; page-break-after: avoid; color: #1f2933; }
a { color: #1a5fb4; text-decoration: none; }
/* Links stay clickable in the PDF; do not print their URLs after the text. */
a[href]::after { content: none !important; }
pre, pre code { white-space: pre-wrap !important; word-break: break-word; overflow-wrap: anywhere; overflow: visible !important; }
pre { font-size: 8.5pt; background: #f6f8fa !important; border: 1px solid #d0d7de; padding: 6px 8px; break-inside: avoid; }
code { font-size: 0.92em; }
table { width: 100% !important; max-width: 100% !important; table-layout: auto; border-collapse: collapse; }
td, th { overflow-wrap: anywhere; word-break: break-word; }
table.tableblock td, table.tableblock th, .md-typeset table td, .md-typeset table th { border: 1px solid #d0d7de; padding: 3px 6px; }
.guide-wide { page: guide-wide; }
img { max-width: 100% !important; height: auto !important; break-inside: avoid; }
.imageblock img[src$=".svg"], img.guide-diagram, a > img[src$=".svg"] { width: 100% !important; }
.imageblock, figure { break-inside: avoid; }
.guide-page { break-before: page; }
.sect1 { break-before: page; }
.sectionbody > .guide-page:first-child { break-before: avoid; }
.sect1 > h2 { font-size: 20pt; border-bottom: 2px solid #495057; padding-bottom: 4px; }

/* Admonitions: five types plus experimental labels, visually distinct (FR-030c). */
.admonitionblock { break-inside: avoid; margin: 10px 0; }
.admonitionblock > table { border: 0; border-left: 5px solid #868e96; background: #f8f9fa; width: 100%; }
.admonitionblock td.icon { width: 110px; vertical-align: top; padding: 6px 8px; text-align: left; }
.admonitionblock td.icon i, .admonitionblock td.icon img { display: none; }
.admonitionblock td.icon::before { font-family: Helvetica, Arial, sans-serif; font-weight: bold; font-size: 8pt; letter-spacing: 0.03em; white-space: nowrap; }
.admonitionblock td.content { padding: 6px 10px; border: 0 !important; }
.admonitionblock.note > table { border-left-color: #1971c2; background: #e7f5ff; }
.admonitionblock.note td.icon::before { content: "NOTE"; color: #1971c2; }
.admonitionblock.tip > table { border-left-color: #2b8a3e; background: #ebfbee; }
.admonitionblock.tip td.icon::before { content: "TIP"; color: #2b8a3e; }
.admonitionblock.important > table { border-left-color: #e8590c; background: #fff4e6; }
.admonitionblock.important td.icon::before { content: "IMPORTANT"; color: #e8590c; }
.admonitionblock.warning > table { border-left-color: #f59f00; background: #fff9db; }
.admonitionblock.warning td.icon::before { content: "WARNING"; color: #b35c00; }
.admonitionblock.caution > table { border-left-color: #c92a2a; background: #fff5f5; }
.admonitionblock.caution td.icon::before { content: "CAUTION"; color: #c92a2a; }
.admonitionblock.guide-experimental > table { border: 2px dashed #7048e8; border-left: 6px solid #7048e8; background: #f3f0ff; }
.admonitionblock.guide-experimental td.icon::before { content: "EXPERIMENTAL"; color: #7048e8; }
.admonition, .markdown-alert, div.note, div.tip, div.important, div.warning, div.caution {
  break-inside: avoid; border-left: 5px solid #868e96; background: #f8f9fa; padding: 6px 10px; margin: 10px 0; }
.admonition.note, .markdown-alert-note, div.note { border-left-color: #1971c2; background: #e7f5ff; }
.admonition.tip, .markdown-alert-tip, div.tip { border-left-color: #2b8a3e; background: #ebfbee; }
.admonition.important, .markdown-alert-important, div.important { border-left-color: #e8590c; background: #fff4e6; }
.admonition.warning, .markdown-alert-warning, div.warning { border-left-color: #f59f00; background: #fff9db; }
.admonition.caution, .admonition.danger, .markdown-alert-caution, div.caution { border-left-color: #c92a2a; background: #fff5f5; }
.guide-experimental:not(.admonitionblock) { border: 2px dashed #7048e8 !important; border-left: 6px solid #7048e8 !important; background: #f3f0ff !important; }

/* Title page and table of contents. */
.guide-title-page { break-after: page; padding-top: 70mm; font-family: Helvetica, Arial, sans-serif; }
.guide-title-page h1 { font-size: 28pt; margin: 0 0 8mm 0; border: 0; }
.guide-title-page .guide-baseline { font-size: 12pt; color: #495057; margin-bottom: 8mm; }
.guide-sources-label { font-size: 10pt; font-weight: bold; color: #495057; margin-bottom: 2mm; }
.guide-sources { list-style: none; padding: 0; margin: 0; font-size: 9.5pt; }
.guide-sources li { padding: 2mm 0; border-bottom: 1px solid #dee2e6; overflow-wrap: anywhere; }
.guide-title-page code { font-size: 9pt; }
/* Branded title page: the logo on a band in the site header's color. The generic img rule
   above sets height: auto !important and would stretch the logo to the page width, so the
   band rule needs !important too; max-width keeps a long logo inside the content width
   (the SVG keeps its aspect ratio inside the box). */
.guide-title-page.guide-branded { padding-top: 0; }
.guide-brand-band { display: block; width: 100%; box-sizing: border-box; background: #151515 !important; padding: 6mm 8mm; margin: 0 0 50mm 0; print-color-adjust: exact; -webkit-print-color-adjust: exact; }
.guide-brand-band img { display: block; height: 14mm !important; width: auto !important; max-width: 100% !important; }
.guide-internal-banner { margin-top: 10mm; padding: 4mm; border: 2px solid #c92a2a; color: #c92a2a; font-weight: bold; }
.guide-toc { break-after: page; font-family: Helvetica, Arial, sans-serif; }
.guide-toc h2 { font-size: 18pt; }
.guide-toc ol { list-style: none; padding-left: 0; }
.guide-toc ol ol { padding-left: 6mm; }
.guide-toc li { margin: 2px 0; }
.guide-toc > ol > li { margin-top: 3mm; font-weight: bold; }
.guide-toc > ol > li > ol > li { font-weight: normal; }
`;

// Runs in the page: title page, TOC, link rewriting, anchor checks.
function transformDocument(args) {
  const errors = [];
  const doc = document;
  const slug = (text) => String(text).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'page';

  let pageSel = args.pageHeading;
  let sectionSel = args.sectionHeading;
  if (!pageSel) {
    if (doc.querySelector('#content .sect1 > .sectionbody > .sect2 > h3')) {
      pageSel = '#content .sect1 > .sectionbody > .sect2 > h3';
      sectionSel = sectionSel || '#content .sect1 > h2';
    } else if (doc.querySelector('#content .sect1 > h2')) {
      pageSel = '#content .sect1 > h2';
    } else {
      pageSel = 'h1';
    }
  }
  if (args.hide) doc.querySelectorAll(args.hide).forEach((el) => el.remove());

  const pages = Array.from(doc.querySelectorAll(pageSel)).filter((h) => !h.closest('#header'));
  if (!pages.length) {
    return { errors: [`no page headings found (selector ${pageSel})`], pages: 0, links: 0 };
  }
  const sections = sectionSel ? Array.from(doc.querySelectorAll(sectionSel)) : [];

  // Unique ids: page headings first, then every other element.
  const seen = new Map();
  const claim = (el, base) => {
    let id = base;
    let n = 2;
    while (seen.has(id)) id = `${base}--${n++}`;
    seen.set(id, el);
    el.id = id;
    return id;
  };
  pages.forEach((h, i) => claim(h, h.id || `page-${i + 1}-${slug(h.textContent)}`));
  sections.forEach((h, i) => { if (!seen.has(h.id) || seen.get(h.id) !== h) claim(h, h.id || `section-${i + 1}-${slug(h.textContent)}`); });
  doc.querySelectorAll('[id]').forEach((el) => {
    if (seen.get(el.id) === el) return;
    claim(el, el.id);
  });

  // Page containers start on a new sheet.
  for (const h of pages) {
    const container = h.parentElement && (h.parentElement.matches('.sect2, .sect1, section, article') ? h.parentElement : null);
    (container || h).classList.add('guide-page');
  }

  // Experimental labels and wide tables.
  doc.querySelectorAll('.admonitionblock').forEach((block) => {
    const title = block.querySelector('.title');
    if (title && /^\s*Experimental/i.test(title.textContent)) block.classList.add('guide-experimental');
  });
  doc.querySelectorAll('.admonition, .markdown-alert, blockquote').forEach((block) => {
    if (block.classList.contains('experimental') || /^\s*Experimental:/i.test(block.textContent)) block.classList.add('guide-experimental');
  });
  doc.querySelectorAll('table').forEach((t) => {
    const cols = Math.max(0, ...Array.from(t.rows || []).map((r) => r.cells.length));
    if (cols >= 7) t.classList.add('guide-wide');
  });
  doc.querySelectorAll('img').forEach((img) => {
    if (/\.svg(\?|#|$)/i.test(img.getAttribute('src') || '')) img.classList.add('guide-diagram');
  });

  // Links between pages become in-document anchors.
  const pageIds = pages.map((h) => h.id);
  const findPage = (key) => {
    // A link to a section index page (for example user/index.md) is normalized to the
    // bare section key (user) above, but print.lua keys the index page itself as
    // user/index (id user-index). Try the /index variants so those links resolve.
    const variants = [
      key, key.replace(/\//g, '-'), key.split('/').pop(),
      `${key}/index`, `${key}/index`.replace(/\//g, '-'),
    ];
    for (const v of variants) {
      if (pageIds.includes(v)) return v;
      const hit = pages.find((h) => h.dataset.guidePath === v);
      if (hit) return hit.id;
    }
    return null;
  };
  let rewritten = 0;
  doc.querySelectorAll('a[href]').forEach((a) => {
    const href = a.getAttribute('href');
    if (!href || href.startsWith('#') || /^[a-z][a-z0-9+.-]*:/i.test(href)) return;
    const [rawPath, frag] = href.split('#', 2);
    let key = decodeURIComponent(rawPath).replace(/^(\.\.?\/)+/, '').replace(/\/+$/, '/index');
    key = key.replace(/\.(html?|md|adoc)$/i, '').replace(/(^|\/)index$/, '').replace(/\/$/, '') || 'index';
    const pageId = findPage(key);
    if (!pageId) { errors.push(`unresolved link to ${href} ("${a.textContent.trim()}")`); return; }
    let target = pageId;
    if (frag) {
      for (const cand of [`${pageId}:::${frag}`, frag]) {
        if (doc.getElementById(cand)) { target = cand; break; }
      }
    }
    a.setAttribute('href', `#${target}`);
    rewritten++;
  });

  // Title page and table of contents.
  const meta = args.meta;
  const titlePage = doc.createElement('section');
  titlePage.className = meta.brand ? 'guide-title-page guide-branded' : 'guide-title-page';
  // meta.brand.src is a base64 data URL and meta.brand.alt is already escaped.
  const band = meta.brand
    ? `<div class="guide-brand-band"><img src="${meta.brand.src}" alt="${meta.brand.alt}"></div>` : '';
  const rows = meta.sources.map((s) => `<li><strong>${s.id}</strong>${s.ref ? ` (ref ${s.ref})` : ''}`
    + `<br><a href="${s.url}">${s.url}</a><br>commit <code>${s.commit}</code></li>`).join('');
  titlePage.innerHTML = `${band}<h1>${meta.title}</h1>`
    + `<div class="guide-baseline">Verified against the baseline captured on ${meta.captured}</div>`
    + `<div class="guide-sources-label">Pinned sources</div><ul class="guide-sources">${rows}</ul>`
    + (meta.internal ? '<div class="guide-internal-banner">INTERNAL: this document includes the internal part of the guide. '
      + 'Do not share it outside the internal audience.</div>' : '');

  const toc = doc.createElement('nav');
  toc.className = 'guide-toc';
  // Heading text and ids come from the guide; escape them before building markup.
  const esc = (t) => String(t).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  const pageItem = (h) => `<li><a href="#${esc(h.id)}">${esc(h.textContent.trim())}</a></li>`;
  let list = '';
  if (sections.length) {
    const grouped = sections.map((s) => ({ s, pages: [] }));
    const loose = [];
    for (const h of pages) {
      let owner = null;
      for (const g of grouped) {
        if (g.s.compareDocumentPosition(h) & Node.DOCUMENT_POSITION_FOLLOWING) owner = g;
      }
      (owner ? owner.pages : loose).push(h);
    }
    list = loose.map(pageItem).join('') + grouped.map((g) => `<li><a href="#${esc(g.s.id)}">${esc(g.s.textContent.trim())}</a>`
      + `<ol>${g.pages.map(pageItem).join('')}</ol></li>`).join('');
  } else {
    list = pages.map(pageItem).join('');
  }
  toc.innerHTML = `<h2>Contents</h2><ol>${list}</ol>`;
  const body = doc.body;
  body.insertBefore(toc, body.firstChild);
  body.insertBefore(titlePage, toc);
  doc.title = meta.title;

  // Every in-document link must resolve (FR-030d).
  let links = 0;
  doc.querySelectorAll('a[href^="#"]').forEach((a) => {
    const id = decodeURIComponent(a.getAttribute('href').slice(1));
    if (!id) return;
    links++;
    if (!doc.getElementById(id)) errors.push(`link to missing anchor #${id} ("${a.textContent.trim()}")`);
  });
  return { errors, pages: pages.length, links, rewritten, titles: pages.map((h) => h.textContent.trim()) };
}

// Print CSS for the neutral theme: the vendored faces through file: URLs, the Red Hat
// typography, and the red rule under the title band (FR-018, FR-019). `body *` reaches
// elements with their own font (Asciidoctor's serif italic block titles); the heading and
// code rules come later with the same specificity and win. Pseudo-elements (the
// admonition labels) and the page-number footer keep their Helvetica. Chromium embeds the
// variable fonts as unnamed Type3 fonts. Asciidoctor sets headings to weight 300, which
// the stock serif fallback never had; Red Hat Display has it and looks too thin in print,
// so headings use 500, the wordmark's weight.
function neutralCss(brand) {
  const faces = brand.faces.map((f) => `@font-face { font-family: "${f.family}"; `
    + `src: url("${pathToFileURL(join(brand.fonts, f.file)).href}") format("woff2"); `
    + `font-weight: ${f.weight[0]} ${f.weight[1]}; font-style: ${f.style}; }`);
  return `${faces.join('\n')}
body, body * { font-family: "Red Hat Text", sans-serif !important; }
h1, h2, h3, h4, h5, h6, .guide-title-page, .guide-title-page *, .guide-toc, .guide-toc * { font-family: "Red Hat Display", sans-serif !important; }
pre, pre *, code, kbd, samp { font-family: "Red Hat Mono", monospace !important; }
h1, h2, h3, h4, h5, h6, .guide-title-page h1 { font-weight: 500 !important; }
.guide-brand-band { border-bottom: 3px solid #e00 !important; }
`;
}

// The rendered logo and alt text of a branded guide (build/branding/<view>/branding.json),
// and for the neutral theme the vendored font directory and faces.
function readBrand(root, view) {
  const missing = new Error(`branding output missing for the ${view} view; run make check`);
  let info;
  try {
    info = JSON.parse(readFileSync(join(root, 'build', 'branding', view, 'branding.json'), 'utf8'));
  } catch {
    throw missing;
  }
  if (!info || typeof info.logo !== 'string' || !existsSync(info.logo)) throw missing;
  const data = readFileSync(info.logo).toString('base64');
  const brand = { src: `data:image/svg+xml;base64,${data}`, alt: escapeHtml(info.alt || ''), neutral: false };
  if (info.theme === 'neutral') {
    const faces = Array.isArray(info.font_faces) ? info.font_faces : [];
    if (typeof info.fonts !== 'string' || !faces.length
        || !faces.every((f) => existsSync(join(info.fonts, String(f.file))))) throw missing;
    Object.assign(brand, { neutral: true, fonts: info.fonts, faces });
  }
  return brand;
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  const root = resolve(opts.root);
  const html = resolve(opts.html);
  const out = resolve(opts.out);
  const tmp = join(dirname(out), `.${basename(out)}.${process.pid}.tmp`);
  if (!existsSync(html)) throw new Error(`print HTML not found: ${html}`);

  const require = createRequire(join(root, 'package.json'));
  let yaml; let chromium;
  try {
    yaml = require('js-yaml');
    ({ chromium } = require('playwright'));
  } catch (err) {
    throw new Error(`missing Node packages in ${root} (run npm install): ${err.message}`);
  }
  const guide = yaml.load(readFileSync(join(root, 'guide.yaml'), 'utf8')) || {};
  const baseline = yaml.load(readFileSync(join(root, 'baseline.yaml'), 'utf8')) || {};
  const internal = opts.view === 'internal';
  const title = String(guide.title || 'Guide');
  const captured = baseline.captured instanceof Date ? baseline.captured.toISOString().slice(0, 10) : String(baseline.captured || '');
  const meta = {
    brand: guide.branding == null ? null : readBrand(root, opts.view),
    title: escapeHtml(title),
    captured: escapeHtml(captured),
    internal,
    sources: (baseline.sources || []).map((s) => ({
      id: escapeHtml(s.id), url: escapeHtml(webUrl(s.repo || '')), commit: escapeHtml(s.commit || ''), ref: escapeHtml(s.ref || ''),
    })),
  };

  const browser = await chromium.launch();
  try {
    const page = await browser.newPage();
    // Remote fonts, scripts, and styles are blocked so the print is deterministic. Remote
    // images still load (a broken image fails the print); keep guide images local.
    await page.route('**/*', (route) => {
      const url = route.request().url();
      if (/^(file|data|blob|about):/.test(url) || route.request().resourceType() === 'image') return route.continue();
      return route.abort();
    });
    const pageErrors = [];
    page.on('pageerror', (e) => pageErrors.push(String(e)));
    await page.goto(pathToFileURL(html).href, { waitUntil: 'load', timeout: 120000 });
    await page.addStyleTag({ content: PRINT_CSS });
    if (meta.brand && meta.brand.neutral) await page.addStyleTag({ content: neutralCss(meta.brand) });
    const result = await page.evaluate(transformDocument, {
      meta, pageHeading: opts.pageHeading || null, sectionHeading: opts.sectionHeading || null, hide: opts.hide || null,
    });
    if (result.errors.length) {
      const msg = result.errors.map((e) => `  ${e}`).join('\n');
      if (!opts.allowBrokenLinks || result.pages === 0) throw new Error(`the print document has problems:\n${msg}`);
      console.error(`warning: ${result.errors.length} link problem(s):\n${msg}`);
    }
    // Images the transform inserted (the branded logo, a data URL) load asynchronously:
    // wait for every pending image to load or fail (at most 15 s) before judging them.
    await page.evaluate(() => Promise.race([
      Promise.all(Array.from(document.images).filter((i) => !i.complete).map((i) => new Promise((done) => {
        i.addEventListener('load', done, { once: true });
        i.addEventListener('error', done, { once: true });
      }))),
      new Promise((done) => setTimeout(done, 15000)),
    ]));
    const broken = await page.evaluate(() => Array.from(document.images)
      .filter((i) => !i.complete || i.naturalWidth === 0).map((i) => i.getAttribute('src')));
    // A data URL (the branded logo) is shortened: the message must not dump the file.
    const shown = broken.map((src) => (src && src.startsWith('data:') ? `${src.slice(0, 40)}...` : src));
    if (broken.length) throw new Error(`images failed to load: ${shown.join(', ')}`);
    await page.emulateMedia({ media: 'print' });
    // The vendored faces load only when layout first needs them, so document.fonts.ready
    // alone can resolve before they arrive: load every face explicitly, then print. Each
    // face settles on its own within 15 s; a corrupt file rejects (a NetworkError) and a
    // face no @font-face matches resolves empty. Both name the face and its file.
    if (meta.brand && meta.brand.neutral) {
      const failed = await page.evaluate(async (faces) => {
        const timeout = () => new Promise((_, reject) => setTimeout(() => reject(new Error('timed out after 15 s')), 15000));
        const results = await Promise.allSettled(faces.map((f) => Promise.race([
          document.fonts.load(`${f.style} 16px "${f.family}"`), timeout()])));
        return results.map((r, i) => {
          if (r.status === 'rejected') return `${faces[i].family} ${faces[i].style} (${faces[i].file}): ${r.reason && r.reason.message ? r.reason.message : r.reason}`;
          return r.value.length ? null : `${faces[i].family} ${faces[i].style} (${faces[i].file}): no matching @font-face`;
        }).filter(Boolean);
      }, meta.brand.faces);
      if (failed.length) {
        throw new Error(`vendored fonts failed to load: ${failed.join('; ')}; refresh the toolkit and run make check`);
      }
      await page.evaluate(() => document.fonts.ready);
    }
    const label = escapeHtml(title) + (internal ? ' &middot; INTERNAL: do not share outside the internal audience' : '');
    await page.pdf({
      path: tmp,
      format: 'A4',
      printBackground: true,
      preferCSSPageSize: true,
      displayHeaderFooter: true,
      headerTemplate: '<span></span>',
      footerTemplate: `<div style="font-family: Helvetica, Arial, sans-serif; font-size: 8px; color: #495057; width: 100%; `
        + `padding: 0 16mm; display: flex; justify-content: space-between;"><span>${label}</span>`
        + '<span>Page <span class="pageNumber"></span> of <span class="totalPages"></span></span></div>',
      margin: { top: '18mm', right: '16mm', bottom: '20mm', left: '16mm' },
      outline: true,
      tagged: true,
    });
    renameSync(tmp, out);
    console.log(`wrote ${out}: ${result.pages} pages, ${result.links} in-document links (${result.rewritten} rewritten)`);
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  for (const f of [join(dirname(resolve(process.argv[process.argv.indexOf('--out') + 1] || '.')),
    `.${basename(process.argv[process.argv.indexOf('--out') + 1] || '')}.${process.pid}.tmp`)]) {
    try { rmSync(f, { force: true }); } catch { /* nothing to clean */ }
  }
  console.error(`print-pdf: ${err.message}`);
  process.exit(1);
});
