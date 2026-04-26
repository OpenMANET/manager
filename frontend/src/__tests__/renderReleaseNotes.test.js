// =============================================================================
// renderReleaseNotes.test.js — unit tests for the markdown converter
// =============================================================================

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import React from 'react';
import { renderReleaseNotes } from '../util/renderReleaseNotes.js';

function html(body) {
  const { container } = render(
    React.createElement(React.Fragment, null, ...renderReleaseNotes(body)),
  );
  return container.innerHTML;
}

describe('TestRenderReleaseNotes', () => {
  it('returns [] for null/empty/whitespace input', () => {
    expect(renderReleaseNotes(null)).toEqual([]);
    expect(renderReleaseNotes(undefined)).toEqual([]);
    expect(renderReleaseNotes('')).toEqual([]);
    expect(renderReleaseNotes('   \n  \n')).toEqual([]);
  });

  it('renders #/##/### headings', () => {
    const out = html('# h1\n\n## h2\n\n### h3');
    expect(out).toContain('<h1>h1</h1>');
    expect(out).toContain('<h2>h2</h2>');
    expect(out).toContain('<h3>h3</h3>');
  });

  it('renders paragraphs separated by blank lines', () => {
    const out = html('first paragraph.\n\nsecond paragraph.');
    expect(out).toContain('<p>first paragraph.</p>');
    expect(out).toContain('<p>second paragraph.</p>');
  });

  it('renders dash and star bullet lists', () => {
    const out = html('- item one\n- item two\n* item three');
    expect(out).toContain('<ul>');
    expect(out).toContain('<li>item one</li>');
    expect(out).toContain('<li>item two</li>');
    expect(out).toContain('<li>item three</li>');
  });

  it('renders inline bold, italic, and code', () => {
    const out = html('mix **bold** and *italic* and `code` here.');
    expect(out).toContain('<strong>bold</strong>');
    expect(out).toContain('<em>italic</em>');
    expect(out).toContain('<code>code</code>');
  });

  it('renders external links with target=_blank rel=noopener noreferrer', () => {
    const out = html('see [GitHub](https://github.com/x/y)');
    expect(out).toContain('href="https://github.com/x/y"');
    expect(out).toContain('target="_blank"');
    expect(out).toContain('rel="noopener noreferrer"');
    expect(out).toContain('>GitHub</a>');
  });

  it('renders mailto: links', () => {
    const out = html('contact [us](mailto:foo@example.com).');
    expect(out).toContain('href="mailto:foo@example.com"');
  });

  it('drops javascript: URLs and renders the label as plain text', () => {
    const out = html('[click](javascript:alert(1))');
    expect(out).not.toContain('<a');
    expect(out).not.toContain('javascript:');
    expect(out).toContain('click');
  });

  it('escapes raw HTML — script tags become text', () => {
    const out = html('hello <script>alert(1)</script> world');
    expect(out).not.toContain('<script>');
    expect(out).toContain('&lt;script&gt;');
  });

  it('escapes <img onerror=...> markup as text', () => {
    const out = html('<img src=x onerror=alert(1)>');
    expect(out).not.toContain('<img');
    expect(out).toContain('&lt;img');
  });

  it('passes through fenced code blocks as escaped paragraph text', () => {
    const out = html('```js\nconsole.log(1)\n```');
    expect(out).not.toContain('<pre>');
    expect(out).not.toContain('<code>');
    expect(out).toContain('```js');
  });

  it('does not autolink bare URLs', () => {
    const out = html('see https://example.com for details.');
    expect(out).not.toContain('<a');
    expect(out).toContain('https://example.com');
  });

  it('renders a mixed body with headings, lists, paragraphs, and inline marks', () => {
    const body = [
      '# OpenMANET 1.9.0',
      '',
      'This release adds **streaming** events and *mesh* fixes.',
      '',
      '## New',
      '',
      '- BLOS streaming RPC.',
      '- Configurable interval.',
      '',
      '## Fixed',
      '',
      '- Build fixes for `linux/mipsle`.',
      '',
      'Full notes at [GitHub](https://example.com/v1.9.0).',
    ].join('\n');
    const out = html(body);

    expect(out).toContain('<h1>OpenMANET 1.9.0</h1>');
    expect(out).toContain('<h2>New</h2>');
    expect(out).toContain('<h2>Fixed</h2>');
    expect(out).toContain('<strong>streaming</strong>');
    expect(out).toContain('<em>mesh</em>');
    expect(out).toContain('<code>linux/mipsle</code>');
    expect(out).toContain('href="https://example.com/v1.9.0"');
    expect(out.match(/<ul>/g)?.length ?? 0).toBe(2);
  });
});
