package session

import (
	"context"
	"errors"

	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
)

// Source は成功した描画のもとの本文と、その結果と置き場所。
type Source struct {
	Render                uint64 // render のイベントと同じ描画の番号
	Rev, Buf              any
	DocPath               string // SetDocument に渡されたままのパス。無名なら空
	Text, HTML            string
	Lines                 pipeline.SourceMap
	Root, Path, File, Dir string
	Current               bool // 編集中の文書か
	Stale                 bool // この描画の後に新しい本文が積まれているか
}

// sourceEntry は doc が nil または編集中の文書の別名なら active を返す。
// 呼ぶ側で s.mu を保持する。
func (s *Session) sourceEntry(doc *string) *entry {
	if s.closed {
		return nil
	}
	if doc == nil {
		return s.active
	}
	r := s.resolveDocument(*doc)
	if r == nil {
		return nil
	}
	if s.sameFile(r) {
		return s.active
	}
	return s.documents[r.key]
}

// Source は文書の最後の成功した描画を返す。描画が無い場合や誤りの場合は偽を返す。
func (s *Session) Source(doc *string) (Source, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.sourceEntry(doc)
	if e == nil || e.disposed || e.source == nil {
		return Source{}, false
	}
	src := *e.source
	src.Current = e == s.active
	return src, true
}

// Trial は状態を変えずに、src と同じ条件で本文を試しに描く。
func (s *Session) Trial(ctx context.Context, src Source, text string) (result pipeline.Result, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.RenderTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			result = pipeline.Result{}
			err = errors.New("描画の処理が終了しました。")
		}
		if ctx.Err() != nil {
			result, err = pipeline.Result{}, ctx.Err()
		}
	}()
	return s.opts.Renderer.Render(ctx, text, FileContext(src.Root, src.File, src.Dir, s.opts.Plantuml))
}

// Reload は購読されたファイル文書をすぐ読み直す。編集中の文書には何もしない。
func (s *Session) Reload(doc *string) {
	s.mu.Lock()
	e := s.sourceEntry(doc)
	if e == nil && doc != nil && !s.closed {
		// 削除やリンクの変更で解決できなくても、既存の文書の状態を読み直す。
		// refresh のパス検査を通らないファイルは読み込まない。
		e = s.documents[key(documentName(*doc))]
	}
	if e == nil || e == s.active || e.disposed {
		s.mu.Unlock()
		return
	}
	s.reads.Add(1)
	s.mu.Unlock()
	defer s.reads.Done()
	s.refresh(e, false)
}
