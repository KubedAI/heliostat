/**
 * Exports the home page's animated architecture diagram as standalone SVG files for the README
 * (GitHub keeps SVG animations). Theme tokens become concrete colors and the diagram's CSS is
 * inlined, producing a light and a dark variant.
 *
 * Run with `make diagram` (from the repository root).
 */
import fs from 'node:fs';
import path from 'node:path';
import { renderToStaticMarkup } from 'react-dom/server';
import { ArchitectureDiagram } from '../components/home/architecture-diagram';

const root = path.resolve(import.meta.dirname, '..');
const outDir = path.resolve(root, '..', 'docs', 'assets');
const css = fs.readFileSync(path.join(root, 'app', 'globals.css'), 'utf8');

const SANS = "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif";
const MONO = "ui-monospace, 'SFMono-Regular', 'SF Mono', Menlo, Consolas, monospace";

/** Reads `--name: value;` declarations from the first block matching `selector {`. */
function tokens(selector: string): Record<string, string> {
  const start = css.indexOf(selector);
  const block = css.slice(css.indexOf('{', start) + 1, css.indexOf('}', start));
  const out: Record<string, string> = {};
  for (const match of block.matchAll(/(--[\w-]+):\s*([^;]+);/g)) out[match[1]] = match[2].trim();
  out['--font-sans'] = SANS;
  out['--font-mono'] = MONO;
  return out;
}

/** Removes every at-rule block (media queries, keyframes) so only top-level rules remain. */
function topLevel(source: string): string {
  let out = '';
  let i = 0;
  while (i < source.length) {
    const at = source.indexOf('@', i);
    if (at === -1) return out + source.slice(i);
    out += source.slice(i, at);
    let depth = 0;
    let j = source.indexOf('{', at);
    for (; j < source.length; j++) {
      if (source[j] === '{') depth++;
      else if (source[j] === '}' && --depth === 0) break;
    }
    i = j + 1;
  }
  return out;
}

/** The diagram's own rules (not page layout ones), plus its keyframes. */
function diagramRules(): string {
  const rules: string[] = [];
  for (const match of topLevel(css).matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const selector = match[1].trim();
    const pageOnly = /\.arch-legend|\.arch-svg/.test(selector);
    if (/\.arch-|\.ray-worker/.test(selector) && !pageOnly) rules.push(`${selector} {${match[2]}}`);
  }
  for (const name of ['arch-flow', 'hub-glow', 'ray-worker']) {
    const at = css.indexOf(`@keyframes ${name}`);
    const end = css.indexOf('}\n}', at) + 3;
    rules.push(css.slice(at, end));
  }
  rules.push(
    '@media (prefers-reduced-motion: reduce) { .arch-flow, .arch-hub-glow, .ray-worker { animation: none; } .arch-packet { display: none; } }',
  );
  return rules.join('\n');
}

const LEGEND: [string, string][] = [
  ['var(--blue)', 'RayJob'],
  ['var(--coral)', 'RayCluster'],
  ['var(--mint)', 'RayService'],
  ['var(--ink-faint)', 'on-prem extension'],
];

function legend(y: number): string {
  let x = 380;
  return LEGEND.map(([color, label]) => {
    const item = `<circle cx="${x}" cy="${y}" r="6" fill="${color}"/><text x="${x + 12}" y="${y + 4}" class="arch-mono">${label}</text>`;
    x += 32 + label.length * 8;
    return item;
  }).join('');
}

function exportVariant(theme: 'light' | 'dark') {
  const light = tokens(':root {');
  const values = theme === 'light' ? light : { ...light, ...tokens(":root[data-theme='dark'] {") };
  const figure = renderToStaticMarkup(<ArchitectureDiagram />);
  let svg = figure.slice(figure.indexOf('<svg'), figure.lastIndexOf('</svg>') + 6);

  // Room for a legend under the diagram, and a themed background card.
  svg = svg
    .replace(
      'viewBox="0 0 1200 680"',
      'viewBox="0 0 1200 720" xmlns="http://www.w3.org/2000/svg" width="1200" height="720"',
    )
    .replace(
      /<svg([^>]*)>/,
      (open) =>
        `${open}<style>${diagramRules()} text { font-family: var(--font-sans); }</style>` +
        `<rect x="0" y="0" width="1200" height="720" fill="var(--surface)" rx="12"/>`,
    );
  // Nested logo <svg>s close earlier; the legend belongs before the outer closing tag.
  const end = svg.lastIndexOf('</svg>');
  svg = `${svg.slice(0, end)}${legend(700)}${svg.slice(end)}`;

  const resolved = svg.replace(
    /var\((--[\w-]+)\)/g,
    (_, name: string) => values[name] ?? `var(${name})`,
  );
  const file = path.join(outDir, `architecture-${theme}.svg`);
  fs.writeFileSync(file, `${resolved}\n`);
  console.log(`wrote ${path.relative(path.resolve(root, '..'), file)}`);
}

fs.mkdirSync(outDir, { recursive: true });
exportVariant('light');
exportVariant('dark');
