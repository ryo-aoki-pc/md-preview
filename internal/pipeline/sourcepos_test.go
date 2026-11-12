package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"golang.org/x/net/html"
)

func TestNoSourcepos(t *testing.T) {
	r := fixtureRenderer(t)
	text := "# Title\n\nParagraph\n\n- [ ] task\n\n| A | B |\n| - | - |\n| C | D |\n"
	for _, no := range []bool{false, true} {
		result, err := r.Render(context.Background(), text, Context{NoSourcepos: no})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(result.HTML, "data-sourcepos=") == no || strings.Contains(result.HTML, "data-checkbox-sourcepos=") == no {
			t.Fatalf("位置なし=%v: %s", no, result.HTML)
		}
		if result.HasSourcepos {
			t.Fatal("生成された属性を入力の印にした")
		}
	}
	raw := `<p data-sourcepos="1:1-1:3">raw</p><ul><li><input type="checkbox" class="task-list-item-checkbox" data-checkbox-sourcepos="2:4-2:4"></li></ul>`
	result, err := r.Render(context.Background(), text+"\n"+raw, Context{NoSourcepos: true})
	if err != nil {
		t.Fatal(err)
	}
	root, err := htmltree.Fragment(context.Background(), result.HTML)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	err = htmltree.Walk(context.Background(), root, func(n *html.Node) error {
		for _, a := range n.Attr {
			if a.Key == "data-sourcepos" || a.Key == "data-checkbox-sourcepos" {
				found[n.Data+"/"+a.Key] = a.Val
			}
		}
		return nil
	})
	if err != nil || len(found) != 2 || found["p/data-sourcepos"] != "1:1-1:3" || found["input/data-checkbox-sourcepos"] != "2:4-2:4" {
		t.Fatal(found, result.HTML, err)
	}
	if !result.HasSourcepos {
		t.Fatal("生の属性を見落とした")
	}
}

func TestSourceposInputFlag(t *testing.T) {
	r := fixtureRenderer(t)
	for _, c := range []struct {
		name, text, part string
		want             bool
	}{
		{"なし", "本文", "", false},
		{"本文", "sourcepos", "", true},
		{"大文字", `<p DATA-SOURCEPOS="1:1-1:1">raw</p>`, "", true},
		{"取り込み", "::include{file=part.md}", `<p data-sourcepos="1:1-1:1">raw</p>`, true},
		{"取り込みの大文字", "::include{file=part.md}", "SOURCEPOS", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			result, err := r.Render(context.Background(), c.text, Context{ReadInclude: func(string) IncludeRead {
				return IncludeRead{Kind: IncludeText, Text: c.part}
			}})
			if err != nil || result.HasSourcepos != c.want {
				t.Fatal(result, err)
			}
			b, err := json.Marshal(result)
			if err != nil || strings.Contains(string(b), "HasSourcepos") {
				t.Fatal(string(b), err)
			}
		})
	}
}

func TestSourceposInput(t *testing.T) {
	r := fixtureRenderer(t)
	render := func(text, part string, no bool) Result {
		t.Helper()
		result, err := r.Render(context.Background(), text, Context{NoSourcepos: no, ReadInclude: func(string) IncludeRead {
			return IncludeRead{Kind: IncludeText, Text: part}
		}})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if got := render("本文", "", false).SourceposInput; got != "" {
		t.Fatal("印の無い文書に入力の照合値がある", got)
	}
	text := "::include{file=part.md}"
	first := render(text, `<p data-sourcepos="1:1-1:1">raw</p>`, false)
	if len(first.SourceposInput) != 64 {
		t.Fatal(first.SourceposInput)
	}
	// 位置の有無では変わらず、取り込んだ中身が変わると変わる。
	if got := render(text, `<p data-sourcepos="1:1-1:1">raw</p>`, true).SourceposInput; got != first.SourceposInput {
		t.Fatal("同じ入力で照合値が違う", got)
	}
	if got := render(text, `<p data-sourcepos="1:1-1:2">raw</p>`, true).SourceposInput; got == first.SourceposInput || got == "" {
		t.Fatal("違う入力で照合値が同じ", got)
	}
	b, err := json.Marshal(first)
	if err != nil || strings.Contains(string(b), "SourceposInput") {
		t.Fatal(string(b), err)
	}
}
