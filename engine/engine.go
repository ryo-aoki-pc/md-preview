// Package engine は、埋め込んだ WebAssembly で Markdown を GitLab の整形器の HTML にする。
// 出力は消毒していない。ページやファイルに出す前に、後段で消毒すること。
package engine

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed glfm_markdown.wasm
var wasmBytes []byte

const (
	memoryLimit       = 256 * 1024 * 1024
	abortedMessage    = "描画エンジンが異常終了しました。"
	allocationMessage = "描画エンジンのメモリーを確保できません。"
)

// GitLabOptions は GitLab 19.5.1 の OPTIONS に相当する、独立した写しを返す。
// github_pre_lang は render_options が足す値で、ここでは <pre lang> の形に固定する。
// 戻り値と上書きは任意の名前を持つ map として JSON にできる。
func GitLabOptions() map[string]any {
	return map[string]any{
		"alerts":                      true,
		"autolink":                    true,
		"cjk_friendly_emphasis":       true,
		"description_lists":           true,
		"escaped_char_spans":          true,
		"footnotes":                   true,
		"full_info_string":            true,
		"hardbreaks":                  false,
		"header_accessibility":        true,
		"header_id_prefix":            "user-content-",
		"inapplicable_tasks":          true,
		"math_code":                   true,
		"math_dollars":                true,
		"multiline_block_quotes":      true,
		"only_escape_chars":           []string{"$", "%", "#", "&", "@", "!", "~", "^", ":"},
		"placeholder_detection":       true,
		"relaxed_autolinks":           true,
		"relaxed_tasklist_character":  true,
		"smart":                       false,
		"sourcepos":                   true,
		"strikethrough":               true,
		"table":                       true,
		"tasklist":                    true,
		"tasklist_classes":            true,
		"tasklist_in_table":           true,
		"wikilinks_title_before_pipe": true,
		"unsafe":                      true,
		"github_pre_lang":             true,
	}
}

// Engine はコンパイル済みの WebAssembly と、直列に使うインスタンスを持つ。
// Render と Close は同時に呼べる。使い終わったら Close で解放する。
type Engine struct {
	gate        chan struct{}
	runtime     wazero.Runtime
	compiled    wazero.CompiledModule
	module      api.Module
	closed      bool
	memoryLimit uint64
	// 試験ではインスタンスを包み、異常終了や解放回数を確かめる。
	instantiate func(context.Context) (api.Module, error)
}

// New は実行環境を作り、埋め込んだ WebAssembly を一度だけコンパイルする。
func New(ctx context.Context) (*Engine, error) {
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	compiled, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = runtime.Close(context.Background())
		return nil, &engineError{message: "描画エンジンを準備できません。", cause: err}
	}
	e := &Engine{
		gate: make(chan struct{}, 1), runtime: runtime, compiled: compiled, memoryLimit: memoryLimit,
	}
	e.instantiate = func(ctx context.Context) (api.Module, error) {
		return runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
	}
	return e, nil
}

type engineError struct {
	message string
	cause   error
}

func (e *engineError) Error() string { return e.message }
func (e *engineError) Unwrap() error { return e.cause }

func (e *Engine) lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case e.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) discard() {
	if e.module != nil {
		_ = e.module.Close(context.Background())
		e.module = nil
	}
}

func (e *Engine) broken(ctx context.Context, message string, cause error) error {
	e.discard()
	if err := ctx.Err(); err != nil {
		cause = err
	}
	return &engineError{message: message, cause: cause}
}

type region struct {
	pointer uint32
	length  uint32
}

func (e *Engine) write(ctx context.Context, current api.Module, data []byte) (region, error) {
	if uint64(len(data)) > uint64(^uint32(0)) {
		return region{}, e.broken(ctx, allocationMessage, nil)
	}
	result, err := current.ExportedFunction("glfm_alloc").Call(ctx, uint64(len(data)))
	if err != nil {
		return region{}, e.broken(ctx, abortedMessage, err)
	}
	if len(result) != 1 {
		return region{}, e.broken(ctx, abortedMessage, nil)
	}
	pointer := uint32(result[0])
	if pointer == 0 {
		return region{}, e.broken(ctx, allocationMessage, nil)
	}
	// 確保でメモリーが伸びることがあるため、確保後のメモリーへ書く。
	if !current.Memory().Write(pointer, data) {
		return region{}, e.broken(ctx, abortedMessage, nil)
	}
	return region{pointer: pointer, length: uint32(len(data))}, nil
}

