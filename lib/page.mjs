import { readFileSync } from 'node:fs';

export function loadPage({ theme = 'system', width = 'fluid', jump = false, directory = false } = {}) {
  const read = (name) => readFileSync(new URL('../page/' + name, import.meta.url), 'utf8');
  const config = JSON.stringify({
    appearance: { theme, width },
    // 公式スタイルの設定は、サーバーが保存先を確かめてから入れる。
    assets: null,
    notice: '',
    jump: Boolean(jump),
    directory: Boolean(directory),
  }).replaceAll('<', '\\u003c');
  return {
    html: read('index.html').replace('{{CONFIG}}', () => config),
    js: read('preview.js'),
    css: read('preview.css'),
    fallback: read('fallback.css'),
  };
}
