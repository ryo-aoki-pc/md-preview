import assert from 'node:assert/strict';
import { renameSync, unlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import test from 'node:test';
import { watchFile } from '../lib/watch.mjs';
import { temp, until } from './helpers.mjs';

test('見張りは書き換えをまとめ、別ファイルの変更には反応しない', async (t) => {
  const root = temp(t);
  const file = join(root, 'file.md');
  writeFileSync(file, 'first');
  let count = 0;
  const stop = watchFile(file, () => count++);
  t.after(stop);
  await delay(50);
  writeFileSync(join(root, 'other.md'), 'other');
  await delay(250);
  assert.equal(count, 0);
  writeFileSync(file, 'second');
  writeFileSync(file, 'third');
  writeFileSync(file, 'fourth');
  await until(() => count > 0);
  await delay(250);
  assert.equal(count, 1);
  stop();
  writeFileSync(file, 'after stop');
  await delay(200);
  assert.equal(count, 1);
});

test('見張りは別名で書いてから置き換える保存にも追従する', async (t) => {
  const root = temp(t);
  const file = join(root, 'file.md');
  writeFileSync(file, 'first');
  let count = 0;
  const stop = watchFile(file, () => count++);
  t.after(stop);
  await delay(50);
  writeFileSync(join(root, 'new.md'), 'replacement');
  renameSync(join(root, 'new.md'), file);
  await until(() => count >= 1);
  await delay(150);
  writeFileSync(file, 'after replacement');
  await until(() => count >= 2);
});

test('削除中は通知せず、再作成したら再び通知する', async (t) => {
  const root = temp(t);
  const file = join(root, 'file.md');
  writeFileSync(file, 'first');
  let count = 0;
  const stop = watchFile(file, () => count++);
  t.after(stop);
  await delay(50);
  unlinkSync(file);
  await delay(1200);
  assert.equal(count, 0);
  writeFileSync(file, 'recreated');
  await until(() => count > 0);
});

test('通知が届かなくても定期的な stat で変更を見つける', async (t) => {
  const fs = await import('node:fs');
  const { syncBuiltinESMExports } = await import('node:module');
  const { EventEmitter } = await import('node:events');
  const root = temp(t);
  const file = join(root, 'file.md');
  writeFileSync(file, 'first');
  let count = 0;
  const silent = new EventEmitter();
  silent.close = () => {};
  t.mock.method(fs.default, 'watch', () => silent);
  syncBuiltinESMExports();
  let stop;
  try {
    stop = watchFile(file, () => count++);
  } finally {
    t.mock.restoreAll();
    syncBuiltinESMExports();
  }
  t.after(stop);
  await delay(50);
  writeFileSync(file, 'changed without event');
  await until(() => count > 0, 'stat の比較で変更を見つける', 3000);
  assert.equal(count, 1);
});

test('削除専用の通知を指定した場合だけ、消えたことも1回知らせる', async (t) => {
  const root = temp(t);
  const file = join(root, 'file.md');
  writeFileSync(file, 'first');
  let changed = 0, missing = 0;
  const stop = watchFile(file, () => changed++, { onMissing: () => missing++ });
  t.after(stop);
  await delay(50);
  unlinkSync(file);
  await until(() => missing === 1);
  assert.equal(changed, 0);
  await delay(150);
  assert.equal(missing, 1);
  writeFileSync(file, 'again');
  await until(() => changed === 1);
});
