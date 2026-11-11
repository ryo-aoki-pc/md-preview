// Package edit は、描画に使った原文を確かめて Markdown のファイルやバッファへ反映する。
package edit

import (
	"context"
	"errors"
	"sync"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
	"github.com/ryo-aoki-pc/md-preview/internal/session"
	"github.com/ryo-aoki-pc/md-table-excel/tableedit"
)

type Host interface {
	Source(doc *string) (session.Source, bool)
	Trial(ctx context.Context, src session.Source, text string) (pipeline.Result, error)
	Reload(doc *string)
}
type Mode int

const (
	File Mode = iota
	Buffer
)

type Options struct {
	Host    Host
	Mode    Mode
	Written func(src session.Source, text string)
	// Buffer の方式で、行の置き換えを編集元（Neovim）に頼む。応答を待って返す。
	Send func(ctx context.Context, e BufferEdit) *Error
}

type BufferEdit struct {
	Path  string // session.Source.DocPath。無名なら空
	Buf   any    // session.Source.Buf
	Rev   any    // session.Source.Rev
	Start int    // 0 始まり
	End   int    // 含まない
	Lines []string
}
type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string                      { return e.Message }
func fault(status int, code, message string) *Error { return &Error{status, code, message} }
func invalid() *Error {
	return fault(400, "invalid", "編集の要求の形が正しくありません。")
}

type Service struct {
	mu      sync.Mutex
	opts    Options
	cache   map[string]*mapping
	history histories
}

