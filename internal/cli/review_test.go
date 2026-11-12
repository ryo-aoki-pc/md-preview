package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/pipeline"
	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
)

func TestReviewArguments(t *testing.T) {
	for _, args := range [][]string{{"--review", "main.md"}, {"--review", "folder"}, {"--edit", "--review", "main.md"}} {
		o, e := ParseArgs(args)
		if e != nil || !o.Review || o.Edit != (len(args) == 3) {
			t.Fatal(o, e)
		}
	}
	for _, args := range [][]string{{"--stdio", "--review"}, {"render", "--review", "main.md"}, {"install-style", "--review"}} {
		if _, e := ParseArgs(args); e == nil {
			t.Fatal("使えない組み合わせを許した", args)
		}
	}
	if !strings.Contains(strings.SplitN(Help, "\n", 2)[0], "[--review]") {
		t.Fatal("コメントの引数の案内が無い")
	}
}

func TestReviewPreviewRoutes(t *testing.T) {
	for _, editing := range []bool{false, true} {
		root := t.TempDir()
		file := filepath.Join(root, "main.md")
		if e := os.WriteFile(file, []byte("本文\n"), 0644); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		args := []string{"--review", "--no-open", "--no-official-style", "--no-plantuml", "--root", root}
		if editing {
			args = append(args, "--edit")
		}
		args = append(args, file)
		code := Run(ctx, args, IO{Stdout: io.Discard, Stderr: io.Discard}, Dependencies{Renderer: fakeRenderer{}, OnStart: func(a *App) error {
			defer a.Close()
			for _, route := range []string{"", "_/review/tables?render=0", "_/edit/tables?render=0"} {
				r, e := http.Get(a.URL + route)
				if e != nil {
					t.Fatal(e)
				}
				body, e := io.ReadAll(r.Body)
				_ = r.Body.Close()
				if e != nil {
					t.Fatal(e)
				}
				switch route {
				case "":
					if r.StatusCode != 200 || !strings.Contains(string(body), `"review":true`) || strings.Contains(string(body), `"edit":{`) != editing {
						t.Fatal("ページの設定が違う", editing, r.StatusCode)
					}
				case "_/review/tables?render=0":
					if r.StatusCode != 400 {
						t.Fatal("コメントの読み取りの経路が無い", r.StatusCode)
					}
				default:
					want := 404
					if editing {
						want = 400
					}
					if r.StatusCode != want {
						t.Fatal("編集の経路の有効条件が違う", editing, r.StatusCode)
					}
				}
			}
			return nil
		}})
		cancel()
		if code != 0 {
			t.Fatal("起動に失敗した", code)
		}
	}
}

func TestReviewLaunchWiring(t *testing.T) {
	e, err := engine.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	for _, review := range []bool{false, true} {
		name := "コメントなし"
		if review {
			name = "コメントあり"
		}
		t.Run(name, func(t *testing.T) {
			root, err := resolve.RealPath(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "main.md")
			if err := os.WriteFile(file, []byte("本文\n"), 0644); err != nil {
				t.Fatal(err)
			}
			args := []string{"--no-open", "--no-official-style", "--no-plantuml", "--root", root}
			if review {
				args = append(args, "--review")
			}
			args = append(args, file)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var calls atomic.Int32
			code := Run(ctx, args, IO{Stdout: io.Discard, Stderr: io.Discard}, Dependencies{
				Renderer: pipeline.New(e),
				Launch: func(_ context.Context, opened string) error {
					calls.Add(1)
					if opened != root {
						t.Errorf("差し替えた起動へ渡るルートが違います: %q", opened)
					}
					return nil
				},
				OnStart: func(a *App) error {
					defer a.Close()
					post := func(route, body string) (int, string, string) {
						t.Helper()
						req, err := http.NewRequestWithContext(ctx, "POST", a.URL+route, strings.NewReader(body))
						if err != nil {
							t.Fatal(err)
						}
						req.Header.Set("Origin", strings.TrimSuffix(a.URL, "/"+a.Server.Token+"/"))
						req.Header.Set("Content-Type", "application/json")
						r, err := http.DefaultClient.Do(req)
						if err != nil {
							t.Fatal(err)
						}
						data, err := io.ReadAll(r.Body)
						_ = r.Body.Close()
						if err != nil {
							t.Fatal(err)
						}
						return r.StatusCode, string(data), r.Header.Get("Allow")
					}
					status, body, allow := post("_/review/launch", `{"id":"ffffffffffffffffffffffffffffffff"}`)
					if review {
						if status != 404 || !strings.Contains(body, `"code":"expired"`) {
							t.Fatal("コメントありで起動の口がありません", status, body)
						}
						for {
							source, ready := a.Session.Source(nil)
							if ready {
								status, body, _ = post("_/review/prepare", fmt.Sprintf(`{"render":%d,"comments":[{"target":{"kind":"block","tag":"p","range":"1:1-1:6"},"text":"直してください"}]}`, source.Render))
								break
							}
							if ctx.Err() != nil {
								t.Fatal("描画を待つ時間を超えました")
							}
							time.Sleep(5 * time.Millisecond)
						}
						var prepared struct{ ID string }
						if status != 200 || json.Unmarshal([]byte(body), &prepared) != nil || prepared.ID == "" {
							t.Fatal("起動用の依頼文を作れません", status, body)
						}
						status, body, _ = post("_/review/launch", fmt.Sprintf(`{"id":%q}`, prepared.ID))
						if status != 200 || body != `{"launched":true}` || calls.Load() != 1 {
							t.Fatal("差し替えた起動が呼ばれません", status, body, calls.Load())
						}
					} else if status != 405 || allow != "GET, HEAD" {
						t.Fatal("コメントなしで起動の口があります", status, body)
					}
					return nil
				},
			})
			if code != 0 || !review && calls.Load() != 0 {
				t.Fatal("起動の配線が違います", code, calls.Load())
			}
		})
	}
}
