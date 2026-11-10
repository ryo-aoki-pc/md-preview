import { existsSync, realpathSync, statSync } from 'node:fs';
import { readFile, stat } from 'node:fs/promises';
import { basename, dirname, isAbsolute, join, relative } from 'node:path';
import { Worker } from 'node:worker_threads';
import { resolve } from './resolve.mjs';
import { encodeDocument, isMarkdown } from './documents.mjs';
import { watchFile } from './watch.mjs';
import { includeWatchPath } from './include-watch.mjs';

function documentRoot(path, override) {
  if (override) return realpathSync.native(override);
  if (!path) return null;
  const folder = dirname(path);
  for (let dir = folder; ; dir = dirname(dir)) {
    if (existsSync(join(dir, '.git'))) return realpathSync.native(dir);
    if (dirname(dir) === dir) break;
  }
  return realpathSync.native(folder);
}
const keyPath = (path) => {
  const value = path?.replaceAll('\\', '/');
  return process.platform === 'win32' ? value?.toLowerCase() : value;
};
const unavailable = (file) => ({ file, dir: '', rev: 0, error: 'この文書は開けない', markdown: '', headings: [] });

export function createSession({
  onRender = () => {},
  onScroll = () => {},
  renderTimeoutMs = 15000,
  maxBytes = 16 * 1024 * 1024,
  root: rootOverride,
  releaseDelayMs = 5000,
  maxDocuments = 32,
} = {}) {
  let worker = null;
  const terminations = new Set();
  const pending = new Map();
  const documents = new Map();
  const subscribers = new Set();
  const rootListeners = new Set();
  const active = { generation: 0, render: null, context: null, path: null, disposed: false };
  let current = null;
  let sequence = 0;
  let timer;
  let closed = false;
  let mutating = false;
  let activeRoot = rootOverride ? realpathSync.native(rootOverride) : null;
  let scroll = null;

  function clearIncludes(entry) {
    for (const record of entry.includeWatches?.values() || []) record.stop();
    entry.includeWatches?.clear();
  }
  function updateIncludes(job, includes) {
    const entry = job.entry;
    const root = job.context.root;
    const next = new Map();
    for (const include of includes || []) {
      if (next.size >= 32) break;
      const target = includeWatchPath(root, include.path);
      if (target) next.set(keyPath(target), { name: include.path, target });
    }
    entry.includeWatches ||= new Map();
    for (const [key, record] of entry.includeWatches) {
      if (!next.has(key) || next.get(key).name !== record.name) {
        record.stop();
        entry.includeWatches.delete(key);
      }
    }
    for (const [key, value] of next) {
      if (entry.includeWatches.has(key)) continue;
      const record = { name: value.name, stop: () => {} };
      const changed = () => {
        if (closed || entry.disposed || entry.suspended || entry.includeWatches.get(key) !== record) return;
        // 編集中の文書は、ディスクでなく最後に受けた本文で描き直す。ファイル文書は本文も読み直す。
        if (entry === active && entry.input) enqueue(entry, entry.input.text, entry.input.rev);
        else if (entry !== active) void refresh(entry);
      };
      try {
        record.stop = watchFile(value.target, changed, {
          onMissing: changed,
          missingRoot: root,
          isAllowed: () => includeWatchPath(root, value.name) !== null,
        });
        entry.includeWatches.set(key, record);
      } catch {
        // 見張りの開始と同時に親が消えた場合は、次の描画で再び確かめる。
      }
    }
  }

  function publish(job, result) {
    const entry = job.entry;
    if (closed || entry.disposed || job.generation !== entry.generation) return;
    entry.render = { file: job.context.file, dir: job.context.dir, rev: job.rev, ...result };
    if (entry === active) {
      onRender(entry.render);
      for (const sub of subscribers) if (sub.alias && !sub.stale) sub.listener('render', entry.render);
    } else {
      for (const sub of entry.tabs) sub.listener('render', entry.render);
    }
  }
  const fail = (job, message) => {
    if (job.generation === job.entry.generation) clearIncludes(job.entry);
    publish(job, { error: message, markdown: job.text });
  };
  function discardWorker() {
    const old = worker;
    worker = null;
    if (old) {
      const stopped = old.terminate();
      terminations.add(stopped);
      stopped.then(
        () => terminations.delete(stopped),
        () => terminations.delete(stopped),
      );
    }
  }
  function cancel(entry) {
    clearIncludes(entry);
    entry.generation++;
    pending.delete(entry);
    if (current?.entry === entry) {
      clearTimeout(timer);
      current = null;
      discardWorker();
    }
  }
  function finish(job, result) {
    if (current !== job || closed) return;
    clearTimeout(timer);
    current = null;
    if (result.ok) {
      const { html, headings, includes, notices } = result.result;
      if (!job.entry.disposed && job.generation === job.entry.generation) updateIncludes(job, includes);
      publish(job, { html, headings, includes, notices });
    } else {
      // 文書の側の誤り (入れ子が深すぎる、など) は、worker がその文を渡してくる。
      fail(job, (result.message || '文書を描画できませんでした。') + '原文を表示します。');
    }
    drain();
  }
  function startWorker() {
    const next = new Worker(new URL('./worker.mjs', import.meta.url));
    worker = next;
    next.on('message', (result) => {
      if (worker === next && current?.id === result.id) finish(current, result);
    });
    const crash = () => {
      if (closed || worker !== next) return;
      const job = current;
      clearTimeout(timer);
      current = null;
      discardWorker();
      if (job) fail(job, '描画の処理が終了しました。原文を表示します。');
      drain();
    };
    next.on('error', crash);
    next.on('exit', crash);
    return next;
  }
  function drain() {
    if (closed || mutating || current) return;
    while (pending.size) {
      const [entry, job] = pending.entries().next().value;
      pending.delete(entry);
      if (entry.disposed || job.generation !== entry.generation) continue;
      if (Buffer.byteLength(job.text, 'utf8') > maxBytes) {
        fail(job, '文書が大きすぎます。原文を表示します。');
        continue;
      }
      current = job;
      try {
        const running = worker || startWorker();
        timer = setTimeout(() => {
          if (current !== job || closed) return;
          current = null;
          discardWorker();
          fail(job, '文書の描画が時間切れになりました。原文を表示します。');
          drain();
        }, renderTimeoutMs);
        running.postMessage({ id: job.id, text: job.text, context: job.context });
      } catch {
        discardWorker();
        finish(job, { ok: false });
      }
      return;
    }
  }
  function enqueue(entry, text, rev) {
    // 取り込んだファイルが変わったときに描き直すための、編集中の文書の最後の本文 (ファイルの文書は読み直す)。
    if (entry === active) entry.input = { text, rev };
    const job = { id: ++sequence, entry, generation: ++entry.generation, context: entry.context, text, rev };
    // Map の既存の位置を保ち、文書ごとの最新1件を、文書をまたいで順に描く。
    pending.set(entry, job);
    drain();
  }
  function drop(entry) {
    clearTimeout(entry.expiry);
    entry.stop?.();
    entry.disposed = true;
    cancel(entry);
    documents.delete(entry.key);
    drain();
  }
  function release(sub) {
    if (!sub.entry) {
      sub.alias = false;
      return;
    }
    const entry = sub.entry;
    entry.tabs.delete(sub);
    sub.entry = null;
    sub.alias = false;
    if (!entry.tabs.size && !entry.disposed) {
      entry.touched = ++sequence;
      clearTimeout(entry.expiry);
      entry.expiry = setTimeout(() => drop(entry), releaseDelayMs);
      entry.expiry.unref?.();
    }
  }
  function resolveDocument(raw) {
    if (!activeRoot || !isMarkdown(raw)) return null;
    let result;
    try {
      result = resolve(activeRoot, encodeDocument(raw));
    } catch {
      return null;
    }
    if (result.status !== 200) return null;
    try {
      if (!statSync(result.path).isFile()) return null;
    } catch {
      return null;
    }
    const name = raw
      .split('/')
      .filter((part) => part && part !== '.')
      .join('/');
    return { key: keyPath(name), name, path: result.path };
  }
  function sameFile(result) {
    return !!active.context && !!active.path && keyPath(result.path) === keyPath(active.path);
  }
  async function refresh(entry) {
    if (entry.disposed || closed) return;
    if (entry.loading) {
      entry.dirty = true;
      return;
    }
    entry.loading = true;
    do {
      entry.dirty = false;
      try {
        const result = resolveDocument(entry.name);
        if (!result) throw new Error();
        // 読む前に大きさを見る (巨大なファイルを、上限の検査の前にメモリーへ載せない)。
        if ((await stat(result.path)).size > maxBytes) {
          if (entry.disposed || closed) break;
          cancel(entry);
          entry.render = { ...unavailable(entry.name), error: '文書が大きすぎます。' };
          for (const sub of entry.tabs) sub.listener('render', entry.render);
          drain();
          continue;
        }
        const text = (await readFile(result.path, 'utf8')).replace(/^\uFEFF/, '');
        if (entry.disposed || closed) break;
        if (entry.dirty) continue;
        enqueue(entry, text, ++entry.rev);
      } catch {
        if (entry.disposed || closed) break;
        cancel(entry);
        entry.render = unavailable(entry.name);
        for (const sub of entry.tabs) sub.listener('render', entry.render);
        drain();
      }
    } while (entry.dirty && !entry.disposed && !closed);
    entry.loading = false;
  }
  function bind(sub, result = resolveDocument(sub.raw)) {
    if (sub.stale || closed) return;
    if (!result) {
      release(sub);
      sub.listener('render', unavailable(sub.raw));
      return;
    }
    if (sameFile(result)) {
      if (sub.entry) release(sub);
      sub.alias = true;
      if (active.render) sub.listener('render', active.render);
      return;
    }
    if (sub.entry?.key === result.key) return;
    release(sub);
    let entry = documents.get(result.key);
    if (!entry) {
      if (documents.size >= maxDocuments) {
        const idle = [...documents.values()]
          .filter((value) => !value.tabs.size)
          .sort((a, b) => a.touched - b.touched)[0];
        if (idle) drop(idle);
        else {
          sub.listener('render', unavailable(sub.raw));
          return;
        }
      }
      const dir = result.name.includes('/') ? result.name.slice(0, result.name.lastIndexOf('/') + 1) : '';
      entry = {
        key: result.key,
        name: result.name,
        tabs: new Set(),
        generation: 0,
        rev: 0,
        render: null,
        disposed: false,
        context: { root: activeRoot, path: result.path, file: result.name, dir },
      };
      documents.set(entry.key, entry);
      try {
        entry.stop = watchFile(join(activeRoot, ...result.name.split('/')), () => void refresh(entry), {
          onMissing: () => void refresh(entry),
        });
      } catch {
        drop(entry);
        sub.listener('render', unavailable(sub.raw));
        return;
      }
      void refresh(entry);
    }
    clearTimeout(entry.expiry);
    entry.tabs.add(sub);
    sub.entry = entry;
    if (entry.render) sub.listener('render', entry.render);
  }

  return {
    setDocument({ path = null, name, text, rev }) {
      if (closed) throw new Error('プレビューは終了しています');
      if (path && !isAbsolute(path)) throw new Error('文書には絶対パスを指定してください');
      const documentPath = path && join(realpathSync.native(dirname(path)), basename(path));
      const root = documentRoot(documentPath, rootOverride);
      const file = documentPath ? relative(root, documentPath).replaceAll('\\', '/') : name || '[無名]';
      const dir = path && file.includes('/') ? file.slice(0, file.lastIndexOf('/') + 1) : '';
      const changedRoot = keyPath(root) !== keyPath(activeRoot);
      mutating = true;
      try {
        if (keyPath(documentPath) !== keyPath(active.context?.path) || changedRoot) {
          clearIncludes(active);
          active.render = null;
          scroll = null;
        }
        active.suspended = false;
        active.context = { root, path: documentPath, name: name || (path && basename(path)), file, dir };
        try {
          active.path = path ? realpathSync.native(path) : null;
        } catch {
          active.path = documentPath;
        }
        activeRoot = root;
        if (changedRoot) {
          for (const sub of subscribers) {
            release(sub);
            sub.stale = true;
            sub.listener('reload', {});
          }
          for (const entry of [...documents.values()]) drop(entry);
          for (const listener of rootListeners) listener();
        } else {
          const resolved = new Map(
            [...subscribers].filter((sub) => !sub.stale).map((sub) => [sub, resolveDocument(sub.raw)]),
          );
          // 新しく編集中になる文書の監視を先に外し、32件の境界でも切り替えの順番に左右されないようにする。
          for (const [sub, result] of resolved) {
            if (sub.entry && (!result || sameFile(result) || sub.entry.key !== result.key)) release(sub);
          }
          for (const [sub, result] of resolved) bind(sub, result);
        }
        enqueue(active, text, rev);
      } finally {
        mutating = false;
        drain();
      }
    },
    setScroll(value) {
      if (closed) return;
      scroll = { line: value.line, ratio: value.ratio, lines: value.lines };
      onScroll(scroll);
    },
    cancelRender() {
      active.suspended = true;
      cancel(active);
      drain();
    },
    root: () => activeRoot,
    hasDocument: () => !!active.context,
    isCurrentDocument(raw) {
      const result = resolveDocument(raw);
      return !!result && sameFile(result);
    },
    last: () => ({ render: active.render, scroll }),
    documentCount: () => documents.size,
    onRootChange(listener) {
      rootListeners.add(listener);
      return () => rootListeners.delete(listener);
    },
    subscribeDocument(raw, listener) {
      const sub = { raw, listener, entry: null, alias: false, stale: false };
      subscribers.add(sub);
      bind(sub);
      return () => {
        if (subscribers.delete(sub)) release(sub);
      };
    },
    async close() {
      closed = true;
      clearIncludes(active);
      clearTimeout(timer);
      pending.clear();
      current = null;
      for (const entry of [...documents.values()]) drop(entry);
      subscribers.clear();
      rootListeners.clear();
      discardWorker();
      await Promise.all(terminations);
    },
  };
}
