package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func TestStdioReviewPreparation(t *testing.T) {
	e, err := engine.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	for _, tc := range []struct {
		name, disk, text, code string
		status                 int
		unnamed, missing       bool
	}{
		{name: "一致", disk: "本文\n", text: "本文\n", status: 200},
		{name: "未保存", disk: "前の文\n", text: "本文\n", status: 409, code: "changed"},
		{name: "BOM と CRLF", disk: "\ufeff本文\r\n", text: "本文\n", status: 200},
		{name: "バッファの CRLF", disk: "本文\n", text: "本文\r\n", status: 200},
		{name: "バッファの BOM", disk: "本文\n", text: "\ufeff本文\n", status: 200},
		{name: "無名", text: "本文\n", unnamed: true, status: 422, code: "unnamed"},
		{name: "未作成", text: "本文\n", missing: true, status: 422, code: "excluded"},
		{name: "文書の上限", text: "本文\n", disk: strings.Repeat("a", 16*1024*1024+1), status: 422, code: "large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := resolve.RealPath(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "docs"), 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "docs", "main.md")
			if tc.unnamed {
				path = ""
			} else if !tc.missing {
				if err := os.WriteFile(path, []byte(tc.disk), 0644); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			launches := 0
			input := io.NopCloser(strings.NewReader("{\"type\":\"hello\",\"protocol\":1,\"review\":1}\n"))
			code := Run(ctx, []string{"--stdio", "--no-official-style", "--no-plantuml"}, IO{Stdin: input, Stdout: io.Discard, Stderr: io.Discard}, Dependencies{
				Renderer: pipeline.New(e),
				Launch: func(_ context.Context, opened string) error {
					launches++
					if opened != root {
						t.Errorf("Neovim のルートが違う: %q", opened)
					}
					return nil
				},
				OnStart: func(a *App) error {
					defer a.Close()
					// buf と edit を省いても、コメントの対象はバッファから確認できる。
					if err := a.Session.SetDocument(session.Document{Path: path, Name: "名前だけ.md", Text: tc.text, Rev: 1}); err != nil {
						t.Fatal(err)
					}
					var src session.Source
					for {
						var ready bool
						src, ready = a.Session.Source(nil)
						if ready {
							break
						}
						if ctx.Err() != nil {
							t.Fatal("描画が届きません")
						}
						time.Sleep(5 * time.Millisecond)
					}
					call := func(method, route, body string) (int, string) {
						t.Helper()
						req, _ := http.NewRequestWithContext(ctx, method, a.URL+route, strings.NewReader(body))
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("Origin", strings.TrimSuffix(a.URL, "/"+a.Server.Token+"/"))
						r, err := http.DefaultClient.Do(req)
						if err != nil {
							t.Fatal(err)
						}
						defer r.Body.Close()
						b, err := io.ReadAll(r.Body)
						if err != nil {
							t.Fatal(err)
						}
						return r.StatusCode, string(b)
					}
					position := regexp.MustCompile(`data-sourcepos="([^"]+)"`).FindStringSubmatch(src.HTML)
					if len(position) != 2 {
						t.Fatal("段落の位置がありません", src.HTML)
					}
					for _, route := range []string{fmt.Sprintf("_/review/tables?render=%d", src.Render), fmt.Sprintf("_/review/source?render=%d&tag=p&range=%s", src.Render, position[1])} {
						if status, body := call("GET", route, ""); status != 200 {
							t.Fatal("保存前もコメントの原文を確認できる", status, body)
						}
					}
					status, body := call("POST", "_/review/prepare", fmt.Sprintf(`{"render":%d,"comments":[{"target":{"kind":"block","tag":"p","range":%q},"text":"直してください"}]}`, src.Render, position[1]))
					if status != tc.status || tc.code != "" && !strings.Contains(body, `"code":"`+tc.code+`"`) {
						t.Fatal("依頼文の可否が違う", status, body)
					}
					if tc.code == "changed" && !strings.Contains(body, "Neovim で保存（:w）してから依頼文を作ってください。") || tc.unnamed && !strings.Contains(body, "名前のないバッファでは依頼文を作れません。") {
						t.Fatal("知らせが違う", body)
					}
					if status == 200 {
						var prepared struct{ ID, Prompt string }
						if err := json.Unmarshal([]byte(body), &prepared); err != nil || prepared.ID == "" || !strings.Contains(prepared.Prompt, "docs/main.md") || strings.Contains(prepared.Prompt, root) {
							t.Fatal("相対パスで依頼文を作れません", body)
						}
						// 別のバッファへ移っても起動先は prepare で確認したルートに限る。
						if err := a.Session.SetDocument(session.Document{Text: "別の無名\n", Rev: 2}); err != nil {
							t.Fatal(err)
						}
						status, body = call("POST", "_/review/launch", fmt.Sprintf(`{"id":%q}`, prepared.ID))
						if status != 200 || body != `{"launched":true}` || launches != 1 {
							t.Fatal("検証済みのルートを開けません", status, body, launches)
						}
					} else {
						status, body = call("POST", "_/review/launch", `{"id":"ffffffffffffffffffffffffffffffff"}`)
						if status != 404 || !strings.Contains(body, `"code":"expired"`) || launches != 0 {
							t.Fatal("依頼文なしで起動しています", status, body, launches)
						}
					}
					return nil
				},
			})
			if code != 0 {
				t.Fatal("終了コード", code)
			}
		})
	}
}
