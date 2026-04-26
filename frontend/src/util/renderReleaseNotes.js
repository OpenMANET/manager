// =============================================================================
// renderReleaseNotes.js — minimal Markdown → React converter
// =============================================================================
//
// Turns a constrained subset of Markdown into a flat array of React elements,
// one per top-level block. The supported subset matches what GitHub release
// bodies actually use:
//
//   - `#`, `##`, `###` headings.
//   - `- ` / `* ` bullet lists (single level, no nesting).
//   - Paragraphs separated by blank lines.
//   - Inline: `**bold**`, `*italic*`, `` `code` ``, `[text](url)`.
//
// Anything outside the subset (tables, images, blockquotes, fenced code
// blocks, nested lists, raw HTML) passes through as escaped plain text.
// External links are emitted with target="_blank" rel="noopener noreferrer".
// `javascript:` and other non-http(s)/mailto URLs are stripped — the link
// renders as plain text instead.
//
// XSS safety: the output is built from React elements only, so no
// dangerouslySetInnerHTML is needed and arbitrary HTML in the input cannot
// reach the DOM as live markup.

import React from 'react';

const SAFE_URL_RE = /^(https?:\/\/|mailto:)/i;

export function renderReleaseNotes(body) {
  if (typeof body !== 'string' || body.trim() === '') {
    return [];
  }

  const blocks = splitBlocks(body);
  const out = [];
  let listBuffer = null;

  for (let i = 0; i < blocks.length; i++) {
    const block = blocks[i];

    const heading = matchHeading(block);
    if (heading) {
      flushList(out, listBuffer);
      listBuffer = null;
      const Tag = `h${heading.level}`;
      out.push(
        React.createElement(Tag, { key: `h-${i}` }, renderInline(heading.text, `h-${i}`)),
      );
      continue;
    }

    const items = matchList(block);
    if (items) {
      if (listBuffer == null) listBuffer = [];
      for (const item of items) {
        listBuffer.push(item);
      }
      continue;
    }

    flushList(out, listBuffer);
    listBuffer = null;

    out.push(
      React.createElement(
        'p',
        { key: `p-${i}` },
        renderInline(block, `p-${i}`),
      ),
    );
  }

  flushList(out, listBuffer);
  return out;
}

function flushList(out, buffer) {
  if (!buffer || buffer.length === 0) return;
  const items = buffer.map((text, i) =>
    React.createElement('li', { key: `li-${i}` }, renderInline(text, `li-${i}`)),
  );
  out.push(
    React.createElement('ul', { key: `ul-${out.length}` }, items),
  );
}

// splitBlocks splits the body on blank lines, preserving list items as a
// single block when they appear consecutively (no blank line between).
function splitBlocks(body) {
  const lines = body.replace(/\r\n?/g, '\n').split('\n');
  const blocks = [];
  let cur = [];

  for (const line of lines) {
    if (line.trim() === '') {
      if (cur.length > 0) {
        blocks.push(cur.join('\n'));
        cur = [];
      }
      continue;
    }
    cur.push(line);
  }
  if (cur.length > 0) blocks.push(cur.join('\n'));
  return blocks;
}

function matchHeading(block) {
  const m = /^(#{1,3})\s+(.+?)\s*$/.exec(block);
  if (!m) return null;
  if (block.includes('\n')) return null; // single-line headings only
  return { level: m[1].length, text: m[2] };
}

// matchList returns an array of item texts when *every* line is a `- ` or
// `* ` bullet. Mixed-content blocks fall back to paragraphs.
function matchList(block) {
  const lines = block.split('\n');
  const items = [];
  for (const line of lines) {
    const m = /^\s*[-*]\s+(.*)$/.exec(line);
    if (!m) return null;
    items.push(m[1]);
  }
  return items.length > 0 ? items : null;
}

// renderInline walks **bold** → *italic* → `code` → [link](url) and returns
// a flat React fragment array. Each pass replaces the matched span with its
// element form and recurses on the remaining text.
function renderInline(text, keyPrefix) {
  return inlinePass(text, keyPrefix, 0);
}

const PATTERNS = [
  {
    re: /\*\*([^*]+)\*\*/,
    build: (m, key, recurse) =>
      React.createElement('strong', { key }, recurse(m[1])),
  },
  {
    re: /(?<!\*)\*([^*\n]+)\*(?!\*)/,
    build: (m, key, recurse) =>
      React.createElement('em', { key }, recurse(m[1])),
  },
  {
    re: /`([^`\n]+)`/,
    build: (m, key) => React.createElement('code', { key }, m[1]),
  },
  {
    re: /\[([^\]]+)\]\(([^)]+)\)/,
    build: (m, key, recurse) => {
      const url = m[2].trim();
      if (!SAFE_URL_RE.test(url)) {
        // Drop unsafe URL — render the label as plain text.
        return recurse(m[1]);
      }
      return React.createElement(
        'a',
        { key, href: url, target: '_blank', rel: 'noopener noreferrer' },
        recurse(m[1]),
      );
    },
  },
];

function inlinePass(text, keyPrefix, depth) {
  if (typeof text !== 'string') return text;
  if (text === '') return [];
  if (depth > 4) return [text]; // bound recursion

  for (let p = 0; p < PATTERNS.length; p++) {
    const { re, build } = PATTERNS[p];
    const m = re.exec(text);
    if (!m) continue;

    const before = text.slice(0, m.index);
    const after = text.slice(m.index + m[0].length);
    const key = `${keyPrefix}-${p}-${m.index}`;
    const recurse = (s) => inlinePass(s, `${key}r`, depth + 1);

    const out = [];
    if (before) out.push(...flatten(inlinePass(before, `${key}b`, depth + 1)));
    out.push(build(m, key, recurse));
    if (after) out.push(...flatten(inlinePass(after, `${key}a`, depth + 1)));
    return out;
  }

  return [text];
}

function flatten(value) {
  if (Array.isArray(value)) return value;
  return [value];
}
