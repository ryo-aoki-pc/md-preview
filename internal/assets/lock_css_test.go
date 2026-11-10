package assets

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLockValidation(t *testing.T) {
	for _, kind := range []string{"schema", "version", "hash", "size", "size-missing", "size-null", "files-empty", "relative", "backslash", "drive", "root", "device", "control", "trailing", "install-name", "case-duplicate", "archive-duplicate", "collision", "license", "mime", "source-both", "source-neither", "output-hash", "output-size", "registry", "repository", "index", "manifest", "http", "credentials", "license-http", "styles", "sprite"} {
		t.Run(kind, func(t *testing.T) {
			f := assetFixture(t, nil, false)
			r := f.lock.Files["main.css"]
			switch kind {
			case "schema":
				f.lock.Schema = 2
			case "version":
				f.lock.Version = "../escape"
			case "hash":
				f.lock.Archive.SHA256 = "invalid"
			case "size":
				f.lock.Archive.Size = -1
			case "files-empty":
				f.lock.Files = nil
			case "relative":
				f.lock.Files["../escape.css"] = r
			case "backslash":
				f.lock.Files[`a\b.css`] = r
			case "drive":
				f.lock.Files["C:escape.css"] = r
			case "root":
				f.lock.Files["/escape.css"] = r
			case "device":
				f.lock.Files["con.css"] = r
			case "control":
				f.lock.Files["a\x00.css"] = r
			case "trailing":
				f.lock.Files["a."] = r
			case "install-name":
				f.lock.Files["install.json"] = r
			case "case-duplicate":
				f.lock.Files["MAIN.CSS"] = r
			case "archive-duplicate":
				f.lock.Files["another.css"] = r
			case "collision":
				r.Archive = "assets/nested.css"
				f.lock.Files["main.css/child.css"] = r
			case "license":
				r.Licenses = []string{"unknown.txt"}
				f.lock.Files["main.css"] = r
			case "mime":
				r.MIME = "application/javascript"
				f.lock.Files["main.css"] = r
			case "source-both":
				r.URL = f.server.URL + "/license"
				f.lock.Files["main.css"] = r
			case "source-neither":
				r.Archive = ""
				f.lock.Files["main.css"] = r
			case "output-hash":
				r.OutputSHA256 = ""
				f.lock.Files["main.css"] = r
			case "output-size":
				r.OutputSize = nil
				f.lock.Files["main.css"] = r
			case "registry", "repository", "index", "manifest":
				f.lock.Archive.Registry = "https://registry.gitlab.com"
				f.lock.Archive.Repository = "gitlab-org/gitlab-foss/gitlab-assets-ce"
				f.lock.Archive.IndexSHA256 = strings.Repeat("0", 64)
				f.lock.Archive.ManifestSHA256 = strings.Repeat("0", 64)
				if kind == "registry" {
					f.lock.Archive.Registry = "https://other.invalid"
				}
				if kind == "repository" {
					f.lock.Archive.Repository = "other/project"
				}
				if kind == "index" {
					f.lock.Archive.IndexSHA256 = ""
				}
				if kind == "manifest" {
					f.lock.Archive.ManifestSHA256 = ""
				}
			case "http":
				f.lock.Archive.URL = "http://example.invalid/archive"
			case "credentials":
				f.lock.Archive.URL = "https://user:password@example.invalid/archive"
			case "license-http":
				r = f.lock.Files["licenses/source.txt"]
				r.URL = "http://example.invalid/license"
				f.lock.Files["licenses/source.txt"] = r
			case "styles":
				f.lock.Styles.Light = []string{"unknown.css"}
			case "sprite":
				f.lock.Sprite = "main.css"
			}
			data := f.lockJSON()
			if kind == "size-missing" || kind == "size-null" {
				var raw map[string]any
				if json.Unmarshal(data, &raw) != nil {
					t.Fatal("見本の JSON が不正です")
				}
				archive := raw["archive"].(map[string]any)
				if kind == "size-missing" {
					delete(archive, "size")
				} else {
					archive["size"] = nil
				}
				data, _ = json.Marshal(raw)
			}
			if _, err := ParseLock(data); err == nil {
				t.Fatalf("不正なロックを受け付けました: %s", kind)
			}
		})
	}
}

func TestCSSCommentsStringsDataEscapesAndReferences(t *testing.T) {
	css := `.gl-\[\'x\'\] { content:'url(/assets/no)'; /* url(x) */ mask:url('data:image/svg+xml,<svg/>'); src:url(../../fonts/a.woff2#id) }`
	files := map[string]Record{"nested/themes/a.css": {Archive: "assets/nested/themes/a.css"}, "fonts/a.woff2": {}}
	var visited []string
	got, err := RewriteCSS(css, "nested/themes/a.css", files, func(name string) { visited = append(visited, name) })
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`content:'url(/assets/no)'`, `/* url(x) */`, `url('data:image/svg+xml,<svg/>')`, `url("../../fonts/a.woff2#id")`, `.gl-\[\'x\'\]`} {
		if !strings.Contains(got, part) {
			t.Errorf("CSS を変えました: %q が %q にありません", part, got)
		}
	}
	if len(visited) != 1 || visited[0] != "fonts/a.woff2" {
		t.Fatalf("参照の記録: %v", visited)
	}
	for _, pair := range [][2]string{
		{`a{src:URL ( /assets/fonts/a.woff2?x=1#id )}`, `a{src:url("./fonts/a.woff2?x=1#id")}`},
		{`a{src:url("/assets/fonts/\61 .woff2")}`, `a{src:url("./fonts/a.woff2")}`},
		{`a{src:url(#same)}`, `a{src:url(#same)}`},
		{`a{src:myurl(/ignored)}`, `a{src:myurl(/ignored)}`},
	} {
		got, err := RewriteCSS(pair[0], "a.css", files, nil)
		if err != nil || got != pair[1] {
			t.Errorf("CSS の変換: %q %v; 期待 %q", got, err, pair[1])
		}
	}
}

func TestCSSRejectsUnpinnedExternalAndMalformed(t *testing.T) {
	for _, item := range []struct{ css, word string }{
		{`a{src:url(/assets/missing)}`, "ロックにありません"},
		{`a{src:url(https://example.invalid/a)}`, "外部資産"},
		{`a{src:url(//example.invalid/a)}`, "外部資産"},
		{`a{src:url("x)}`, "文字列"},
		{`/* 終わらない`, "コメント"},
		{`a{src:url(x}`, "url()"},
		{`a{src:url(/assets/con.css)}`, "相対パス"},
		{`a{src:url(/assets/%2e%2e/escape)}`, "ロックにありません"},
	} {
		_, err := RewriteCSS(item.css, "a.css", nil, nil)
		requireFailure(t, err, item.word)
	}
}