func New(o Options) *Service { return &Service{opts: o, cache: map[string]*mapping{}} }
func (s *Service) Config() map[string]any {
	if s.opts.Mode == Buffer {
		return map[string]any{"scope": "current", "undo": false}
	}
	return map[string]any{"scope": "all", "undo": true}
}
func (s *Service) source(doc *string, render uint64) (session.Source, *Error) {
	if s.opts.Host == nil {
		return session.Source{}, fault(404, "missing", "描画した文書がありません。")
	}
	src, ok := s.opts.Host.Source(doc)
	if !ok {
		return src, fault(404, "missing", "描画した文書がありません。")
	}
	if render != 0 && (render != src.Render || src.Stale) {
		return src, fault(409, "stale", "描画が古くなりました。画面を読み直してください。")
	}
	return src, nil
}
func (s *Service) Tables(ctx context.Context, doc *string, render uint64) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if render == 0 {
		return nil, invalid()
	}
	src, e := s.source(doc, render)
	if e != nil {
		return nil, e
	}
	text, _, fileErr := s.read(src)
	m, e := s.tables(ctx, src)
	if e != nil {
		return nil, e
	}
	result := m.response
	if s.opts.Mode == File {
		result.Undo = s.history.status(pathKey(src.Path), text)
	}
	if fileErr != nil {
		result.Undo = undoState{}
		result.Document = documentState{false, fileErr.Message}
		result.Tables = append([]tableInfo{}, result.Tables...)
		for i := range result.Tables {
			result.Tables[i].Editable = false
			result.Tables[i].Reason = fileErr.Message
			result.Tables[i].Cells = []cellInfo{}
		}
	}
	return result, nil
}
func (s *Service) Text(ctx context.Context, doc *string, render uint64, r Range) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if render == 0 {
		return nil, invalid()
	}
	src, e := s.source(doc, render)
	if e != nil {
		return nil, e
	}
	if _, _, e = s.read(src); e != nil {
		return nil, e
	}
	start, end, _, e := offsets(src, r)
	if e != nil {
		return nil, e
	}
	if ctx.Err() != nil {
		return nil, fault(422, "cancelled", "原文の読み取りを取り消しました。")
	}
	return map[string]any{"text": removeCR(src.Text[start:end]), "render": src.Render}, nil
}
func (s *Service) Apply(ctx context.Context, doc *string, body map[string]any) (any, *Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, e := parseRequest(body)
	if e != nil {
		return nil, e
	}
	if s.opts.Mode == Buffer && (r.undo || r.redo) {
		return nil, fault(404, "unsupported", "元に戻す操作は Neovim で行ってください。")
	}
	src, e := s.source(doc, r.render)
	if e != nil {
		return nil, e
	}
	old, bom, e := s.readFile(src)
	if e != nil {
		return nil, e
	}
	key := pathKey(src.Path)
	var text string
	warnings := []string{}
	var move *historyMove
	if r.undo || r.redo {
		text, move, e = s.history.prepare(key, old, r.redo)
		if e != nil {
			return nil, e
		}
	} else {
		if e = s.editable(src, old); e != nil {
			return nil, e
		}
		switch r.kind {
		case "table":
			m, err := s.tables(ctx, src)
			if err != nil {
				return nil, err
			}
			if r.table >= len(m.response.Tables) {
				return nil, fault(422, "range", "指定した表がありません。")
			}
			t := m.response.Tables[r.table]
			if !t.Editable {
				return nil, fault(422, "excluded", t.Reason)
			}
			result, applyErr := r.operation.apply(ctx, m.document, tableedit.Target{Number: t.Number, SourceHash: m.models[r.table].SourceHash})
			if applyErr != nil {
				return nil, tableError(applyErr)
			}
			text, warnings = result.Text, result.Warnings
		case "range", "checkbox":
			start, end, line, err := offsets(src, r.rangeValue)
			if err != nil {
				return nil, err
			}
			replacement := r.text
			if r.kind == "checkbox" {
				if end-start != 1 || start < 1 || end >= len(old) || old[start-1] != '[' || old[end] != ']' || (old[start] != ' ' && old[start] != 'x' && old[start] != 'X') {
					return nil, fault(422, "range", "チェックボックスの原文と位置が合いません。")
				}
				replacement = " "
				if r.checked {
					replacement = "x"
				}
			} else {
				replacement = normalizeNewlines(replacement, newlineAt(old, line))
			}
			text = old[:start] + replacement + old[end:]
			if len(text) > maxDocumentBytes {
				return nil, fault(422, "large", "編集後の文書が 16 MiB を超えています。")
			}
			if !utf8.ValidString(text) {
				return nil, fault(422, "invalid", "入力が UTF-8 ではありません。")
			}
			if text != old {
				if _, trialErr := s.opts.Host.Trial(ctx, src, text); trialErr != nil {
					return nil, fault(422, "render", "編集後の文書を描画できません。")
				}
			}
		}
	}
	if text == old {
		return map[string]any{"ok": true, "unchanged": true}, nil
	}
	if len(text) > maxDocumentBytes {
		return nil, fault(422, "large", "編集後の文書が 16 MiB を超えています。")
	}
	if s.opts.Mode == Buffer {
		replacement := bufferEdit(src, old, text)
		if replacement.Start == replacement.End && len(replacement.Lines) == 0 {
			return map[string]any{"ok": true, "unchanged": true}, nil
		}
		if _, e = s.source(doc, r.render); e != nil {
			return nil, e
		}
		if e = s.opts.Send(ctx, replacement); e != nil {
			return nil, e
		}
		if warnings == nil {
			warnings = []string{}
		}
		return map[string]any{"ok": true, "warnings": warnings}, nil
	}
	// 試しの描画中に外で変わった場合も、古い本文を上書きしない。
	current, currentBOM, e := s.readFile(src)
	if e != nil {
		return nil, e
	}
	if current != old || currentBOM != bom {
		s.history.clear(key)
		delete(s.cache, key)
		return nil, changed()
	}
	if r.render != 0 {
		if _, e = s.source(doc, r.render); e != nil {
			return nil, e
		}
	}
	if err := tableedit.WriteFile(ctx, src.Path, text, bom); err != nil {
		return nil, fault(500, "write", "文書を書き込めません。")
	}
	if move != nil {
		s.history.commitMove(key, move)
	} else {
		s.history.push(key, old, text)
	}
	delete(s.cache, key)
	if src.Current {
		if s.opts.Written != nil {
			s.opts.Written(src, text)
		}
	} else {
		s.opts.Host.Reload(doc)
	}
	if warnings == nil {
		warnings = []string{}
	}
	return map[string]any{"ok": true, "warnings": warnings}, nil
}
func changed() *Error {
	return fault(409, "changed", "文書が外で変わりました。画面を読み直してください。")
}
func tableError(err error) *Error {
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	var e *tableedit.Error
	if errors.As(err, &e) {
		status := 422
		if e.Code == "stale" {
			status = 409
		}
		if e.Code == "internal" || e.Code == "write" {
			status = 500
		}
		return fault(status, e.Code, e.Message)
	}
	return fault(500, "internal", "表を編集できません。")
}
