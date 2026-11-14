package stdio

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReviewNegotiation(t *testing.T) {
	for _, value := range []string{"", "true", `"1"`, "2", "0", "null", "{}", "[]", "1", "1.0"} {
		for _, editing := range []bool{false, true} {
			t.Run(fmt.Sprintf("review=%s/edit=%t", value, editing), func(t *testing.T) {
				suffix, expected := "", ""
				if editing {
					suffix, expected = `,"edit":1`, `,"edit":1`
				}
				if value != "" {
					suffix += `,"review":` + value
				}
				enabled := value == "1" || value == "1.0"
				if enabled {
					expected += `,"review":1`
				}
				a := startEditApp(t, `{"type":"hello","protocol":1`+suffix+`}`, 0)
				a.out.Lock()
				lines := strings.Split(a.out.data.String(), "\n")
				a.out.Unlock()
				want := fmt.Sprintf(`{"type":"ready","protocol":1,"version":"試験版","url":%q%s}`, a.server.URL, expected)
				if lines[0] != want || lines[1] != `{"type":"clients","count":0}` {
					t.Fatalf("出力のバイト列が違う: %q", lines)
				}
				r, err := http.Get(a.server.URL)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(r.Body)
				_ = r.Body.Close()
				if strings.Contains(string(body), `"review":true`) != enabled || strings.Contains(string(body), `"reviewBuffer":true`) != enabled || strings.Contains(string(body), `"edit":{`) != editing {
					t.Fatal("ページの設定が違う", string(body))
				}
				for _, route := range []string{"tables?render=1", "source?render=1&tag=p&range=1:1-1:6", "prepare", "launch"} {
					method := "GET"
					if route == "prepare" || route == "launch" {
						method = "POST"
					}
					req, _ := http.NewRequest(method, a.server.URL+"_/review/"+route, strings.NewReader(`{}`))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", a.server.Port))
					r, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					_, _ = io.Copy(io.Discard, r.Body)
					_ = r.Body.Close()
					want := 404
					if method == "POST" {
						want = 400
					}
					if !enabled {
						want = 405
						if method == "GET" {
							want = 404
						}
					}
					if r.StatusCode != want {
						t.Fatal(route, r.StatusCode, want)
					}
				}
			})
		}
	}
}
