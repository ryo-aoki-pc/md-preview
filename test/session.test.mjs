import assert from 'node:assert/strict';
import { mkdirSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import test from 'node:test';
import { createSession } from '../lib/session.mjs';
import { temp, until } from './helpers.mjs';

test('描画中の入力は最新の1件にまとまり、古い結果を送らない', async (t) => {
  const rendered = [];
  const session = createSession({ onRender: (data) => rendered.push(data) });
  t.after(() => session.close());
  for (const [index, text] of ['# 最初', '# 中間', '# 最新'].entries())
    session.setDocument({ path: null, name: '無名.md', text, rev: index + 1 });
  await until(() => rendered.length > 0);
  assert.equal(rendered.length, 1);
  assert.equal(rendered[0].rev, 3);
  assert.match(rendered[0].html, /最新/);
  assert.equal(rendered[0].file, '無名.md');
  assert.equal(rendered[0].dir, '');
  assert.equal(session.root(), null);
});

test('時間切れで worker を止め、次の本文では描画できる', { timeout: 10000 }, async (t) => {
  const rendered = [];
  const session = createSession({ onRender: (data) => rendered.push(data), renderTimeoutMs: 1000 });
  t.after(() => session.close());
  const heavy = '>'.repeat(200000) + ' x\n';
  session.setDocument({ path: null, name: '重い.md', text: heavy, rev: 1 });
  await until(() => rendered.length === 1);
  assert.match(rendered[0].error, /時間切れ/);
  assert.equal(rendered[0].markdown, heavy);
  session.setDocument({ path: null, name: '軽い.md', text: '# 復帰\n', rev: 2 });
  await until(() => rendered.length === 2);
  assert.equal(rendered[1].error, undefined);
  assert.match(rendered[1].html, /復帰/);
});

test('古い描画の時間切れは最新の本文の結果を上書きしない', { timeout: 10000 }, async (t) => {
  const rendered = [];
  const session = createSession({ onRender: (data) => rendered.push(data), renderTimeoutMs: 1000 });
  t.after(() => session.close());
  session.setDocument({ path: null, name: '前.md', text: '>'.repeat(200000) + ' x\n', rev: 1 });
  session.setDocument({ path: null, name: '後.md', text: '# 新しい本文', rev: 2 });
  await until(() => rendered.length > 0);
  assert.equal(rendered.length, 1);
  assert.equal(rendered[0].rev, 2);
  assert.match(rendered[0].html, /新しい本文/);
});

test('大きさは UTF-8 のバイト数で制限し、原文を保持する', async (t) => {
  const rendered = [];
  const session = createSession({ maxBytes: 5, onRender: (data) => rendered.push(data) });
  t.after(() => session.close());
  session.setDocument({ path: null, name: '大.md', text: '日本', rev: 1 });
  assert.match(rendered[0].error, /大きすぎ/);
  assert.equal(rendered[0].markdown, '日本');
  session.setDocument({ path: null, name: '小.md', text: 'ok', rev: 2 });
  await until(() => rendered.length === 2);
  assert.match(rendered[1].html, /ok/);
});

test('git のトップ・.git ファイル・フォルダー・指定ルートから file と dir を作る', async (t) => {
  const base = temp(t);
  const repo = join(base, 'repo');
  const docs = join(repo, 'docs', '日本語');
  mkdirSync(docs, { recursive: true });
  writeFileSync(join(repo, '.git'), 'gitdir: elsewhere');
  const path = join(docs, '文書.md');
  writeFileSync(path, 'text');
  for (const [override, expectedRoot, expectedFile, expectedDir] of [
    [undefined, repo, 'docs/日本語/文書.md', 'docs/日本語/'],
    [docs, docs, '文書.md', ''],
  ]) {
    const session = createSession({ root: override });
    t.after(() => session.close());
    session.setDocument({ path, text: '# 文書', rev: 'r1' });
    assert.equal(session.root(), realpathSync.native(expectedRoot));
    await until(() => !!session.last().render);
    assert.equal(session.last().render.file, expectedFile);
    assert.equal(session.last().render.dir, expectedDir);
    assert.equal(session.last().render.rev, 'r1');
  }
  const outside = join(base, 'plain', 'file.md');
  mkdirSync(dirname(outside));
  writeFileSync(outside, 'plain');
  const session = createSession();
  t.after(() => session.close());
  session.setDocument({ path: outside, text: 'plain', rev: 1 });
  // 試験フォルダー自体がリポジトリ内なら、さらに上の .git も見つかる。
  const expected = realpathSync.native(new URL('../', import.meta.url));
  assert.equal(session.root(), expected);
  await until(() => !!session.last().render);
  assert.equal(session.last().render.file, relative(expected, outside).replaceAll('\\', '/'));
});

test('スクロールの最後の値を保持し、終了後は worker を動かさない', async () => {
  const received = [];
  const session = createSession({ onScroll: (data) => received.push(data) });
  const scroll = { line: 8, ratio: 0.4, lines: 50 };
  session.setScroll(scroll);
  assert.deepEqual(session.last(), { render: null, scroll });
  assert.deepEqual(received, [scroll]);
  await session.close();
  assert.throws(() => session.setDocument({ text: '', name: '', rev: 1 }), /終了/);
});

test('git がない場合は文書のフォルダーをルートにする', async (t) => {
  const fs = await import('node:fs');
  const { syncBuiltinESMExports } = await import('node:module');
  const root = temp(t);
  const file = join(root, 'plain.md');
  writeFileSync(file, 'x');
  const session = createSession({ maxBytes: 0 });
  t.after(() => session.close());
  // 書き込み領域をリポジトリ内に限定したまま、git がない環境を再現する。
  t.mock.method(fs.default, 'existsSync', () => false);
  syncBuiltinESMExports();
  try {
    session.setDocument({ path: file, text: 'x', rev: 1 });
  } finally {
    t.mock.restoreAll();
    syncBuiltinESMExports();
  }
  assert.equal(session.root(), realpathSync.native(root));
  assert.equal(session.last().render.file, 'plain.md');
  assert.equal(session.last().render.dir, '');
});

test('worker が途中で終了しても誤りを通知し、その後に描画できる', async (t) => {
  const { Worker } = await import('node:worker_threads');
  const rendered = [];
  const session = createSession({ onRender: (data) => rendered.push(data) });
  t.after(() => session.close());
  // 実際の worker を、本文が届く前に外から停止して異常終了を再現する。
  const mocked = t.mock.method(Worker.prototype, 'postMessage', function () {
    void this.terminate();
  });
  session.setDocument({ path: null, name: '停止.md', text: '# 停止', rev: 1 });
  mocked.mock.restore();
  await until(() => rendered.length === 1);
  assert.match(rendered[0].error, /処理が終了/);
  assert.equal(rendered[0].markdown, '# 停止');
  session.setDocument({ path: null, name: '復帰.md', text: '# 復帰', rev: 2 });
  await until(() => rendered.length === 2);
  assert.match(rendered[1].html, /復帰/);
});

test('文書のフォルダーがリンクでも、実ルートからの file と dir を使う', async (t) => {
  const { symlinkSync } = await import('node:fs');
  const base = temp(t);
  const repo = join(base, 'repo');
  mkdirSync(join(repo, 'docs'), { recursive: true });
  mkdirSync(join(repo, '.git'));
  const alias = join(base, 'alias');
  try {
    symlinkSync(repo, alias, process.platform === 'win32' ? 'junction' : 'dir');
  } catch (error) {
    if (['EPERM', 'EACCES', 'ENOTSUP'].includes(error.code))
      return t.skip('文書フォルダーのリンクを作る権限または機能がない');
    throw error;
  }
  const session = createSession({ maxBytes: 0 });
  t.after(() => session.close());
  session.setDocument({ path: join(alias, 'docs', 'new.md'), text: 'x', rev: 1 });
  assert.equal(session.root(), realpathSync.native(repo));
  assert.equal(session.last().render.file, 'docs/new.md');
  assert.equal(session.last().render.dir, 'docs/');
});

test('ファイル文書を別々に見張り、編集中と同じファイルは未保存の本文を見せる', async (t) => {
  const root = temp(t);
  const a = join(root, 'a.md');
  const b = join(root, 'b.md');
  writeFileSync(a, '# ディスクのA');
  writeFileSync(b, '# ディスクのB');
  const main = [];
  const session = createSession({ root, onRender: (data) => main.push(data) });
  t.after(() => session.close());
  session.setDocument({ path: a, name: 'a.md', text: '# 未保存のA', rev: 10 });
  const seenA = [],
    seenB = [];
  const stopA = session.subscribeDocument('a.md', (name, data) => seenA.push({ name, data }));
  const stopB = session.subscribeDocument('b.md', (name, data) => seenB.push({ name, data }));
  t.after(() => {
    stopA();
    stopB();
  });
  await until(
    () =>
      seenA.some((value) => value.data.html?.includes('未保存のA')) &&
      seenB.some((value) => value.data.html?.includes('ディスクのB')),
  );
  assert.equal(session.documentCount(), 1);
  session.setScroll({ line: 1, ratio: 0, lines: 2 });
  writeFileSync(b, '# 保存したB');
  await until(() => seenB.some((value) => value.data.html?.includes('保存したB')));
  assert.equal(
    seenA.some((value) => value.name === 'scroll'),
    false,
  );
  assert.equal(
    seenB.some((value) => value.name === 'scroll'),
    false,
  );
  assert.equal(
    main.some((value) => value.html?.includes('保存したB')),
    false,
  );
  session.setDocument({ path: b, name: 'b.md', text: '# 未保存のB', rev: 11 });
  await until(
    () =>
      seenA.some((value) => value.data.html?.includes('ディスクのA')) &&
      seenB.some((value) => value.data.html?.includes('未保存のB')),
  );
});

test('文書をまたぐ待ち行列も、各文書の最新1件だけを描く', { timeout: 10000 }, async (t) => {
  const { Worker } = await import('node:worker_threads');
  const root = temp(t);
  const editing = join(root, 'editing.md');
  writeFileSync(editing, '');
  writeFileSync(join(root, 'a.md'), '# A0');
  writeFileSync(join(root, 'b.md'), '# B0');
  const calls = [];
  const original = Worker.prototype.postMessage;
  t.mock.method(Worker.prototype, 'postMessage', function (message) {
    calls.push(message.text);
    return Reflect.apply(original, this, [message]);
  });
  const session = createSession({ root, renderTimeoutMs: 1000 });
  t.after(() => session.close());
  session.setDocument({ path: editing, text: '>'.repeat(200000) + ' x\n', rev: 1 });
  const received = [];
  const stops = ['a.md', 'b.md'].map((name) =>
    session.subscribeDocument(name, (event, data) => received.push({ event, data })),
  );
  t.after(() => stops.forEach((stop) => stop()));
  // 最初の readFile が終わってから、描画待ちの本文をさらに置き換える。
  await new Promise((resolve) => setTimeout(resolve, 100));
  writeFileSync(join(root, 'a.md'), '# A1');
  writeFileSync(join(root, 'a.md'), '# A2');
  await until(
    () =>
      received.some((value) => value.data.html?.includes('A2')) &&
      received.some((value) => value.data.html?.includes('B0')),
  );
  assert.equal(calls.includes('# A0'), false);
  assert.equal(calls.includes('# A1'), false);
  assert.equal(calls.filter((value) => value === '# A2').length, 1);
  assert.equal(calls.filter((value) => value === '# B0').length, 1);
});

test('32文書が接続中なら新しいものを開かず、空きができれば古い待機文書を捨てる', async (t) => {
  const root = temp(t);
  for (let i = 0; i < 33; i++) writeFileSync(join(root, i + '.md'), '# 文書' + i);
  const session = createSession({ root, releaseDelayMs: 40 });
  t.after(() => session.close());
  const stops = [];
  for (let i = 0; i < 32; i++) stops.push(session.subscribeDocument(i + '.md', () => {}));
  t.after(() => stops.forEach((stop) => stop()));
  const rejected = [];
  const rejectStop = session.subscribeDocument('32.md', (name, data) => rejected.push(data));
  assert.equal(session.documentCount(), 32);
  assert.equal(rejected[0].error, 'この文書は開けない');
  rejectStop();
  stops[0]();
  assert.equal(session.documentCount(), 32);
  const accepted = [];
  const stopNew = session.subscribeDocument('32.md', (name, data) => accepted.push(data));
  await until(() => accepted.some((value) => value.html?.includes('文書32')));
  assert.equal(session.documentCount(), 32);
  stopNew();
  await until(() => session.documentCount() === 31);
});

test('最後のタブが離れても5秒は保持し、再接続で使い、離れたままなら捨てる', { timeout: 9000 }, async (t) => {
  const root = temp(t);
  writeFileSync(join(root, 'a.md'), '# 文書');
  const session = createSession({ root });
  t.after(() => session.close());
  const received = [];
  const stop = session.subscribeDocument('a.md', (name, data) => received.push(data));
  await until(() => received.length > 0);
  stop();
  assert.equal(session.documentCount(), 1);
  const again = [];
  const stopAgain = session.subscribeDocument('a.md', (name, data) => again.push(data));
  assert.equal(again[0].rev, received[0].rev);
  stopAgain();
  await until(() => session.documentCount() === 0, '最後の接続から5秒後に文書を捨てる', 6500);
});

test('ファイル文書のサイズ制限と、削除・再作成を文書ごとに扱う', async (t) => {
  const { unlinkSync } = await import('node:fs');
  const root = temp(t);
  const file = join(root, 'a.md');
  writeFileSync(file, '日本');
  const session = createSession({ root, maxBytes: 5 });
  t.after(() => session.close());
  const received = [];
  const stop = session.subscribeDocument('a.md', (name, data) => received.push(data));
  t.after(stop);
  await until(() => received.length > 0);
  assert.match(received[0].error, /大きすぎ/);
  assert.equal(received[0].markdown, ''); // 大きすぎるファイルは読まず、本文をページへ送らない
  unlinkSync(file);
  await until(() => received.some((value) => value.error === 'この文書は開けない'));
  writeFileSync(file, 'ok');
  await until(() => received.some((value) => value.html?.includes('ok')));
});

test('拒否した文書の理由は区別せず、別のルートへ移ったタブには reload だけを送る', async (t) => {
  const root = temp(t);
  const one = join(root, 'one'),
    two = join(root, 'two');
  mkdirSync(one);
  mkdirSync(two);
  mkdirSync(join(one, '.git'));
  mkdirSync(join(two, '.git'));
  writeFileSync(join(one, 'main.md'), '');
  writeFileSync(join(two, 'main.md'), '');
  writeFileSync(join(one, 'other.md'), '# 文書');
  const session = createSession();
  t.after(() => session.close());
  session.setDocument({ path: join(one, 'main.md'), text: '# 編集', rev: 1 });
  const stops = [];
  for (const name of ['a.txt', '../main.md', '.git/config.md', 'missing.md']) {
    const received = [];
    stops.push(session.subscribeDocument(name, (event, data) => received.push({ event, data })));
    assert.equal(received[0].data.error, 'この文書は開けない');
  }
  const fileEvents = [];
  stops.push(session.subscribeDocument('other.md', (name, data) => fileEvents.push({ name, data })));
  t.after(() => stops.forEach((stop) => stop()));
  await until(() => fileEvents.some((value) => value.name === 'render'));
  session.setDocument({ path: join(two, 'main.md'), text: '# 別の編集', rev: 2 });
  assert.equal(fileEvents.at(-1).name, 'reload');
  assert.equal(session.documentCount(), 0);
});

test('32文書の境界でも、編集中の文書を別のタブの文書へ切り替えられる', async (t) => {
  const root = temp(t);
  for (let i = 0; i < 33; i++) writeFileSync(join(root, i + '.md'), '# ディスク' + i);
  const session = createSession({ root });
  t.after(() => session.close());
  session.setDocument({ path: join(root, '0.md'), text: '# 未保存0', rev: 1 });
  const results = Array.from({ length: 33 }, () => []);
  const stops = results.map((values, i) => session.subscribeDocument(i + '.md', (name, data) => values.push(data)));
  t.after(() => stops.forEach((stop) => stop()));
  await until(() => results.every((values) => values.some((value) => value.html)));
  assert.equal(session.documentCount(), 32);
  session.setDocument({ path: join(root, '1.md'), text: '# 未保存1', rev: 2 });
  await until(
    () =>
      results[0].some((value) => value.html?.includes('ディスク0')) &&
      results[1].some((value) => value.html?.includes('未保存1')),
  );
  assert.equal(
    results.some((values) => values.some((value) => value.error)),
    false,
  );
  assert.equal(session.documentCount(), 32);
});

test('include の変更は、表示中の未保存の本文と同じ rev で描き直す', async (t) => {
  const root = temp(t);
  const file = join(root, 'main.md');
  writeFileSync(file, '# ディスクの本文');
  writeFileSync(join(root, 'part.md'), '最初の取り込み');
  const rendered = [];
  const session = createSession({ root, onRender: (data) => rendered.push(data) });
  t.after(() => session.close());
  session.setDocument({ path: file, text: '# 未保存の本文\n\n::include{file=part.md}', rev: 42 });
  await until(() => rendered.some((value) => value.html?.includes('最初の取り込み')));
  assert.equal(rendered.at(-1).includes[0].ok, true);
  writeFileSync(join(root, 'part.md'), '変更された取り込み');
  await until(() => rendered.some((value) => value.html?.includes('変更された取り込み')));
  assert.equal(rendered.at(-1).rev, 42);
  assert.match(rendered.at(-1).html, /未保存の本文/);
  assert.doesNotMatch(rendered.at(-1).html, /ディスクの本文/);
});

test('無い include は親がまだ無い場合も見張り、出現・削除・再作成で描き直す', async (t) => {
  const { unlinkSync } = await import('node:fs');
  const root = temp(t);
  const file = join(root, 'main.md');
  writeFileSync(file, '');
  const rendered = [];
  const session = createSession({ root, onRender: (data) => rendered.push(data) });
  t.after(() => session.close());
  session.setDocument({ path: file, text: '::include{file=new/deep/part.md}', rev: 1 });
  await until(() => rendered.length > 0);
  assert.equal(rendered.at(-1).includes[0].ok, false);
  mkdirSync(join(root, 'new', 'deep'), { recursive: true });
  const included = join(root, 'new', 'deep', 'part.md');
  writeFileSync(included, '現れた本文');
  await until(() => rendered.some((value) => value.html?.includes('現れた本文')));
  const beforeDelete = rendered.length;
  unlinkSync(included);
  await until(() => rendered.length > beforeDelete && rendered.at(-1).includes[0].ok === false);
  writeFileSync(included, '作り直した本文');
  await until(() => rendered.some((value) => value.html?.includes('作り直した本文')));
});

async function trackWatches(t) {
  const fs = await import('node:fs');
  const { syncBuiltinESMExports } = await import('node:module');
  const original = fs.default.watch;
  const watched = [];
  t.mock.method(fs.default, 'watch', function (...args) {
    const watcher = Reflect.apply(original, fs.default, args);
    const value = { path: String(args[0]), closed: false };
    const close = watcher.close.bind(watcher);
    watcher.close = () => {
      value.closed = true;
      return close();
    };
    watched.push(value);
    return watcher;
  });
  syncBuiltinESMExports();
  t.after(() => {
    t.mock.restoreAll();
    syncBuiltinESMExports();
  });
  return watched;
}

test('include は32個までとし、集合の入れ替え・休止・終了で見張りを外す', async (t) => {
  const root = temp(t);
  const file = join(root, 'main.md');
  writeFileSync(file, '');
  for (let i = 0; i < 32; i++) writeFileSync(join(root, i + '.md'), '取り込み' + i);
  const watched = await trackWatches(t);
  const rendered = [];
  const session = createSession({ root, onRender: (data) => rendered.push(data) });
  t.after(() => session.close());
  const source = Array.from({ length: 32 }, (_, i) => '::include{file=' + i + '.md}').join('\n');
  session.setDocument({ path: file, text: source, rev: 1 });
  await until(() => rendered.length > 0);
  assert.equal(watched.filter((value) => !value.closed).length, 32);
  session.setDocument({ path: file, text: '::include{file=0.md}', rev: 2 });
  await until(() => rendered.some((value) => value.rev === 2));
  assert.equal(watched.filter((value) => !value.closed).length, 1);
  session.cancelRender();
  assert.equal(watched.filter((value) => !value.closed).length, 0);
  session.setDocument({ path: file, text: '::include{file=1.md}', rev: 3 });
  await until(() => rendered.some((value) => value.rev === 3));
  assert.equal(watched.filter((value) => !value.closed).length, 1);
  await session.close();
  assert.equal(watched.filter((value) => !value.closed).length, 0);
});

test('ファイル文書の include はその文書だけを描き直し、文書の破棄で外す', async (t) => {
  const root = temp(t);
  for (const name of ['a', 'b']) {
    writeFileSync(join(root, name + '.md'), '::include{file=' + name + '-part.md}');
    writeFileSync(join(root, name + '-part.md'), '本文' + name);
  }
  const watched = await trackWatches(t);
  const session = createSession({ root, releaseDelayMs: 30 });
  t.after(() => session.close());
  const a = [],
    b = [];
  const stopA = session.subscribeDocument('a.md', (_name, data) => a.push(data));
  const stopB = session.subscribeDocument('b.md', (_name, data) => b.push(data));
  t.after(() => {
    stopA();
    stopB();
  });
  await until(() => a.length > 0 && b.length > 0);
  const countB = b.length;
  writeFileSync(join(root, 'a-part.md'), 'Aだけを変更');
  await until(() => a.some((value) => value.html?.includes('Aだけを変更')));
  assert.equal(b.length, countB);
  stopA();
  stopB();
  await until(() => session.documentCount() === 0);
  assert.equal(watched.filter((value) => !value.closed).length, 0);
});

test('ルート外・.git と外へ出るリンクの下の include は見張らない', async (t) => {
  const { symlinkSync } = await import('node:fs');
  const base = temp(t);
  const root = join(base, 'root');
  mkdirSync(root);
  mkdirSync(join(root, '.git'));
  writeFileSync(join(base, 'outside.md'), '外');
  writeFileSync(join(root, '.git', 'private.md'), '内側の禁止箇所');
  try {
    symlinkSync(base, join(root, 'escape'), process.platform === 'win32' ? 'junction' : 'dir');
  } catch (error) {
    if (['EPERM', 'EACCES', 'ENOTSUP'].includes(error.code)) return t.skip('リンクを作成できない');
    throw error;
  }
  const watched = await trackWatches(t);
  const rendered = [];
  const session = createSession({ root, onRender: (data) => rendered.push(data) });
  t.after(() => session.close());
  session.setDocument({
    path: join(root, 'main.md'),
    text: '::include{file=../outside.md}\n::include{file=.git/private.md}\n::include{file=escape/missing.md}',
    rev: 1,
  });
  await until(() => rendered.length > 0);
  assert.equal(watched.length, 0);
  assert.equal(
    rendered[0].includes.every((value) => value.ok === false),
    true,
  );
});
