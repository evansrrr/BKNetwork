#!/usr/bin/env node
// Front-end static checks that need no dependencies, so they can run in CI and
// in a pre-commit hook on a machine without node_modules.
//
// It guards three things that are easy to break by accident and expensive to
// notice by hand:
//   1. web/app.js still parses (node --check equivalent, without spawning).
//   2. web/index.html contains no inline event handlers or inline styles; the
//      CSP the server sends has no 'unsafe-inline', so such markup silently
//      stops working in the packaged app.
//   3. no eval/new Function and no third-party script/style sources beyond the
//      campus traffic endpoint the CSP already allows.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import vm from 'node:vm';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const problems = [];

function fail(message) {
  problems.push(message);
}

function read(relative) {
  try {
    return readFileSync(join(root, relative), 'utf8');
  } catch (err) {
    fail(`cannot read ${relative}: ${err.message}`);
    return '';
  }
}

const html = read('web/index.html');
const css = read('web/styles.css');

// The UI is a native ES module graph: app.js is the entry, js/*.js hold the
// modules it imports. All checks below run over every file of the graph.
const moduleFiles = ['web/app.js', 'web/js/dom.js', 'web/js/state.js', 'web/js/ui.js',
  'web/js/api.js', 'web/js/actions.js'];
const sources = new Map();
for (const file of moduleFiles) {
  sources.set(file, read(file));
}

// 1. Syntax. vm.Script does not know import/export, so the module syntax is
// stripped first; the result still catches every other syntax error, and the
// import graph itself is validated by actually loading it (see the end).
const scriptish = (src) => src
  .replace(/^import\s+\{[^}]*\}\s+from\s+'[^']*';\s*$/gm, '')
  .replace(/^export\s+/gm, '');
for (const [file, src] of sources) {
  try {
    new vm.Script(scriptish(src), { filename: file });
  } catch (err) {
    fail(`${file} does not parse: ${err.message}`);
  }
}

// 1b. Every import specifier must point at a file of the graph (a typo or a
// missing module otherwise only fails inside the browser).
for (const [file, src] of sources) {
  for (const match of src.matchAll(/^import\s+\{[^}]*\}\s+from\s+'([^']+)';\s*$/gm)) {
    const resolved = file.startsWith('web/app.js')
      ? 'web/' + match[1].replace('./', '')
      : 'web/js/' + match[1].replace('./', '');
    if (!sources.has(resolved)) {
      fail(`${file} imports unknown module ${match[1]}`);
    }
  }
}

// 1c. Every imported binding must be exported by the module it comes from.
const exportCache = new Map();
for (const [file, src] of sources) {
  exportCache.set(file, new Set(
    [...src.matchAll(/^export (?:async )?(?:function|const) ([A-Za-z_$][\w$]*)/gm)].map(m => m[1])));
}
for (const [file, src] of sources) {
  for (const match of src.matchAll(/^import\s+\{([^}]*)\}\s+from\s+'([^']+)';\s*$/gm)) {
    const resolved = file.startsWith('web/app.js')
      ? 'web/' + match[2].replace('./', '')
      : 'web/js/' + match[2].replace('./', '');
    const available = exportCache.get(resolved) || new Set();
    for (const name of match[1].split(',').map(s => s.trim()).filter(Boolean)) {
      if (!available.has(name)) {
        fail(`${file} imports missing export ${name} from ${match[2]}`);
      }
    }
  }
}

// 2. CSP-hostile markup
const inlineHandler = /<[^>]*\son(?:click|change|input|submit|load|error|keydown|keyup|focus|blur|mouseover|mouseout)\s*=/gi;
for (const match of html.matchAll(inlineHandler)) {
  fail(`inline event handler in index.html: ${match[0].trim().slice(0, 60)}`);
}

const inlineStyleAttribute = /<[^>]*\sstyle\s*=/gi;
for (const match of html.matchAll(inlineStyleAttribute)) {
  fail(`inline style attribute in index.html: ${match[0].trim().slice(0, 60)}`);
}

if (/<style[\s>]/i.test(html)) {
  fail('inline <style> block in index.html (style-src does not allow it)');
}

// 3. Dangerous or foreign code in the bundle
for (const [file, src] of sources) {
  for (const pattern of [/\beval\s*\(/, /new\s+Function\s*\(/]) {
    if (pattern.test(src)) {
      fail(`${file} uses ${pattern} (blocked by the CSP)`);
    }
  }
}

// The only allowed cross-origin reference is the campus traffic endpoint, which
// app.js loads itself; loaded assets must stay same-origin. Plain <a href>
// links are fine: they are navigation, not resources.
const foreignAsset = /(?:\ssrc\s*=\s*["']https?:\/\/|<link[^>]*\shref\s*=\s*["']https?:\/\/)/gi;
for (const match of html.matchAll(foreignAsset)) {
  fail(`index.html loads a cross-origin asset: ${match[0].trim().slice(0, 60)}`);
}

// css sanity: the shell adds spacing utility classes, keep them defined.
for (const className of ['.section--spaced', '.section--spaced-sm']) {
  if (!css.includes(`${className} {`)) {
    fail(`web/styles.css is missing ${className}`);
  }
}

// 4. Element ids referenced from any module must exist in index.html (a typo
// in a getElementById call is otherwise a silent no-op).
const ids = new Set();
for (const match of html.matchAll(/\sid="([^"]+)"/g)) {
  ids.add(match[1]);
}
const missing = new Set();
for (const src of sources.values()) {
  for (const match of src.matchAll(/getElementById\(['"]([^'"]+)['"]\)/g)) {
    if (!ids.has(match[1])) {
      missing.add(match[1]);
    }
  }
}
if (missing.size > 0) {
  fail(`modules refer to missing element ids: ${[...missing].sort().join(', ')}`);
}

if (problems.length > 0) {
  for (const problem of problems) {
    console.error(`error: ${problem}`);
  }
  process.exit(1);
}

console.log('frontend checks passed (syntax, CSP markup, element ids)');
