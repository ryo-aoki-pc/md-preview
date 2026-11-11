// Package pipeline は、Markdown を、GitLab の順で消毒とフィルターを通した最終の HTML にする。
//
// ファイルにもネットワークにも触らない
// (読み込みは Context の関数に任せる)。
package pipeline

import (
	"context"

	"github.com/ryo-aoki-pc/md-preview/engine"
)

// IncludeKind は、::include のファイルを読んだ結果の種類。
type IncludeKind int

const (
	// IncludeText は、本文を読めた。
	IncludeText IncludeKind = iota
	// IncludeMissing は、ファイルが無い。
	IncludeMissing
	// IncludeNotReadable は、読めない。
	IncludeNotReadable
	// IncludeTooLarge は、大きすぎる。
	IncludeTooLarge
	// IncludeInvalid は、上のどれでもない値。
	IncludeInvalid
)

// IncludeRead は、Context.ReadInclude の戻り値。
type IncludeRead struct {
	Kind IncludeKind
	Text string // Kind が IncludeText のときの本文
}

// Context は、文書の置き場所と、手元のファイルを調べる関数。
//
// ReadInclude と Exists に渡す path は、ルートからの相対の、文字どおりの名前 (区切りは /)。ここでは .. と絶対パスだけを
// 落とす。パーセントエンコードは解かない。呼ぶ側は、ルートの内側の実ファイルかどうかを自分で確かめること。
type Context struct {
	// File は文書のパス (ルートからの相対。include の基準)。無ければ nil。
	File *string
	// Dir は文書のフォルダー (ルートからの相対。画像などの基準)。無ければ nil。
	Dir *string
	// ReadInclude は ::include のファイルを読む。nil なら include を展開しない。
	ReadInclude func(path string) IncludeRead
	// Exists は、相対の画像などが手元にあるかを返す。nil なら調べない。
	Exists func(path string) bool
	// Plantuml が真なら、plantuml のコードブロックを図の img に置き換える (手元で描けるときだけ真にする)。
	Plantuml bool
}

// Heading は、見出しの一覧の 1 件。
type Heading struct {
	Level int    `json:"level"`
	ID    string `json:"id"`
	Text  string `json:"text"`
}

// Include は、::include の記録の 1 件。
type Include struct {
	Line  int    `json:"line"`
	Path  string `json:"path"`
	Lines int    `json:"lines"`
	OK    bool   `json:"ok"`
}

// Notice は、GitLab では別の見え方になる所の知らせ。Text は原文の抜き出しで、消毒していない (文字として表示する)。
type Notice struct {
	Kind string `json:"kind"`
	Line *int   `json:"line"`
	Text string `json:"text"`
}

// Diagram は、img に置き換えた PlantUML の図。ID は原文の sha256。
type Diagram struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

// Result は、描画の結果。HTML は消毒済み。
type Result struct {
	Source   SourceMap `json:"-"`
	HTML     string    `json:"html"`
	Headings []Heading `json:"headings"`
	Includes []Include `json:"includes"`
	Notices  []Notice  `json:"notices"`
	Diagrams []Diagram `json:"diagrams"`
}

// DocumentError は、利用者に見せてよい、文書の側の誤り (入れ子が深すぎる、など)。Message は日本語の 1 行。
type DocumentError struct {
	Message string
}

func (e *DocumentError) Error() string { return e.Message }

// Renderer は、描画エンジンを使って文書を描く。同時に呼んでよい。
type Renderer struct {
	engine *engine.Engine
}

// New は、描画エンジンから Renderer を作る。
func New(e *engine.Engine) *Renderer { return &Renderer{engine: e} }

// Render は、Markdown を最終の HTML にする。ctx が時間切れ・取り消しになったら、途中で止めて ctx の誤りを返す。
// 文書の側の誤りは *DocumentError で返す。
func (r *Renderer) Render(ctx context.Context, text string, c Context) (Result, error) {
	return r.render(ctx, text, c)
}
