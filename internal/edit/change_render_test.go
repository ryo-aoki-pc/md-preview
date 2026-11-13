package edit

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
)

// 公開の行の対応が、編集の後の本当の描画の行と合うこと（取り込みが読めた場合と、無い場合）。
func TestPublicKeepAgreesWithRender(t *testing.T) {
	renderer := pipeline.New(testEngine(t))
	before := "::include{file=a.md}\n\n本文\n\n::include{file=b.md}\n\n後\n"
	after := "::include{file=a.md}\n\n変更\n追加\n\n::include{file=b.md}\n\n後\n"
	for _, kind := range []pipeline.IncludeKind{pipeline.IncludeText, pipeline.IncludeMissing} {
		ctx := pipeline.Context{ReadInclude: func(string) pipeline.IncludeRead {
			return pipeline.IncludeRead{Kind: kind, Text: "一\n二\n三"}
		}}
		old, err := renderer.Render(context.Background(), before, ctx)
		if err != nil {
			t.Fatal(err)
		}
		next, err := renderer.Render(context.Background(), after, ctx)
		if err != nil {
			t.Fatal(err)
		}
		keep := publicKeep(before, after, commonKeep(before, after), old.Source)
		if len(keep) == 0 {
			t.Fatal("対応が空", kind)
		}
		for _, k := range commonKeep(before, after) {
			for i := 0; i < k[2]; i++ {
				from, want := old.Source.Expanded(k[0]+i), next.Source.Expanded(k[1]+i)
				got := 0
				for _, p := range keep {
					if p[0] <= from && from < p[0]+p[2] {
						got = p[1] + from - p[0]
					}
				}
				if got != want {
					t.Fatalf("取り込み=%d の原文の %d 行: 公開の行 %d → %d、描画では %d（%v）", kind, k[0]+i, from, got, want, keep)
				}
			}
		}
	}
}

// 同じ行が続く文書でも、書き換えた範囲の外だけを対応にし、残った行を書き換えた行と取り違えない。
func TestRangeKeepWithRepeatedLines(t *testing.T) {
	s, h := testService(t, testEngine(t), "同じ\n\n同じ\n", nil)
	v, err := applyJSON(t, s, h, `{"range":"1:1-1:6","text":"同じ\n\n同じ"}`)
	if err != nil || disk(t, h) != "同じ\n\n同じ\n\n同じ\n" {
		t.Fatal(err, disk(t, h))
	}
	// 共通の先頭だけで作ると旧の 3 行目が新の 3 行目（足した段落）になる。
	checkKeep(t, responseChange(t, v).Keep, []lineKeep{{2, 4, 2}})
	if got := rangeKeep("前\n- [ ] a\n後\n", "前\n- [x] a\n後\n", 7, 8); !slices.Equal(got, []lineKeep{{1, 1, 1}, {3, 3, 1}}) {
		t.Fatal(got)
	}
}

// 位置属性の無い生の HTML の表も、原文と対応づけた開始行を公開の行で返す。
func TestHTMLTablePublicStart(t *testing.T) {
	s, h := testService(t, testEngine(t), "前\n\n"+htmlTable+"\n後\n", nil)
	if tables := getTables(t, s, h).Tables; len(tables) != 1 || tables[0].Start != 3 {
		t.Fatal(tables)
	}
	s, h = testService(t, testEngine(t), "::include{file=part.md}\n\n"+htmlTable, map[string]string{"part.md": "一\n二\n三"})
	if tables := getTables(t, s, h).Tables; len(tables) != 1 || tables[0].Start != 5 {
		t.Fatal(tables)
	}
}

// 描画が追い付いていない（描画のもとと書く前の本文が違う）ときは、古い公開の位置を返さない。
func TestTextChangeWithoutCurrentRender(t *testing.T) {
	s, h := testService(t, testEngine(t), "前\n\n本文\n", nil)
	c := s.textChange(h.src, "別の本文\n", "後\n", []lineKeep{{1, 1, 1}})
	if len(c.Keep) != 0 || c.Before != s.version(h.src, "別の本文\n") {
		t.Fatal(c)
	}
}

// バッファでの見出しの操作も、Lua が置き換えた後の本文の版と行の対応を返す。
func TestBufferHeadingChange(t *testing.T) {
	for _, text := range []string{"## A\n\nA\n\n## B\n\nB\n", "## A\n\nA\n\n## B\n\nB"} {
		s, h, sent := bufferService(t, testEngine(t), text)
		v, err := headingRequest(t, s, h, 0, map[string]any{"type": "move", "direction": "down"})
		if err != nil || len(*sent) != 1 {
			t.Fatal(err, sent)
		}
		c := responseChange(t, v)
		after := strings.Join(slicesForHeadingTest(bufferLines(text), (*sent)[0]), "\n") + "\n"
		if after != "## B\n\nB\n\n## A\n\nA\n" || c.After != s.version(h.src, after) {
			t.Fatal(after, c)
		}
		checkKeep(t, c.Keep, []lineKeep{{1, 5, 3}, {5, 1, 3}})
	}
}
