import { createHash } from 'node:crypto';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { temp } from './helpers.mjs';

export function assetFixture(t, overrides = {}) {
  const directory = temp(t);
  const bodies = {
    'app.css': ':root{--official-fixture:1}',
    'tree.css': '.md{padding:1px}',
    'fonts.css': '.md{font-family:sans-serif}',
    'white.css': '.md{color:rgb(1,2,3)}',
    'dark.css': '.md{color:rgb(4,5,6)}',
    'icons.svg': '<svg xmlns="http://www.w3.org/2000/svg"/>',
    'font.woff2': Buffer.from([97, 0, 98, 255, 10]),
    'licenses/MIT.txt': '試験用の原文',
    ...overrides,
  };
  const files = Object.fromEntries(
    Object.entries(bodies).map(([path, body]) => [
      path,
      { size: Buffer.byteLength(body), sha256: createHash('sha256').update(body).digest('hex') },
    ]),
  );
  const styles = {
    light: ['app.css', 'tree.css', 'fonts.css', 'white.css'],
    dark: ['app.css', 'tree.css', 'fonts.css', 'dark.css'],
  };
  const lock = { version: 'test-1', files, styles, sprite: 'icons.svg' };
  const bytes = JSON.stringify(lock) + '\n',
    hash = createHash('sha256').update(bytes).digest('hex');
  const id = lock.version + '-' + hash.slice(0, 12),
    bundle = join(directory, id),
    lockPath = join(directory, 'lock.json');
  writeFileSync(lockPath, bytes);
  const manifest = { ...structuredClone(lock), schema: 1, id, lock_sha256: hash };
  const write = (name, body) => {
    const target = join(bundle, name);
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, body);
  };
  for (const [path, body] of Object.entries(bodies)) write(path, body);
  const save = () => write('install.json', JSON.stringify(manifest));
  return { directory, bundle, id, lockPath, manifest, write, save, bodies };
}
