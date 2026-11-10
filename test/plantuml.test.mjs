import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { EventEmitter } from 'node:events';
import { chmodSync, mkdirSync, writeFileSync } from 'node:fs';
import { request } from 'node:http';
import { tmpdir } from 'node:os';
import { delimiter, join } from 'node:path';
import test from 'node:test';
import { toHtml } from 'hast-util-to-html';
import { plantumlId, plantumlInput } from '../lib/plantuml-source.mjs';
import plantuml from '../lib/filters/plantuml.mjs';
import { findPlantuml, createPlantumlRenderer } from '../lib/plantuml.mjs';
import { renderDocument } from '../lib/pipeline.mjs';
import { fragment, elements, content, element, text } from '../lib/tree.mjs';
import { createSession } from '../lib/session.mjs';
import { createServer } from '../lib/server.mjs';
import { loadPage } from '../lib/page.mjs';
import { exportDocument } from '../lib/export.mjs';
import { main, parseArgs } from '../cli.mjs';
import { temp, get, until, isolateUserData } from './helpers.mjs';
import { fakePlantuml, SVG } from './plantuml-helper.mjs';

isolateUserData();
const fence = (source) => '```plantuml\n' + source + '\n```\n';
const starts = (fake) => fake.calls().filter((call) => call.kind === 'start');
const alive = (pid) => {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
};

