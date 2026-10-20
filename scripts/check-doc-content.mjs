#!/usr/bin/env node
// Fails when Docusaurus starter content remains in the public documentation.
// Single source for ci.yml, release.yml and docs.yml.

import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const site = path.join(root, 'yaginx-docs-site');
const targets = ['docs', 'src', 'docusaurus.config.ts', 'README.md'];
const forbidden = /Dinosaurs are cool|My Project|facebook\/docusaurus|Stack Overflow.*docusaurus|Discord.*docusaurus|Twitter.*docusaurus|Create a (Blog|Document|Page)/;

function collect(entry) {
  const absolute = path.join(site, entry);
  if (!fs.existsSync(absolute)) return [];
  if (fs.statSync(absolute).isFile()) return [absolute];
  return fs.readdirSync(absolute, {withFileTypes: true}).flatMap((child) =>
    collect(path.join(entry, child.name)),
  );
}

const hits = [];
for (const file of targets.flatMap(collect)) {
  fs.readFileSync(file, 'utf8').split(/\r?\n/).forEach((line, index) => {
    if (forbidden.test(line)) hits.push(`${path.relative(root, file)}:${index + 1}: ${line.trim()}`);
  });
}

if (hits.length > 0) {
  console.error('Docusaurus starter content remains in the public documentation:');
  for (const hit of hits) console.error(`- ${hit}`);
  process.exit(1);
}
console.log('Documentation content check passed.');
