package stdio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
)

func TestImageNegotiationAndDisk(t *testing.T) {
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			hello := `{"type":"hello","protocol":1}`
			if enabled {
				hello = `{"type":"hello","protocol":1,"edit":1}`
			}
			a := startEditApp(t, hello, 0)
			root, err := resolve.RealPath(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "main.md")
			if err = os.WriteFile(file, []byte("ディスク\n"), 0644); err != nil {
				t.Fatal(err)
			}
			src := a.document(t, file, 7, 1)
			post := func(render uint64) (int, string) {
				t.Helper()
				req, err := http.NewRequest("POST", fmt.Sprintf("%s_/edit/image?render=%d", a.server.URL, render), bytes.NewReader(body.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "image/png")
				req.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", a.server.Port))
				r, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
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
			status, response := post(src.Render)
			if !enabled {
				if status != 405 {
					t.Fatal(status, response)
				}
				if _, err := os.Stat(filepath.Join(root, "images")); !os.IsNotExist(err) {
					t.Fatal("編集の通知なしで画像を作った", err)
				}
				return
			}
			var path string
			if err := json.Unmarshal([]byte(response), &path); err != nil || status != 200 || !strings.HasPrefix(path, "images/img-") || filepath.IsAbs(path) {
				t.Fatal(status, response, err)
			}
			imageBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil || !bytes.Equal(imageBytes, body.Bytes()) {
				t.Fatal(err)
			}
			text, _ := os.ReadFile(file)
			if string(text) != "ディスク\n" || len(a.edits()) != 0 {
				t.Fatal("画像の保存で Markdown やバッファを変えた", string(text), a.edits())
			}
			a.document(t, file, 7, 2)
			if status, response := post(src.Render); status != 409 || !strings.Contains(response, `"code":"stale"`) {
				t.Fatal(status, response)
			}
			unnamed := a.document(t, nil, 8, 3)
			if status, response := post(unnamed.Render); status != 422 || !strings.Contains(response, "名前のある文書") {
				t.Fatal(status, response)
			}
		})
	}
}
