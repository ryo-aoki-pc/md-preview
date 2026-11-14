package export

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-tools/internal/documents"
	"github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"github.com/ryo-aoki-pc/md-tools/internal/paths"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"golang.org/x/net/html"
)

const maximumImageBytes = 8 * 1024 * 1024
const maximumTotalImageBytes = 32 * 1024 * 1024

func imageMIME(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "image/gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

func imageExtensionMIME(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return ""
}

func readImage(path string, open func(string) (*os.File, error)) ([]byte, string) {
	// 名前付きパイプなどを開いて待ち続けないように、開く前にも種類を確かめる。
	info, err := os.Stat(path)
	if err != nil {
		return nil, "画像を読めません"
	}
	if !info.Mode().IsRegular() {
		return nil, "通常の画像ファイルではありません"
	}
	f, err := open(path)
	if err != nil {
		return nil, "画像を読めません"
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, "通常の画像ファイルではありません"
	}
	if st.Size() > maximumImageBytes {
		return nil, "1 枚の上限の 8 MiB を超えています"
	}
	b, err := io.ReadAll(io.LimitReader(f, maximumImageBytes+1))
	if err != nil {
		return nil, "画像を読めません"
	}
	if len(b) > maximumImageBytes {
		return nil, "1 枚の上限の 8 MiB を超えています"
	}
	return b, ""
}

// embedImages は実パスごとに一度だけ読み、本文の画像の src だけを埋める。
func embedImages(ctx context.Context, tree *html.Node, root, dir string, open func(string) (*os.File, error)) ([]string, error) {
	type reference struct {
		node *html.Node
		mime string
	}
	type imageFile struct {
		path string
		refs []reference
	}
	files := []*imageFile{}
	indices := map[string]*imageFile{}
	reasons := []string{}
	counts := map[string]int{}
	warn := func(reason string, count int) {
		if counts[reason] == 0 {
			reasons = append(reasons, reason)
		}
		counts[reason] += count
	}
	err := htmltree.Walk(ctx, tree, func(n *html.Node) error {
		if n.Type != html.ElementNode || n.Data != "img" {
			return nil
		}
		raw := htmltree.Get(n, "src")
		if raw == "" {
			return nil
		}
		if htmltree.HasClass(n, "gp-plantuml") && strings.HasPrefix(raw, "data:image/svg+xml;base64,") {
			return nil
		}
		// data URL の画像は、すでに HTML に入っているので、そのまま使い、警告もしない。
		if strings.HasPrefix(strings.ToLower(raw), "data:") {
			return nil
		}
		if strings.HasPrefix(raw, "//") || scheme.MatchString(raw) {
			warn("外部 URL の画像は埋め込みません", 1)
			return nil
		}
		name := raw
		if i := strings.IndexAny(name, "?#"); i >= 0 {
			name = name[:i]
		}
		target, err := url.PathUnescape(name)
		if err != nil || target == "" || !utf8.ValidString(target) {
			warn("画像のパスが正しくありません", 1)
			return nil
		}
		local := paths.Resolve(target, dir)
		if !local.Safe {
			warn("ルートの外の画像は埋め込みません", 1)
			return nil
		}
		found := resolve.Resolve(root, documents.Encode(local.Path))
		if found.Status != 200 {
			reason := "ルートの外または許可されないパスの画像です"
			if found.Status == 404 {
				reason = "画像が見つかりません"
			}
			warn(reason, 1)
			return nil
		}
		mime := imageExtensionMIME(target)
		if mime == "" {
			warn("対応していない画像の種類です（PNG・JPEG・GIF・WebP のみ）", 1)
			return nil
		}
		key := found.Path
		if runtime.GOOS == "windows" {
			key = resolve.LowerASCII(key)
		}
		file := indices[key]
		if file == nil {
			file = &imageFile{path: found.Path}
			indices[key] = file
			files = append(files, file)
		}
		file.refs = append(file.refs, reference{n, mime})
		return nil
	})
	if err != nil {
		return nil, err
	}
	total := 0
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b, reason := readImage(file.path, open)
		mime := imageMIME(b)
		if reason == "" && mime == "" {
			reason = "対応していない画像の種類です（PNG・JPEG・GIF・WebP のみ）"
		}
		if reason != "" {
			warn(reason, len(file.refs))
			continue
		}
		matching := []*html.Node{}
		for _, ref := range file.refs {
			if ref.mime != mime {
				warn("拡張子と画像の種類が一致しません", 1)
			} else {
				matching = append(matching, ref.node)
			}
		}
		if len(matching) == 0 {
			continue
		}
		if total+len(b) > maximumTotalImageBytes {
			warn("合計の上限の 32 MiB を超えています", len(matching))
			continue
		}
		total += len(b)
		data := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
		for _, node := range matching {
			htmltree.Set(node, "src", data)
		}
	}
	warnings := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		// 文書に書かれた URL に絶対パスや巨大な data URL があっても、警告には含めない。
		warnings = append(warnings, fmt.Sprintf("画像を埋め込みませんでした（%d 件）: %s", counts[reason], reason))
	}
	return warnings, nil
}
