// 描画を worker_threads の中で行う。重い文書でも、サーバー (HTTP と標準入力) を止めない。
// renderDocument はファイルに触らないので、::include の読み込みと、画像などの有無の確かめは、ここで行う。
import { isUtf8 } from 'node:buffer';
import { readFileSync, statSync } from 'node:fs';
import { parentPort } from 'node:worker_threads';

import { DocumentError, renderDocument } from './pipeline.mjs';
import { resolve } from './resolve.mjs';

// GitLab は 10 MB を超える blob を「大きすぎる」として取り込まない (Gitlab::Git::Blob の表示の上限)。
const MAX_INCLUDE_BYTES = 10 * 1024 * 1024;

// ルートからの相対の、文字どおりの名前を、ルートの内側の実ファイルにする。内側に無ければ null。
// resolve() は URL のパス (パーセントエンコード) を受けるので、区切りごとにエンコードして渡す
// (名前に % があっても、解かれて別の名前にならない)。
function locate(root, path) {
  let encoded;
  try {
    encoded = path.split('/').map(encodeURIComponent).join('/');
  } catch {
    return null; // 対になっていないサロゲートを含む名前
  }
  const result = resolve(root, encoded);
  return result.status === 200 ? result.path : null;
}

function fileOf(root, path) {
  const real = locate(root, path);
  if (!real) return null;
  try {
    const info = statSync(real);
    return info.isFile() ? { real, size: info.size } : null;
  } catch {
    return null;
  }
}

// 文書にルートがあるときだけ、読み込みの関数を渡す。無名の文書 (ルート無し) では include を展開せず、
// 画像の有無も調べない (手元に無いのか、置き場所が決まっていないだけなのかを区別できない)。
export function fileContext({ root, file, dir }) {
  const context = { file, dir };
  if (!root) return context;
  context.readInclude = (path) => {
    const found = fileOf(root, path);
    if (!found) return null;
    if (found.size > MAX_INCLUDE_BYTES) return { error: 'too large' };
    let bytes;
    try {
      bytes = readFileSync(found.real);
    } catch {
      return { error: 'not readable' };
    }
    // GitLab の readable_text? に当たる判定: バイナリー (NUL を含む) と UTF-8 でないものは読まない。
    if (bytes.includes(0) || !isUtf8(bytes)) return { error: 'not readable' };
    return bytes.toString('utf8').replace(/^﻿/, '');
  };
  context.exists = (path) => fileOf(root, path) !== null;
  return context;
}

parentPort?.on('message', ({ id, text, context }) => {
  try {
    parentPort.postMessage({ id, ok: true, result: renderDocument(text, fileContext(context)) });
  } catch (error) {
    // 文書の側の誤り (入れ子が深すぎる、など) だけ、その文を利用者に見せる。ほかは中の誤りなので、文は渡さない。
    parentPort.postMessage({ id, ok: false, message: error instanceof DocumentError ? error.message : null });
  }
});
