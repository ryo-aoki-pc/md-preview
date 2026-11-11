package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-preview/engine"
)

func TestSourceMap(t *testing.T) {
	e, err := engine.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	r := New(e)
	for _, c := range []struct {
		name, text string
		original   []int
		reasons    []string
		context    Context
	}{
		{name: "恒等", text: "前\n本文\n後", original: []int{1, 2, 3}, reasons: []string{"", "", ""}},
		{name: "CRLF", text: "前\r\n本文\r\n後", original: []int{1, 2, 3}, reasons: []string{"", "", ""}},
		{name: "複数の取り込み", text: "前\n::include{file=part.md}\n間\n::include{file=part.md}\n後", original: []int{1, 2, 2, 3, 4, 4, 5}, reasons: []string{"", "include", "include", "", "include", "include", ""}, context: Context{ReadInclude: func(string) IncludeRead { return IncludeRead{Kind: IncludeText, Text: "一\n二"} }}},
		{name: "空の取り込み", text: "::include{file=part.md}\n後", original: []int{1, 2}, reasons: []string{"include", ""}, context: Context{ReadInclude: func(string) IncludeRead { return IncludeRead{Kind: IncludeText} }}},
		{name: "失敗した指示", text: "前\n::include{file=missing.md}\n後", original: []int{1, 2, 3}, reasons: []string{"", "directive", ""}, context: Context{ReadInclude: func(string) IncludeRead { return IncludeRead{Kind: IncludeMissing} }}},
		{name: "リンクの指示", text: "::include{file=https://example.com/part.md}\n後", original: []int{1, 2}, reasons: []string{"directive", ""}, context: Context{ReadInclude: func(string) IncludeRead { t.Fatal("リンク先を読んだ"); return IncludeRead{} }}},
		{name: "展開しない指示", text: "::include{file=part.md}", original: []int{1}, reasons: []string{"directive"}},
		{name: "front matter", text: "---\na: b\n---\n本文", original: []int{1, 2, 3, 4}, reasons: []string{"frontmatter", "frontmatter", "frontmatter", ""}},
		{name: "前置きのある front matter", text: "# coding: utf-8\n\n---\na: b\n---\n本文", original: []int{1, 2, 3, 4, 5, 6}, reasons: []string{"frontmatter", "frontmatter", "frontmatter", "frontmatter", "frontmatter", ""}},
		{name: "front matter と取り込み", text: "---\na: b\n---\n::include{file=part.md}\n後", original: []int{1, 2, 3, 4, 4, 5}, reasons: []string{"frontmatter", "frontmatter", "frontmatter", "include", "include", ""}, context: Context{ReadInclude: func(string) IncludeRead { return IncludeRead{Kind: IncludeText, Text: "一\n二"} }}},
		{name: "末尾が区切り", text: "---\na: b\n---", original: []int{1, 2, 3}, reasons: []string{"frontmatter", "frontmatter", "frontmatter"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			result, err := r.Render(context.Background(), c.text, c.context)
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range c.original {
				line := i + 1
				orig, reason := result.Source.Original(line)
				if orig != want || reason != c.reasons[i] {
					t.Errorf("%d 行: 原文=%d、理由=%q、期待=%d/%q", line, orig, reason, want, c.reasons[i])
				}
				start := result.Source.Expanded(orig)
				if got, _ := result.Source.Original(start); got != orig || start > line {
					t.Errorf("逆向きの対応: 原文=%d、展開後=%d", orig, start)
				}
			}
		})
	}
	for _, text := range []string{
		"前\r本文\n後", "前\n後\r", "前\r\r\n後",
		"---\va: b\n---\n本文", "---\fa: b\n---\n本文", "---\na: b\n---\u0085本文",
		"---\na: b\n---\u2028本文",
	} {
		result, err := r.Render(context.Background(), text, Context{})
		if err != nil {
			t.Fatal(err)
		}
		for line := 1; line <= strings.Count(text, "\n")+3; line++ {
			if _, reason := result.Source.Original(line); reason != "shift" {
				t.Errorf("行がずれる文書 %q の %d 行: 理由=%q", text, line, reason)
			}
		}
	}
	// 取り込んだ本文に単独の CR があるときも、行の数え方がずれるので全体を拒む。
	{
		included := Context{ReadInclude: func(string) IncludeRead { return IncludeRead{Kind: IncludeText, Text: "一\r二"} }}
		result, err := r.Render(context.Background(), "前\n::include{file=part.md}\n後", included)
		if err != nil {
			t.Fatal(err)
		}
		for line := 1; line <= 4; line++ {
			if _, reason := result.Source.Original(line); reason != "shift" {
				t.Errorf("取り込みに単独の CR がある文書の %d 行: 理由=%q", line, reason)
			}
		}
	}
	// 本文が続かなければ、front matter の変換による LF の増減だけでは全体を拒まない。
	result, err := r.Render(context.Background(), "---\va: b\n---\n", Context{})
	if err != nil {
		t.Fatal(err)
	}
	if _, reason := result.Source.Original(1); reason != "frontmatter" {
		t.Fatal(reason)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 5 {
		t.Fatal(string(encoded), err)
	}
	for _, field := range []string{"html", "headings", "includes", "notices", "diagrams"} {
		if _, ok := fields[field]; !ok {
			t.Fatal("既存の JSON の項目が変わった", field)
		}
	}
}

func TestZeroSourceMap(t *testing.T) {
	var m SourceMap
	for _, line := range []int{1, 2, 100} {
		if orig, reason := m.Original(line); orig != line || reason != "" || m.Expanded(line) != line {
			t.Fatal(line, orig, reason)
		}
	}
}
