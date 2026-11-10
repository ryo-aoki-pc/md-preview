import { stat, watch } from 'node:fs';
import { basename, dirname } from 'node:path';

// フォルダーを見張るので、別名で書いてから置き換える保存にも追従する。
export function watchFile(path, onChange, { onMissing = () => {} } = {}) {
  let stopped = false;
  let debounce;
  let checking = false;
  let signature;
  let known = false;
  let eventPending = false;

  function check(fromEvent = false) {
    eventPending ||= fromEvent;
    if (stopped || checking) return;
    checking = true;
    stat(path, (error, info) => {
      checking = false;
      if (stopped) return;
      const forced = eventPending;
      eventPending = false;
      if (error || !info.isFile()) {
        const disappeared = known && signature !== null;
        signature = null;
        known = true;
        // 従来の onChange は削除中に呼ばない。必要な表示側だけが削除専用の通知を使う。
        if (disappeared) onMissing();
        return;
      }
      const next = `${info.mtimeMs}:${info.size}`;
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
  const watcher = watch(dirname(path), (event, name) => {
    if (name === null || String(name) === basename(path)) queue();
  });
  // fs.watch が利用できなくなっても、stat による確認を続ける。
  watcher.on('error', () => {});
  check();
  const poll = setInterval(check, 1000);
  return function stop() {
    stopped = true;
    clearTimeout(debounce);
    clearInterval(poll);
    watcher.close();
  };
}
