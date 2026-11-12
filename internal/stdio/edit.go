package stdio

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/edit"
	"github.com/ryo-aoki-pc/md-tools/internal/server"
)

const maxEditBytes = 8 * 1024 * 1024

type lineEdit struct {
	Start int      `json:"start"`
	End   int      `json:"end"`
	Lines []string `json:"lines"`
}
type editMessage struct {
	Type  string     `json:"type"`
	ID    uint64     `json:"id"`
	Path  any        `json:"path"`
	Buf   any        `json:"buf"`
	Rev   any        `json:"rev"`
	Edits []lineEdit `json:"edits"`
}

// 標準入力の読み取りと HTTP の編集要求で、休止・終了と応答待ちを共有する。
type editBridge struct {
	mu             sync.Mutex
	next           uint64
	waiting        map[uint64]chan *edit.Error
	paused, closed bool
	output         *output
	timeout        time.Duration
}

func newEditBridge(output *output, timeout time.Duration) *editBridge {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &editBridge{output: output, timeout: timeout, waiting: map[uint64]chan *edit.Error{}}
}
func pausedEdit() *edit.Error {
	return &edit.Error{Status: 409, Code: "paused", Message: "プレビューは休止中です。Neovim で再開してください。"}
}
func closedEdit() *edit.Error {
	return &edit.Error{Status: 503, Code: "closed", Message: "プレビューを終了しています。"}
}
func busyEdit() *edit.Error {
	return &edit.Error{Status: 503, Code: "busy", Message: "Neovim へ送れません。"}
}
func (b *editBridge) send(ctx context.Context, e edit.BufferEdit) *edit.Error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return closedEdit()
	}
	if b.paused {
		b.mu.Unlock()
		return pausedEdit()
	}
	if ctx.Err() != nil {
		b.mu.Unlock()
		return &edit.Error{Status: 422, Code: "cancelled", Message: "編集の要求を取り消しました。"}
	}
	// JSON の安全な整数を使い切っても、済んだ番号を再利用しない。
	if b.next >= 9007199254740991 {
		b.mu.Unlock()
		return busyEdit()
	}
	b.next++
	id := b.next
	var path any
	if e.Path != "" {
		path = e.Path
	}
	lines := e.Lines
	if lines == nil {
		lines = []string{}
	}
	line, err := server.Stringify(editMessage{"edit", id, path, e.Buf, e.Rev, []lineEdit{{e.Start, e.End, lines}}})
	if err != nil {
		b.mu.Unlock()
		return busyEdit()
	}
	if len(line)+1 > maxEditBytes {
		b.mu.Unlock()
		return &edit.Error{Status: 413, Code: "large", Message: "編集の内容が大きすぎます。"}
	}
	result := make(chan *edit.Error, 1)
	b.waiting[id] = result
	// 登録から送信までを休止・終了と直列にし、応答の取りこぼしを防ぐ。
	if !b.output.sendLine(line) {
		delete(b.waiting, id)
		b.mu.Unlock()
		return busyEdit()
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
	}()
	timer := time.NewTimer(b.timeout)
	defer timer.Stop()
	select {
	case e := <-result:
		return e
	case <-timer.C:
		return &edit.Error{Status: 504, Code: "timeout", Message: "Neovim から応答がありません。"}
	case <-ctx.Done():
		return &edit.Error{Status: 422, Code: "cancelled", Message: "編集の要求を取り消しました。"}
	}
}
func (b *editBridge) result(id uint64, ok bool, message string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.waiting[id]
	if ch == nil {
		return
	}
	delete(b.waiting, id)
	var e *edit.Error
	if !ok {
		if end := strings.IndexAny(message, "\r\n\u2028\u2029"); end >= 0 {
			message = message[:end]
		}
		count := 0
		for offset := range message {
			if count == 200 {
				message = message[:offset]
				break
			}
			count++
		}
		if message == "" {
			message = "Neovim が編集を受け付けませんでした。"
		}
		e = &edit.Error{Status: 409, Code: "rejected", Message: message}
	}
	ch <- e
}
func (b *editBridge) isPaused() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.paused
}
func (b *editBridge) pause(paused bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.paused = paused
	if paused {
		b.finish(pausedEdit())
	}
}
func (b *editBridge) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.finish(closedEdit())
}

// 呼ぶ側で b.mu を保持する。応答済みの番号は、その場で知らない番号にする。
func (b *editBridge) finish(e *edit.Error) {
	for id, ch := range b.waiting {
		delete(b.waiting, id)
		ch <- e
	}
}
