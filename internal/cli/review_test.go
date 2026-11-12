package cli

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
