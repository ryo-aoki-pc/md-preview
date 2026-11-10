package pipeline

import (
	"context"
	"errors"
)

// render は、描画の流れ (消毒・フィルター・配色・取り込み・知らせ) を Go に移すまでの仮の実装。
func (r *Renderer) render(_ context.Context, _ string, _ Context) (Result, error) {
	return Result{}, errors.New("描画の流れは、まだ Go 版にありません。")
}
