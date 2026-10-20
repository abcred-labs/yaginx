#!/usr/bin/env node

import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const site = path.join(root, 'yaginx-docs-site');
const basePath = '/yaginx';
const files = [
  'README.md',
  'CONTRIBUTING.md',
  'SECURITY.md',
  'docs',
  'yaginx-docs-site/docs',
];
const extensions = ['', '.md', '.mdx', '/index.md', '/index.mdx', '/README.md'];

function collectMarkdown(entry) {
  const absolute = path.join(root, entry);
  if (!fs.existsSync(absolute)) return [];
  const stat = fs.statSync(absolute);
  if (stat.isFile()) return /\.mdx?$/.test(absolute) ? [absolute] : [];
  return fs.readdirSync(absolute, {withFileTypes: true}).flatMap((child) =>
    collectMarkdown(path.join(entry, child.name)),
  );
}

function firstExistingFile(base) {
  return extensions
    .map((extension) => base + extension)
    .find((candidate) => fs.existsSync(candidate) && fs.statSync(candidate).isFile());
}

// Site-absolute links (/docs/..., /img/...) resolve against the Docusaurus
// site, optionally prefixed with its baseUrl.
function resolveAbsolute(target) {
  const trimmed = target.startsWith(basePath + '/') ? target.slice(basePath.length) : target;
  if (trimmed === '/docs' || trimmed.startsWith('/docs/')) {
    return firstExistingFile(path.join(site, trimmed.replace(/\/$/, '')));
  }
  const asset = path.join(site, 'static', trimmed);
  return fs.existsSync(asset) ? asset : undefined;
}

function slugify(heading) {
  return heading
    .trim()
    .toLowerCase()
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/[`*_~]/g, '')
    .replace(/[^\p{L}\p{N}\s-]/gu, '')
    .replace(/\s/g, '-');
}

const anchorCache = new Map();
function anchorsOf(file) {
  if (!anchorCache.has(file)) {
    const anchors = new Set();
    let fenced = false;
    for (const line of fs.readFileSync(file, 'utf8').split(/\r?\n/)) {
      if (/^\s*(```|~~~)/.test(line)) fenced = !fenced;
      if (fenced) continue;
      for (const explicit of line.matchAll(/<a\s+(?:id|name)="([^"]+)"/g)) anchors.add(explicit[1].toLowerCase());
      const heading = /^#{1,6}\s+(.*?)(?:\s+\{#([^}]+)\})?\s*#*\s*$/.exec(line);
      if (heading) anchors.add((heading[2] ?? slugify(heading[1])).toLowerCase());
    }
    anchorCache.set(file, anchors);
  }
  return anchorCache.get(file);
}

// Returns {file, fragment} for a checkable local target, or null for external links.
function localTarget(source, target) {
  if (target.startsWith('#')) return {file: source, fragment: target.slice(1)};
  if (/^[a-z][a-z0-9+.-]*:/i.test(target)) return null;
  const [pathPart, ...rest] = target.split('#');
  const withoutQuery = pathPart.split('?', 1)[0];
  if (!withoutQuery) return null;
  const decoded = decodeURIComponent(withoutQuery);
  const file = decoded.startsWith('/')
    ? resolveAbsolute(decoded)
    : path.resolve(path.dirname(source), decoded);
  return {file, fragment: rest.join('#')};
}

const markdownFiles = [...new Set(files.flatMap(collectMarkdown))];
const missing = [];
const linkPattern = /!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g;

for (const source of markdownFiles) {
  // Links inside fenced code samples are examples, not navigation.
  const content = fs.readFileSync(source, 'utf8').replace(/```[\s\S]*?```/g, '');
  for (const match of content.matchAll(linkPattern)) {
    const target = match[1].replace(/^<|>$/g, '');
    const resolved = localTarget(source, target);
    if (!resolved) continue;
    const label = `${path.relative(root, source)} -> ${target}`;
    if (!resolved.file || !fs.existsSync(resolved.file)) {
      missing.push(label);
      continue;
    }
    if (resolved.fragment && /\.mdx?$/.test(resolved.file)) {
      const wanted = decodeURIComponent(resolved.fragment).toLowerCase();
      if (!anchorsOf(resolved.file).has(wanted)) missing.push(`${label} (missing anchor)`);
    }
  }
}

if (missing.length > 0) {
  console.error('Broken local documentation links:');
  for (const item of missing) console.error(`- ${item}`);
  process.exit(1);
}

console.log(`Documentation link check passed (${markdownFiles.length} Markdown files).`);