// Render は Markdown を HTML にする。overrides は既定値に重ねる任意のオプション。
// 取り消しや時間切れは errors.Is(err, ctx.Err()) で見分けられる。
func (e *Engine) Render(ctx context.Context, markdown string, overrides map[string]any) (html string, err error) {
	if markdown == "" {
		return "", nil
	}
	if err := e.lock(ctx); err != nil {
		return "", err
	}
	defer func() { <-e.gate }()
	if e.closed {
		return "", errors.New("描画エンジンは閉じられています。")
	}
	if e.module != nil && e.module.IsClosed() {
		e.discard()
	}
	if e.module != nil {
		pages, _ := e.module.Memory().Grow(0)
		if uint64(pages)*65536 > e.memoryLimit {
			e.discard()
		}
	}
	if e.module == nil {
		e.module, err = e.instantiate(ctx)
		if err != nil {
			return "", e.broken(ctx, abortedMessage, err)
		}
	}
	current := e.module
	settings := GitLabOptions()
	for name, value := range overrides {
		settings[name] = value
	}
	// JSON.stringify と同じく &・<・> をそのまま渡す。入力の長さが変わると、
	// Rust のメモリー配置に由来する属性の順序まで変わることがある。
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(settings); err != nil {
		return "", &engineError{message: "描画オプションを JSON にできません。", cause: err}
	}
	options := bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'})
	// 領域を返すのはこの一か所だけ。壊れた実体はメモリーごと捨てる。
	regions := make([]region, 0, 3)
	defer func() {
		if e.module != current {
			return
		}
		for _, r := range regions {
			if _, freeErr := current.ExportedFunction("glfm_free").Call(ctx, uint64(r.pointer), uint64(r.length)); freeErr != nil {
				html, err = "", e.broken(ctx, abortedMessage, freeErr)
				return
			}
		}
		if ctx.Err() != nil {
			html, err = "", e.broken(ctx, abortedMessage, ctx.Err())
		}
	}()
	for _, data := range [][]byte{[]byte(markdown), options} {
		r, writeErr := e.write(ctx, current, data)
		if writeErr != nil {
			return "", writeErr
		}
		regions = append(regions, r)
	}
	result, err := current.ExportedFunction("glfm_render").Call(ctx,
		uint64(regions[0].pointer), uint64(regions[0].length), uint64(regions[1].pointer), uint64(regions[1].length))
	if err != nil || len(result) != 1 {
		return "", e.broken(ctx, abortedMessage, err)
	}
	pointer := uint32(result[0])
	if pointer == 0 {
		return "", e.broken(ctx, allocationMessage, nil)
	}
	header, ok := current.Memory().Read(pointer, 5)
	if !ok {
		return "", e.broken(ctx, abortedMessage, nil)
	}
	failed := header[0] != 0
	length := binary.LittleEndian.Uint32(header[1:5])
	// ポインタや長さの加算を i32 に戻す前に検査し、回り込みを防ぐ。
	if uint64(length)+5 > uint64(^uint32(0)) || uint64(pointer)+5+uint64(length) > uint64(^uint32(0)) {
		return "", e.broken(ctx, abortedMessage, nil)
	}
	regions = append(regions, region{pointer: pointer, length: length + 5})
	body, ok := current.Memory().Read(pointer+5, length)
	if !ok {
		return "", e.broken(ctx, abortedMessage, nil)
	}
	if !utf8.Valid(body) {
		return "", errors.New("描画エンジンの結果が UTF-8 ではありません。")
	}
	html = string(body) // 解放前に、WebAssembly のメモリーから写す。
	if failed {
		return "", errors.New(html)
	}
	return html, nil
}

// Close は描画の完了を待ち、実行環境とコンパイル済みのコードを解放する。
// 待っている間の取り消しでは閉じずに戻るため、必要なら再度呼び出す。
func (e *Engine) Close(ctx context.Context) error {
	if err := e.lock(ctx); err != nil {
		return err
	}
	defer func() { <-e.gate }()
	if e.closed {
		return nil
	}
	e.closed = true
	e.module = nil
	// 解放は、呼び出し側の取り消しで中断させない (途中で止めると、閉じた印だけが残って閉じ直せない)。
	return e.runtime.Close(context.WithoutCancel(ctx))
}
