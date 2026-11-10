import { stat, statSync, watch } from 'node:fs';
import { basename, dirname, isAbsolute, relative, sep } from 'node:path';

// フォルダーを見張るので、別名で書いてから置き換える保存にも追従する。
export function watchFile(path, onChange, { onMissing = () => {}, missingRoot = null, isAllowed = () => true } = {}) {
  let stopped = false;
  let debounce;
  let checking = false;
  let signature;
  let known = false;
  let eventPending = false;
  let watcher;
  let watchedDirectory;
  const sameName = (a, b) => (process.platform === 'win32' ? a.toLowerCase() === b.toLowerCase() : a === b);

  function attach() {
    let directory = dirname(path);
    while (missingRoot) {
      const rel = relative(missingRoot, directory);
      if (isAbsolute(rel) || rel === '..' || rel.startsWith('..' + sep)) return;
      try {
        if (statSync(directory).isDirectory()) break;
      } catch {}
      if (!rel) return;
      directory = dirname(directory);
    }
    if (watchedDirectory === directory && watcher) return;
    watcher?.close();
    watcher = watch(directory, (_event, name) => {
      const first = relative(directory, path).split(sep)[0] || basename(path);
      if (name === null || sameName(String(name), first)) queue();
    });
    watchedDirectory = directory;
    // fs.watch が利用できなくなっても、stat による確認を続ける。
    watcher.on('error', () => {});
  }
  function missing() {
    const disappeared = known && signature !== null;
    signature = null;
    known = true;
    // 従来の onChange は削除中に呼ばない。必要な表示側だけが削除専用の通知を使う。
    if (disappeared) onMissing();
  }
  function check(fromEvent = false) {
    eventPending ||= fromEvent;
    if (stopped || checking) return;
    if (!isAllowed()) {
      watcher?.close();
      watcher = null;
      watchedDirectory = null;
      missing();
      return;
    }
    if (missingRoot) {
      try {
        attach();
      } catch {}
    }
    checking = true;
    stat(path, (error, info) => {
      checking = false;
      if (stopped) return;
      const forced = eventPending;
      eventPending = false;
      if (error || !info.isFile()) return missing();
      const next = String(info.mtimeMs) + ':' + info.size;
      const changed = known && signature !== next;
      signature = next;
      known = true;
      if (changed || forced) onChange();
    });
  }
  function queue() {
    clearTimeout(debounce);
    debounce = setTimeout(() => check(true), 100);
  }
  if (isAllowed()) attach();
  check();
  const poll = setInterval(check, 1000);
  return function stop() {
    stopped = true;
    clearTimeout(debounce);
    clearInterval(poll);
    watcher?.close();
  };
}
