// 描画エンジン: Markdown を、GitLab の整形器 (gem gitlab-glfm-markdown の glfm.rs・formatter.rs と Comrak) の HTML にする。
// engine/glfm_markdown.wasm を読むだけで、外への通信も、ほかのプロセスの起動もしない。
// 出てくる HTML は消毒していない (生の HTML を通す)。消毒を通す前に、ページにもファイルにも出さない。
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

function instantiateWasm() {
  compiled ??= new WebAssembly.Module(readFileSync(wasmUrl));
  return new WebAssembly.Instance(compiled, {}).exports;
}

/**
 * 描画エンジンを作る。ふつうは下の renderMarkdown を使う (instantiate と memoryLimit は試験のための口)。
 * @param {{ instantiate?: () => object, memoryLimit?: number }} [settings]
 *   instantiate は { memory, glfm_alloc, glfm_free, glfm_render } を返す
 */
export function createEngine({ instantiate = instantiateWasm, memoryLimit = MEMORY_LIMIT } = {}) {
  let wasm = null;

  // 中から例外が出た後は、WebAssembly の中の状態 (スタックの位置や確保の帳簿) を信用できない。
  // 捨てて、次の描画で作り直す。
  function broken(message, cause) {
    wasm = null;
    return new Error(message, { cause });
  }

  function write(current, bytes) {
    let pointer;
    try {
      // WebAssembly の i32 は符号付きで返るので、2 GB を超える位置でも正の数にする。
      pointer = current.glfm_alloc(bytes.length) >>> 0;
    } catch (error) {
      throw broken('描画エンジンが異常終了しました。', error);
    }
    if (pointer === 0) throw broken('描画エンジンのメモリーを確保できません。');
    try {
      // 領域を取るとメモリーが伸びて buffer が入れ替わることがあるので、取った後に見直す。
      new Uint8Array(current.memory.buffer, pointer, bytes.length).set(bytes);
    } catch (error) {
      // 取った領域に書けないのは、中の帳簿が壊れているとき。返さずに捨てる。
      throw broken('描画エンジンが異常終了しました。', error);
    }
    return pointer;
  }

  return function render(text, options = {}) {
    if (typeof text !== 'string') throw new TypeError('Markdown は文字列で渡してください。');
    if (text === '') return ''; // GitLab も空の入力は空で返す
    if (wasm && wasm.memory.buffer.byteLength > memoryLimit) wasm = null;
    wasm ??= instantiate();
    const current = wasm;
    const input = encoder.encode(text);
    const settings = encoder.encode(JSON.stringify({ ...GITLAB_OPTIONS, ...options }));
    // 取った領域の [先頭, 長さ]。返すのは finally の 1 か所だけにする (二重に返さない)。
    const regions = [];
    try {
      regions.push([write(current, input), input.length]);
      regions.push([write(current, settings), settings.length]);
      let result;
      try {
        result = current.glfm_render(regions[0][0], input.length, regions[1][0], settings.length) >>> 0;
      } catch (error) {
        // 異常終了 (panic) も、スタックのあふれ (RangeError) も、ここに来る。
        throw broken('描画エンジンが異常終了しました。', error);
      }
      if (result === 0) throw broken('描画エンジンのメモリーを確保できません。');
      const view = new DataView(current.memory.buffer);
      const failed = view.getUint8(result) !== 0;
      const length = view.getUint32(result + 1, true);
      regions.push([result, 5 + length]);
      const body = decoder.decode(new Uint8Array(current.memory.buffer, result + 5, length));
      if (failed) throw new Error(body);
      return body;
    } finally {
      // 作り直すことにした後は、古いメモリーごと捨てるので返さない。
      if (wasm === current) for (const [pointer, length] of regions) current.glfm_free(pointer, length);
    }
  };
}

/**
 * Markdown を HTML にする (消毒はしていない)。
 * @type {(text: string, options?: object) => string}
 *   text の改行は LF にしておく (GitLab は解析の前に CR を消す)。
 *   options は GITLAB_OPTIONS に重ねる値 (gem の RenderOptions の名前)
 */
export const renderMarkdown = createEngine();
