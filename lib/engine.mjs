// 描画エンジン: Markdown を、GitLab の整形器 (gem gitlab-glfm-markdown の glfm.rs・formatter.rs と Comrak) の HTML にする。
// engine/glfm_markdown.wasm を読むだけで、外への通信も、ほかのプロセスの起動もしない。
// 出てくる HTML は消毒していない (生の HTML を通す)。必ず lib/pipeline.mjs の消毒を通してから表示する。
import { readFileSync } from 'node:fs';

const wasmUrl = new URL('../engine/glfm_markdown.wasm', import.meta.url);
const encoder = new TextEncoder();
const decoder = new TextDecoder('utf-8', { fatal: true });
// 大きい文書を描いた後は WebAssembly のメモリーが伸びたままになる。これを超えたら、次の描画の前に作り直す。
const MEMORY_LIMIT = 256 * 1024 * 1024;

// GitLab 19.5.1 の lib/banzai/filter/markdown_engines/glfm_markdown.rb の OPTIONS と同じ値。
// only_escape_chars は GitLab の参照の記号 (REFERENCE_CHARS)。
// github_pre_lang は GitLab では機能フラグ (use_css_language_classes) で決まる。言語は後ろの CodeLanguage の
// フィルターがどちらの形からも拾うので、ここでは <pre lang="…"> の形にしておく。
export const GITLAB_OPTIONS = Object.freeze({
  alerts: true,
  autolink: true,
  cjk_friendly_emphasis: true,
  description_lists: true,
  escaped_char_spans: true,
  footnotes: true,
  full_info_string: true,
  hardbreaks: false,
  header_accessibility: true,
  header_id_prefix: 'user-content-',
  inapplicable_tasks: true,
  math_code: true,
  math_dollars: true,
  multiline_block_quotes: true,
  only_escape_chars: Object.freeze(['$', '%', '#', '&', '@', '!', '~', '^', ':']),
  placeholder_detection: true,
  relaxed_autolinks: true,
  relaxed_tasklist_character: true,
  smart: false,
  sourcepos: true,
  strikethrough: true,
  table: true,
  tasklist: true,
  tasklist_classes: true,
  tasklist_in_table: true,
  wikilinks_title_before_pipe: true,
  unsafe: true,
  github_pre_lang: true,
});

let compiled = null;
let instance = null;

function exportsOf() {
  if (instance && instance.exports.memory.buffer.byteLength > MEMORY_LIMIT) instance = null;
  if (!instance) {
    compiled ??= new WebAssembly.Module(readFileSync(wasmUrl));
    instance = new WebAssembly.Instance(compiled, {});
  }
  return instance.exports;
}

function write(wasm, bytes) {
  const pointer = wasm.glfm_alloc(bytes.length);
  if (pointer === 0) throw new Error('描画エンジンのメモリーを確保できません。');
  // 領域を取るとメモリーが伸びて buffer が入れ替わることがあるので、取った後に見直す。
  new Uint8Array(wasm.memory.buffer, pointer, bytes.length).set(bytes);
  return pointer;
}

/**
 * Markdown を HTML にする (消毒はしていない)。
 * @param {string} text 改行は LF にしておく (GitLab は解析の前に CR を消す)
 * @param {object} [options] GITLAB_OPTIONS に重ねる値 (gem の RenderOptions の名前)
 * @returns {string}
 */
export function renderMarkdown(text, options = {}) {
  if (typeof text !== 'string') throw new TypeError('Markdown は文字列で渡してください。');
  if (text === '') return ''; // GitLab も空の入力は空で返す
  const wasm = exportsOf();
  const input = encoder.encode(text);
  const settings = encoder.encode(JSON.stringify({ ...GITLAB_OPTIONS, ...options }));
  let inputPointer = 0;
  let settingsPointer = 0;
  try {
    inputPointer = write(wasm, input);
    settingsPointer = write(wasm, settings);
    const result = wasm.glfm_render(inputPointer, input.length, settingsPointer, settings.length);
    if (result === 0) throw new Error('描画エンジンのメモリーを確保できません。');
    const view = new DataView(wasm.memory.buffer);
    const failed = view.getUint8(result) !== 0;
    const length = view.getUint32(result + 1, true);
    const body = decoder.decode(new Uint8Array(wasm.memory.buffer, result + 5, length));
    wasm.glfm_free(result, 5 + length);
    if (failed) throw new Error(body);
    return body;
  } catch (error) {
    if (error instanceof WebAssembly.RuntimeError) {
      // 異常終了 (panic) の後は中の状態が分からないので、次の描画で作り直す。
      instance = null;
      inputPointer = 0;
      settingsPointer = 0;
      throw new Error('描画エンジンが異常終了しました。', { cause: error });
    }
    throw error;
  } finally {
    if (instance) {
      if (inputPointer) wasm.glfm_free(inputPointer, input.length);
      if (settingsPointer) wasm.glfm_free(settingsPointer, settings.length);
    }
  }
}
