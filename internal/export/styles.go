package export

import (
	"crypto/sha256"
	"encoding/base64"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ryo-aoki-pc/md-tools/internal/pagefiles"
	"github.com/ryo-aoki-pc/md-tools/page"
)

var darkRule = regexp.MustCompile(`:root\.gl-dark[^{}]*\{[^{}]*\}`)
var fontURL = regexp.MustCompile(`(?i)url\((fonts/[a-z\d_-]+\.woff2)\)`)
var unusedFont = regexp.MustCompile(`(?i),url\(fonts/[a-z\d_-]+\.(?:woff|ttf)\) format\("(?:woff|truetype)"\)`)
var scriptEnd = regexp.MustCompile(`(?i)</script`)
var styleEnd = regexp.MustCompile(`(?i)</style`)

func styles(theme string) (string, error) {
	fallback, err := page.Files.ReadFile("fallback.css")
	if err != nil {
		return "", err
	}
	preview, err := page.Files.ReadFile("preview.css")
	if err != nil {
		return "", err
	}
	s := string(preview)
	begin := strings.Index(s, "/* ==== 本文のスタイル: ここから")
	end := strings.Index(s, "/* ==== 本文のスタイル: ここまで")
	if begin < 0 {
		return "", failure("同梱のスタイルから、本文の部分を取り出せません")
	}
	newline := strings.IndexByte(s[begin:], '\n')
	start := begin + newline + 1
	if newline < 0 || end <= start {
		return "", failure("同梱のスタイルから、本文の部分を取り出せません")
	}
	css := string(fallback) + "\n" + s[start:end] + "\n:root{--gp-toolbar-height:0px} #gp-content.md{padding:16px 24px} :root[data-theme=\"light\"]{color-scheme:light} :root[data-theme=\"dark\"]{color-scheme:dark}\n"
	if theme == "system" {
		rules := strings.Join(darkRule.FindAllString(string(fallback), -1), "\n")
		rules = strings.ReplaceAll(rules, ":root.gl-dark", ":root[data-theme=\"system\"]")
		css += ":root[data-theme=\"system\"]{color-scheme:light}\n@media screen and (prefers-color-scheme:dark){" + rules + ":root[data-theme=\"system\"]{color-scheme:dark}}\n"
	}
	return css + "@media print{#gp-page.file-holder{max-width:none!important}#gp-content.md{padding:0!important}pre,table{break-inside:avoid;page-break-inside:avoid}}", nil
}
func library(libs *pagefiles.Libraries, name string) (string, error) {
	l, ok := libs.Read(name)
	if !ok {
		return "", failure("同梱のライブラリを読めません: " + name)
	}
	return string(l.Body), nil
}
func katexStyles(libs *pagefiles.Libraries) (string, error) {
	css, err := library(libs, "katex/katex.min.css")
	if err != nil {
		return "", err
	}
	// 変換する CSS もフォントも固定情報を照合した同梱資産だけ。
	var readErr error
	css = fontURL.ReplaceAllStringFunc(css, func(match string) string {
		name := fontURL.FindStringSubmatch(match)[1]
		b, err := library(libs, "katex/"+name)
		if err != nil {
			readErr = err
			return ""
		}
		return "url(data:font/woff2;base64," + base64.StdEncoding.EncodeToString([]byte(b)) + ")"
	})
	return unusedFont.ReplaceAllString(css, ""), readErr
}
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
}
func assemble(path, theme, width, css, body string, scripts []string) string {
	hashes := []string{}
	var inline strings.Builder
	for _, script := range scripts {
		script = scriptEnd.ReplaceAllString(script, `<\/script`)
		sum := sha256.Sum256([]byte(script))
		hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
		inline.WriteString("<script>" + script + "</script>\n")
	}
	source := strings.Join(hashes, " ")
	if source == "" {
		source = "'none'"
	}
	csp := "default-src 'none'; script-src " + source + "; style-src 'unsafe-inline'; font-src data:; img-src * data: blob:; media-src * data:; base-uri 'none'; form-action 'none'"
	class := ""
	if theme == "dark" {
		class = ` class="gl-dark"`
	}
	return "<!doctype html>\n<html lang=\"ja\" data-theme=\"" + theme + "\" data-width=\"" + width + "\"" + class + ">\n<head>\n<meta charset=\"utf-8\">\n<meta http-equiv=\"Content-Security-Policy\" content=\"" + escape(csp) + "\">\n<meta name=\"referrer\" content=\"no-referrer\">\n<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">\n<title>" + escape(filepath.Base(path)) + "</title>\n<style>" + styleEnd.ReplaceAllString(css, `<\/style`) + "</style>\n</head>\n<body><div id=\"gp-page\" class=\"file-holder\"><main id=\"gp-content\" class=\"file-content js-markup-content md\">" + body + "</main></div>\n" + inline.String() + "</body>\n</html>\n"
}
