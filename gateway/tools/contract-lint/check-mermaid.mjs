import { readFile, readdir } from 'node:fs/promises';
import { join, extname } from 'node:path';
import process from 'node:process';
import { JSDOM } from 'jsdom';

const root = process.argv[2];
if (!root) {
  throw new Error('usage: node check-mermaid.mjs <planning-root>');
}

// Parsing in Node has no browser DOM. "loose" avoids the browser-only
// DOMPurify rendering pass; this tool parses trusted repository diagrams and
// never renders untrusted content.
const dom = new JSDOM('<!doctype html><html><body></body></html>');
globalThis.window = dom.window;
globalThis.document = dom.window.document;
// Import after installing window: dompurify otherwise exports its factory,
// while Mermaid expects the initialized sanitizer instance in Node.
const { default: DOMPurify } = await import('dompurify');
globalThis.DOMPurify = DOMPurify;
const { default: mermaid } = await import('mermaid');
mermaid.initialize({ startOnLoad: false, securityLevel: 'loose' });

async function markdownFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const nested = await Promise.all(entries.map(async (entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return markdownFiles(path);
    return extname(entry.name) === '.md' ? [path] : [];
  }));
  return nested.flat();
}

const block = /```mermaid[\t ]*\r?\n([\s\S]*?)```/g;
let count = 0;
for (const file of await markdownFiles(root)) {
  const contents = await readFile(file, 'utf8');
  for (const match of contents.matchAll(block)) {
    try {
      await mermaid.parse(match[1]);
    } catch (error) {
      throw new Error(`${file}: invalid Mermaid diagram: ${error.message}`, { cause: error });
    }
    count += 1;
  }
}
console.log(`Mermaid diagrams: ${count} valid`);