test('PlantUML の ID と標準入力は UTF-8 の原文から決まる', () => {
  assert.equal(plantumlId('abc'), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
  assert.match(plantumlId('日本語'), /^[a-f0-9]{64}$/);
  assert.notEqual(plantumlId('a'), plantumlId('a\n'));
  assert.equal(plantumlInput('a'), '@startuml\na\n@enduml\n');
  assert.equal(plantumlInput('a\n'), '@startuml\na\n@enduml\n');
  assert.equal(plantumlInput('@startuml\na\n@enduml'), '@startuml\na\n@enduml\n');
  assert.equal(plantumlInput('@startmindmap\n@endmindmap\n'), '@startmindmap\n@endmindmap\n');
});

test('フィルターは二つの対象・chomp・重複の集約と行の位置を扱う', () => {
  for (const onCode of [false, true])
    for (const ending of ['\n', '\r', '\r\n']) {
      const code = element('code', onCode ? { dataCanonicalLang: 'plantuml' } : {}, [text('日本語\n' + ending)]);
      const tree = {
        type: 'root',
        children: [
          element('pre', { dataSourcepos: '1:1-3:3', ...(!onCode ? { dataCanonicalLang: 'plantuml' } : {}) }, [code]),
        ],
      };
      const state = { plantuml: {} };
      plantuml(tree, state);
      const img = tree.children[0];
      assert.equal(img.tagName, 'img');
      assert.equal(img.properties.dataSourcepos, '1:1-3:3');
      assert.equal(img.properties.src, '/_/plantuml/' + plantumlId('日本語\n') + '.svg');
      assert.equal(
        img.properties.dataDiagramSrc,
        'data:text/plain;base64,' + Buffer.from('日本語\n').toString('base64'),
      );
      assert.deepEqual([...state.diagrams.values()], ['日本語\n']);
    }
  const result = renderDocument(fence('a') + fence('a'), { plantuml: {} });
  assert.equal(result.diagrams.length, 1);
  assert.equal(elements(fragment(result.html), (node) => node.tagName === 'img').length, 2);
});

test('無効・上限超の図はコードのまま、変換した図は通知から除く', () => {
  const source = 'Alice -> Bob';
  const disabled = renderDocument(fence(source), { exists: () => false });
  assert.deepEqual(disabled.diagrams, []);
  assert.match(disabled.html, /<pre/);
  assert.deepEqual(
    disabled.notices.map((notice) => notice.kind),
    ['diagram'],
  );
  const tree = fragment('<pre data-canonical-lang="plantuml"><code>a</code></pre>');
  const original = toHtml(tree);
  assert.equal(toHtml(plantuml(tree, {})), original);
  const enabled = renderDocument(fence(source), { plantuml: {}, exists: () => assert.fail('図の URL は調べない') });
  assert.deepEqual(enabled.notices, []);
  assert.match(enabled.html, /data-diagram="plantuml"/);
  assert.doesNotMatch(enabled.html, /class="plantuml"/);
  const large = renderDocument(fence('あ'.repeat((256 * 1024) / 3 + 1)), { plantuml: {} });
  assert.deepEqual(large.diagrams, []);
  assert.deepEqual(
    large.notices.map((notice) => notice.kind),
    ['diagram'],
  );
  const exact = renderDocument(fence('x'.repeat(256 * 1024)), { plantuml: {} });
  assert.equal(exact.diagrams.length, 1);
});

test('悪意のある図の原文を属性や HTML として解釈しない', () => {
  const source = '\"><script>alert(1)</script> 日本語 & <img onerror="x">';
  const result = renderDocument(fence(source), { plantuml: {} });
  const tree = fragment(result.html),
    nodes = elements(tree);
  assert.deepEqual(
    nodes.map(({ node }) => node.tagName),
    ['a', 'img'],
  );
  assert.equal(
    nodes[0].node.properties.dataDiagramSrc,
    'data:text/plain;base64,' + Buffer.from(source).toString('base64'),
  );
  assert.deepEqual(result.diagrams, [{ id: plantumlId(source), source }]);
});

test('PATH の空・相対・カレントの項目を見ず、実行可能な通常ファイルだけを探す', (t) => {
  const root = temp(t),
    empty = join(root, 'empty'),
    bin = join(root, 'bin');
  mkdirSync(empty);
  mkdirSync(bin);
  const windows = process.platform === 'win32',
    name = windows ? 'plantuml.cmd' : 'plantuml';
  writeFileSync(join(bin, name), '試験用');
  chmodSync(join(bin, name), 0o755);
  const env = { PATH: ['', '.', 'bin', empty, bin].join(delimiter), PATHEXT: '.CMD;.EXE;.BAT' };
  assert.equal(findPlantuml({ env }), join(bin, name));
  assert.equal(findPlantuml({ env: { ...env, PATH: ['', '.', 'bin', empty].join(delimiter) } }), null);
  mkdirSync(join(empty, name));
  assert.equal(findPlantuml({ env: { ...env, PATH: empty } }), null);
  if (!windows) {
    chmodSync(join(bin, name), 0o644);
    assert.equal(findPlantuml({ env: { PATH: bin } }), null);
  }
});

test(
  'Windows は PATHEXT の順序を守り、cmd の引用を壊すパスを使わない',
  { skip: process.platform !== 'win32' && 'Windows だけの試験' },
  (t) => {
    const root = temp(t),
      unsafe = join(root, '%unsafe%');
    mkdirSync(unsafe);
    for (const folder of [root, unsafe])
      for (const extension of ['exe', 'cmd', 'bat']) writeFileSync(join(folder, 'plantuml.' + extension), '試験用');
    assert.equal(findPlantuml({ env: { PATH: root, PATHEXT: '.BAT;.CMD;.EXE' } }), join(root, 'plantuml.bat'));
    assert.equal(findPlantuml({ env: { PATH: root, PATHEXT: '.COM;.CMD;.EXE' } }), join(root, 'plantuml.cmd'));
    assert.equal(findPlantuml({ env: { Path: unsafe + ';' + root, PATHEXT: '.CMD' } }), join(root, 'plantuml.cmd'));
    assert.equal(findPlantuml({ env: { PATH: unsafe, PATHEXT: '.CMD' } }), null);
    assert.equal(findPlantuml({ env: { PATH: '"' + root + '"', PATHEXT: '.CMD' } }), null);
  },
);

test('偽のコマンドで SVG を描き、同じ ID の待ちと成功のキャッシュを共有する', async (t) => {
  const calls = [];
  const fake = fakePlantuml(t, {
    spawn: (...args) => {
      calls.push(args);
      return spawn(...args);
    },
  });
  const [a, b] = await Promise.all([fake.renderer.render('slow 日本語'), fake.renderer.render('slow 日本語')]);
  assert.equal(a.toString(), SVG);
  assert.equal(b.toString(), SVG);
  assert.equal((await fake.renderer.render('slow 日本語')).toString(), SVG);
  assert.equal(starts(fake).length, 1);
  const start = starts(fake)[0];
  assert.equal(start.source, plantumlInput('slow 日本語'));
  assert.equal(start.security, 'SANDBOX');
  assert.match(start.java, /-Djava.awt.headless=true -DPLANTUML_SECURITY_PROFILE=SANDBOX$/);
  assert.equal(start.cwd.toLowerCase(), tmpdir().toLowerCase());
  assert.deepEqual(start.args.slice(0, 5), ['-pipe', '-tsvg', '-charset', 'UTF-8', '-pipedelimitor']);
  assert.match(start.args[5], /^mdpreview-[0-9a-f]{48}$/);
  assert.equal(calls[0][2].shell, undefined);
  assert.equal(calls[0][2].windowsHide, true);
  assert.equal(calls[0][1].join(' ').includes('日本語'), false);
  if (process.platform === 'win32') {
    assert.deepEqual(calls[0][1].slice(0, 3), ['/d', '/s', '/c']);
    assert.equal(calls[0][2].windowsVerbatimArguments, true);
  } else assert.equal(calls[0][2].detached, true);
});

test('図を直列に描き、上限を超えた成功のキャッシュを古い順に捨てる', async (t) => {
  const fake = fakePlantuml(t, { maxCacheBytes: Buffer.byteLength(SVG) });
  await Promise.all([fake.renderer.render('slow first'), fake.renderer.render('second')]);
  assert.deepEqual(
    fake.calls().map((call) => call.kind),
    ['start', 'end', 'start', 'end'],
  );
  await fake.renderer.render('second');
  assert.equal(starts(fake).length, 2);
  await fake.renderer.render('slow first');
  assert.equal(starts(fake).length, 3);
});

test('区切り以降の図を捨て、終了コード 200 の誤りの画像も表示する', async (t) => {
  const fake = fakePlantuml(t);
  assert.equal((await fake.renderer.render('multiple\n@startuml\nsecond\n@enduml')).toString(), SVG);
  assert.equal((await fake.renderer.render('error200')).toString(), SVG);
  assert.notEqual(starts(fake)[0].args[5], starts(fake)[1].args[5]);
});

test('SVG 以外・不正 UTF-8・出力の上限は失敗し、失敗は覚えない', async (t) => {
  const fake = fakePlantuml(t, { maxSvgBytes: 1024 });
  for (const source of ['png', 'plain', 'empty', 'invalid-utf8', 'oversize', 'plain']) {
    await assert.rejects(fake.renderer.render(source), (error) => {
      assert.match(error.message, /^PlantUML [^\r\n]+$/);
      assert.equal(error.message.includes(fake.root), false);
      return true;
    });
  }
  assert.equal(starts(fake).length, 6);
  assert.equal((await fake.renderer.render('recovered')).toString(), SVG);
});

test('時間切れは子孫も止め、その後の図を描ける', async (t) => {
  const fake = fakePlantuml(t, { timeoutMs: 1500 });
  await assert.rejects(fake.renderer.render('tree'), /時間切れ/);
  const pids = fake
    .calls()
    .filter((call) => call.pid)
    .map((call) => call.pid);
  assert.equal(pids.length, 2);
  await until(() => pids.every((pid) => !alive(pid)), '子と子孫が終了する');
  assert.equal((await fake.renderer.render('recovered')).toString(), SVG);
});

test('中止は共有する他の待ちを守り、全員の中止では待ち行列と実行を止める', async (t) => {
  const fake = fakePlantuml(t),
    a = new AbortController(),
    b = new AbortController();
  const first = fake.renderer.render('slow shared', { signal: a.signal });
  const rejected = assert.rejects(first, /中止/);
  const second = fake.renderer.render('slow shared', { signal: b.signal });
  a.abort();
  await rejected;
  assert.equal((await second).toString(), SVG);
  const running = new AbortController(),
    queued = new AbortController();
  const third = fake.renderer.render('hang', { signal: running.signal });
  const fourth = fake.renderer.render('never-start', { signal: queued.signal });
  const rejections = [assert.rejects(third, /中止/), assert.rejects(fourth, /中止/)];
  await until(() => starts(fake).some((call) => call.source.includes('hang')));
  queued.abort();
  running.abort();
  await Promise.all(rejections);
  await fake.renderer.render('after-abort');
  assert.equal(
    starts(fake).some((call) => call.source.includes('never-start')),
    false,
  );
  assert.equal(starts(fake).filter((call) => call.source.includes('shared')).length, 1);
  await assert.rejects(fake.renderer.render('pre-aborted', { signal: AbortSignal.abort() }), /中止/);
});

test('既存の JAVA_TOOL_OPTIONS を保って headless と SANDBOX の指定を後ろに足す', async (t) => {
  const previous = process.env.JAVA_TOOL_OPTIONS;
  process.env.JAVA_TOOL_OPTIONS = '-Dexample.test=true';
  try {
    const fake = fakePlantuml(t);
    await fake.renderer.render('env');
    assert.equal(
      starts(fake)[0].java,
      '-Dexample.test=true -Djava.awt.headless=true -DPLANTUML_SECURITY_PROFILE=SANDBOX',
    );
  } finally {
    if (previous === undefined) delete process.env.JAVA_TOOL_OPTIONS;
    else process.env.JAVA_TOOL_OPTIONS = previous;
  }
});

test('起動の失敗は日本語の一行にし、close 後は受け付けない', async (t) => {
  const renderer = createPlantumlRenderer({ command: join(temp(t), 'missing') });
  await assert.rejects(renderer.render('source'), /^Error: PlantUML の図を描けませんでした$/);
  await renderer.close();
  await assert.rejects(renderer.render('source'), /終了/);
});

test('close は動いている図と待ち行列を中止し、子孫の終了を待つ', async (t) => {
  const fake = fakePlantuml(t);
  const running = assert.rejects(fake.renderer.render('tree'), /中止/);
  const queued = assert.rejects(fake.renderer.render('queued'), /中止/);
  await until(() => fake.calls().some((call) => call.kind === 'child'));
  await fake.renderer.close();
  await Promise.all([running, queued]);
  assert.equal(
    fake.calls().every((call) => !alive(call.pid)),
    true,
  );
  assert.equal(
    starts(fake).some((call) => call.source.includes('queued')),
    false,
  );
});

test('session は図の原文を描き直しと文書の破棄で入れ替え、配信には含めない', async (t) => {
  const root = temp(t),
    file = join(root, 'other.md');
  writeFileSync(file, fence('other'));
  const rendered = [],
    session = createSession({ root, plantuml: true, releaseDelayMs: 20, onRender: (result) => rendered.push(result) });
  t.after(() => session.close());
  session.setDocument({ name: 'active.md', text: fence('first'), rev: 1 });
  await until(() => rendered.length === 1);
  assert.equal(session.plantumlSource(plantumlId('first')), 'first');
  assert.equal('diagrams' in rendered[0], false);
  session.setDocument({ name: 'active.md', text: fence('next'), rev: 2 });
  await until(() => rendered.length === 2);
  assert.equal(session.plantumlSource(plantumlId('first')), undefined);
  assert.equal(session.plantumlSource(plantumlId('next')), 'next');
  const release = session.subscribeDocument('other.md', () => {});
  await until(() => session.plantumlSource(plantumlId('other')) === 'other');
  release();
  await until(() => session.documentCount() === 0);
  assert.equal(session.plantumlSource(plantumlId('other')), undefined);
  await session.close();
  assert.equal(session.plantumlSource(plantumlId('next')), undefined);
});

test('HTTP の図は token と送信元を検査し、GET・HEAD とヘッダー・失敗を扱う', async (t) => {
  const fake = fakePlantuml(t);
  const source = 'http',
    id = plantumlId(source);
  const session = {
    root: () => null,
    last: () => ({}),
    plantumlSource: (value) => (value === id ? source : undefined),
  };
  const server = await createServer({ session, page: loadPage(), plantuml: fake.renderer });
  t.after(() => server.close());
  const route = '_/plantuml/' + id + '.svg';
  const response = await get(server, route);
  assert.equal(response.status, 200);
  assert.equal(response.body.toString(), SVG);
  assert.equal(response.headers['content-type'], 'image/svg+xml');
  assert.equal(response.headers['content-security-policy'], 'sandbox');
  assert.equal(response.headers['cache-control'], 'private, max-age=3600');
  assert.equal(response.headers['x-content-type-options'], 'nosniff');
  assert.equal((await get(server, route, { method: 'HEAD' })).body.length, 0);
  assert.equal((await get(server, route, { path: '/' + route })).status, 404);
  assert.equal((await get(server, route, { headers: { 'Sec-Fetch-Site': 'cross-site' } })).status, 403);
  assert.equal((await get(server, route, { headers: { Host: 'example.com' } })).status, 403);
  assert.equal((await get(server, '_/plantuml/' + '0'.repeat(64) + '.svg')).status, 404);
  const disabled = await createServer({ session, page: loadPage() });
  t.after(() => disabled.close());
  assert.equal((await get(disabled, route)).status, 404);
  const failing = await createServer({
    session,
    page: loadPage(),
    plantuml: {
      render: () => Promise.reject(new Error('秘密の原文やパス')),
      close() {},
    },
  });
  t.after(() => failing.close());
  const failed = await get(failing, route);
  assert.equal(failed.status, 502);
  assert.equal(failed.headers['cache-control'], 'no-store');
  assert.equal(failed.body.toString(), 'PlantUML の図を描けませんでした\n');
});

test('HTTP の切断とサーバーの終了は、描いている子の木を残さない', async (t) => {
  for (const disconnect of [true, false]) {
    const fake = fakePlantuml(t),
      source = 'tree',
      id = plantumlId(source);
    const server = await createServer({
      session: { root: () => null, last: () => ({}), plantumlSource: () => source },
      page: loadPage(),
      plantuml: fake.renderer,
    });
    t.after(() => server.close());
    const req = request(new URL('_/plantuml/' + id + '.svg', server.url));
    req.on('error', () => {});
    req.on('response', (res) => res.resume());
    req.end();
    await until(() => fake.calls().some((call) => call.kind === 'child'));
    if (disconnect) req.destroy();
    else await server.close();
    await until(() => fake.calls().every((call) => !alive(call.pid)), '切断後に子孫が終了する');
    await server.close();
  }
});

test('HTML 書き出しは SVG を img にだけ埋め、リンク・経路・追加スクリプトを残さない', async (t) => {
  const fake = fakePlantuml(t),
    file = join(fake.root, 'doc.md');
  writeFileSync(file, fence('export 日本語'));
  const result = await exportDocument({ file, root: fake.root, plantuml: fake.renderer });
  const tree = fragment(result.html),
    anchor = elements(tree, (node) => node.properties.dataDiagram === 'plantuml')[0].node;
  for (const name of ['href', 'target', 'rel']) assert.equal(anchor.properties[name], undefined);
  assert.equal(anchor.children[0].properties.src, 'data:image/svg+xml;base64,' + Buffer.from(SVG).toString('base64'));
  assert.doesNotMatch(result.html, /\/_\/plantuml\/|<script\b|<svg\b/);
  assert.deepEqual(result.warnings, []);
});

test('HTML 書き出しの失敗は安全なコードブロックと件数の知らせに戻す', async (t) => {
  const fake = fakePlantuml(t),
    file = join(fake.root, 'doc.md'),
    source = 'plain 日本語 "><script>x</script>';
  writeFileSync(file, fence(source) + fence(source));
  const result = await exportDocument({ file, root: fake.root, plantuml: fake.renderer });
  const codes = elements(
    fragment(result.html),
    (node) => node.tagName === 'pre' && node.properties.className?.includes('gp-plantuml-error'),
  );
  assert.equal(codes.length, 2);
  assert.equal(content(codes[0].node), source);
  assert.deepEqual(result.warnings, ['PlantUML の図を描けませんでした（2 件）']);
  assert.doesNotMatch(result.html, /\/_\/plantuml\/|<script\b/);
});

test('HTML 書き出しは、利用者の a の中の図と、同じ形の URL を書いただけの画像を区別する', async (t) => {
  const fake = fakePlantuml(t),
    file = join(fake.root, 'doc.md'),
    written = '/_/plantuml/' + 'a'.repeat(64) + '.svg';
  // 利用者の a の中の図は ImageLink が包まないので、img が図の属性を持ったまま残る。
  const inside = (source) => '<a href="https://example.com/">\n\n' + fence(source) + '\n</a>\n\n';
  writeFileSync(file, inside('inside 日本語') + inside('plain 失敗') + '![本文の画像](' + written + ')\n');
  const result = await exportDocument({ file, root: fake.root, plantuml: fake.renderer });
  const tree = fragment(result.html);
  const images = elements(tree, (node) => node.tagName === 'img').map(({ node }) => node.properties);
  assert.equal(images.length, 2);
  assert.equal(images[0].src, 'data:image/svg+xml;base64,' + Buffer.from(SVG).toString('base64'));
  // 図の属性の無い画像は、図として扱わない (書き出しの相対 URL の扱いに任せる)。
  assert.equal(images[1].alt, '本文の画像');
  assert.doesNotMatch(String(images[1].src), /^data:/);
  const codes = elements(
    tree,
    (node) => node.tagName === 'pre' && node.properties.className?.includes('gp-plantuml-error'),
  );
  assert.equal(codes.length, 1);
  assert.equal(content(codes[0].node), 'plain 失敗');
  // 利用者のリンクは変えない。
  const links = elements(tree, (node) => node.tagName === 'a' && node.properties.href === 'https://example.com/');
  assert.equal(links.length, 2);
  assert.deepEqual(
    result.warnings.filter((line) => line.includes('PlantUML')),
    ['PlantUML の図を描けませんでした（1 件）'],
  );
});

test('CLI の --no-plantuml はプレビュー・stdio・書き出しで受け付ける', () => {
  for (const argv of [
    ['--no-plantuml', 'a.md'],
    ['--stdio', '--no-plantuml'],
    ['render', '--no-plantuml', 'a.md'],
  ])
    assert.equal(parseArgs(argv).plantuml, false);
});

test('PATH の偽コマンドで CLI が図を有効にし、--no-plantuml で無効にする', async (t) => {
  const fake = fakePlantuml(t),
    file = join(fake.root, 'doc.md'),
    previous = process.env.PATH;
  writeFileSync(file, fence('cli'));
  process.env.PATH = fake.root;
  try {
    for (const enabled of [true, false]) {
      const io = { stdout: { write() {} }, stderr: { write() {} }, signals: new EventEmitter() };
      io.onStart = async ({ session, server, close }) => {
        await until(() => !!session.last().render);
        assert.equal(session.last().render.html.includes('/_/plantuml/'), enabled);
        if (enabled) assert.equal((await get(server, '_/plantuml/' + plantumlId('cli') + '.svg')).status, 200);
        await close();
      };
      assert.equal(await main(['--no-open', ...(!enabled ? ['--no-plantuml'] : []), file], io), 0);
    }
    let html = '';
    assert.equal(
      await main(['render', file], {
        stdout: {
          write: (s) => {
            html += s;
          },
        },
        stderr: { write() {} },
      }),
      0,
    );
    assert.match(html, /data:image\/svg\+xml;base64,/);
  } finally {
    process.env.PATH = previous;
  }
});
