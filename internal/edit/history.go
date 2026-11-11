package edit

import (
	"crypto/sha256"
	"strings"
)

const historySteps = 200
const historyBytes = 32 * 1024 * 1024
const historyDocuments = 32

type undoState struct {
	Undo bool `json:"undo"`
	Redo bool `json:"redo"`
}
type change struct {
	prefix, suffix        int
	before, after         string
	beforeHash, afterHash [32]byte
	order                 uint64
}
type history struct {
	undo, redo []change
	used       uint64
}
type histories struct {
	documents map[string]*history
	clock     uint64
}
type historyMove struct {
	redo bool
	step change
}

func (h *histories) clear(key string) { delete(h.documents, key) }
func (h *histories) status(key, text string) undoState {
	d := h.documents[key]
	if d == nil {
		return undoState{}
	}
	hash := sha256.Sum256([]byte(text))
	if len(d.undo) > 0 && d.undo[len(d.undo)-1].afterHash != hash || len(d.redo) > 0 && d.redo[len(d.redo)-1].beforeHash != hash {
		h.clear(key)
		return undoState{}
	}
	return undoState{len(d.undo) > 0, len(d.redo) > 0}
}
func (h *histories) push(key, before, after string) {
	if h.documents == nil {
		h.documents = map[string]*history{}
	}
	h.clock++
	d := h.documents[key]
	if d == nil {
		d = &history{}
		h.documents[key] = d
	}
	// 外で変わった文書への新しい編集は、前の履歴から切り離す。
	if len(d.undo) > 0 && d.undo[len(d.undo)-1].afterHash != sha256.Sum256([]byte(before)) {
		d.undo = nil
	}
	d.redo = nil
	d.used = h.clock
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix && before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}
	// 小さい差分から、元の大きな本文全体を参照し続けない。
	c := change{prefix: prefix, suffix: suffix, before: strings.Clone(before[prefix : len(before)-suffix]), after: strings.Clone(after[prefix : len(after)-suffix]), beforeHash: sha256.Sum256([]byte(before)), afterHash: sha256.Sum256([]byte(after)), order: h.clock}
	d.undo = append(d.undo, c)
	if len(d.undo) > historySteps {
		d.undo = append([]change{}, d.undo[len(d.undo)-historySteps:]...)
	}
	h.trim()
}
func (h *histories) prepare(key, text string, redo bool) (string, *historyMove, *Error) {
	d := h.documents[key]
	if d == nil {
		return "", nil, fault(422, "history", "取り消し・やり直しの履歴がありません。")
	}
	steps := d.undo
	if redo {
		steps = d.redo
	}
	if len(steps) == 0 {
		return "", nil, fault(422, "history", "取り消し・やり直しの履歴がありません。")
	}
	c := steps[len(steps)-1]
	want := c.afterHash
	middle := c.before
	if redo {
		want = c.beforeHash
		middle = c.after
	}
	if sha256.Sum256([]byte(text)) != want {
		h.clear(key)
		return "", nil, changed()
	}
	return text[:c.prefix] + middle + text[len(text)-c.suffix:], &historyMove{redo, c}, nil
}
func (h *histories) commitMove(key string, m *historyMove) {
	d := h.documents[key]
	if m.redo {
		d.redo[len(d.redo)-1] = change{}
		d.redo = d.redo[:len(d.redo)-1]
		d.undo = append(d.undo, m.step)
	} else {
		d.undo[len(d.undo)-1] = change{}
		d.undo = d.undo[:len(d.undo)-1]
		d.redo = append(d.redo, m.step)
	}
	h.clock++
	d.used = h.clock
}
func (h *histories) trim() {
	for len(h.documents) > historyDocuments {
		key := ""
		oldest := ^uint64(0)
		for k, d := range h.documents {
			if d.used < oldest {
				key, oldest = k, d.used
			}
		}
		delete(h.documents, key)
	}
	for {
		size := 0
		key := ""
		oldest := ^uint64(0)
		undo := true
		index := 0
		for k, d := range h.documents {
			for _, stack := range []struct {
				steps []change
				undo  bool
			}{{d.undo, true}, {d.redo, false}} {
				for i, c := range stack.steps {
					size += len(c.before) + len(c.after)
					if c.order < oldest {
						key, oldest, undo, index = k, c.order, stack.undo, i
					}
				}
			}
		}
		if size <= historyBytes || key == "" {
			break
		}
		// 履歴は現在の本文から連続している部分だけを残す。
		d := h.documents[key]
		if undo {
			d.undo = append([]change{}, d.undo[index+1:]...)
		} else {
			// redo は古い手から再生するので、その手から先も使えない。
			d.redo = nil
		}
		if len(d.undo)+len(d.redo) == 0 {
			delete(h.documents, key)
		}
	}
}
