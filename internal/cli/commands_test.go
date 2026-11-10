package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ryo-aoki-pc/md-preview/engine"
	"github.com/ryo-aoki-pc/md-preview/internal/assets"
	"github.com/ryo-aoki-pc/md-preview/internal/pipeline"
	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
	"github.com/ryo-aoki-pc/md-preview/internal/server"
)

func commandRoot(t *testing.T) string {
	t.Helper()
	root, err := resolve.RealPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func TestRenderCommandOutputAndErrors(t *testing.T) {
	e, err := engine.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	root := commandRoot(t)
	file := filepath.Join(root, "doc.md")
	if err := os.WriteFile(file, []byte("# 文書\n\n![欠落](missing.png)\n\n#123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{Renderer: pipeline.New(e)}
	var out, errout bytes.Buffer
	code := Run(t.Context(), []string{"render", "--no-plantuml", "--root", root, file}, IO{Stdout: &out, Stderr: &errout}, deps)
	if code != 0 || !strings.HasPrefix(out.String(), "<!doctype html>") || !strings.Contains(errout.String(), "見つからないメディア: 1 件") || !strings.Contains(errout.String(), "参照: 1 件") {
		t.Fatal(code, errout.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(errout.String()), "\n") {
		if !strings.HasPrefix(line, "mdpreview: ") {
			t.Fatal(line)
		}
	}
	output := filepath.Join(root, "out.html")
	out.Reset()
	errout.Reset()
	code = Run(t.Context(), []string{"render", "--no-plantuml", "-o", output, file}, IO{Stdout: &out, Stderr: &errout}, deps)
	if code != 0 || out.Len() != 0 {
		t.Fatal(code, errout.String())
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"render"}, {"render", "--theme", "bad", file}, {"render", "--width", "bad", file}, {"render", "--port", "1", file}, {"render", "-o"}, {"render", filepath.Join(root, "missing.md")}} {
		out.Reset()
		errout.Reset()
		code := Run(t.Context(), args, IO{Stdout: &out, Stderr: &errout}, deps)
		want := 2
		if len(args) == 2 && args[1] == filepath.Join(root, "missing.md") {
			want = 1
		}
		if code != want || out.Len() != 0 || !strings.HasPrefix(errout.String(), "mdpreview: ") || strings.Count(errout.String(), "\n") != 1 {
			t.Fatal(args, code, errout.String())
		}
	}
}
func TestOptionalComponentsAndPreparation(t *testing.T) {
	root := commandRoot(t)
	file := filepath.Join(root, "doc.md")
	_ = os.WriteFile(file, []byte("本文"), 0600)
	for _, args := range [][]string{{"--help"}, {"--version"}, {"install-style", "--dir", root, "--check"}, {"--no-open", "--no-plantuml", "--no-official-style", file}, {"--stdio", "--no-plantuml", "--no-official-style"}, {"render", "--no-plantuml", file}} {
		prepared, diagrams, styles := 0, 0, 0
		deps := Dependencies{Renderer: fakeRenderer{}, InitializeRenderer: func(context.Context) error { prepared++; return nil }, CreateDiagram: func(context.Context) (server.DiagramRenderer, error) { diagrams++; return nil, nil }, LoadAssets: func(string) (server.OfficialAssets, string) { styles++; return nil, "" }, OnStart: func(a *App) error { return a.Close() }}
		code := Run(t.Context(), args, IO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}, deps)
		want := 1
		if args[0] == "--help" || args[0] == "--version" || args[0] == "install-style" {
			want = 0
		}
		if prepared != want || diagrams != 0 || styles != 0 || code > 1 {
			t.Fatal(args, prepared, diagrams, styles, code)
		}
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func digest(b []byte) string                                              { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func TestInstallStyleCommandFakeRegistryAndOfflineCheck(t *testing.T) {
	dir := commandRoot(t)
	css, license := []byte(".x{color:red}"), []byte("試験用のライセンス\n")
	sprite := []byte("<svg/>")
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tarfile := tar.NewWriter(gz)
	if err := tarfile.WriteHeader(&tar.Header{Name: "assets/main.css", Mode: 0600, Size: int64(len(css))}); err != nil {
		t.Fatal(err)
	}
	_, _ = tarfile.Write(css)
	if err := tarfile.WriteHeader(&tar.Header{Name: "assets/icons.svg", Mode: 0600, Size: int64(len(sprite))}); err != nil {
		t.Fatal(err)
	}
	_, _ = tarfile.Write(sprite)
	_ = tarfile.Close()
	_ = gz.Close()
	blob := compressed.Bytes()
	manifest, _ := json.Marshal(map[string]any{"layers": []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": "sha256:" + digest(blob), "size": len(blob)}}})
	index, _ := json.Marshal(map[string]any{"manifests": []any{map[string]any{"digest": "sha256:" + digest(manifest), "platform": map[string]string{"os": "linux", "architecture": "amd64"}}}})
	var requests atomic.Int32
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.URL.Path == "/license":
			_, _ = w.Write(license)
		case r.URL.Path == "/jwt/auth":
			_, _ = io.WriteString(w, `{"token":"fixture"}`)
		case strings.HasSuffix(r.URL.Path, "/manifests/sha256:"+digest(index)):
			_, _ = w.Write(index)
		case strings.HasSuffix(r.URL.Path, "/manifests/sha256:"+digest(manifest)):
			_, _ = w.Write(manifest)
		case strings.Contains(r.URL.Path, "/blobs/"):
			_, _ = w.Write(blob)
		default:
			t.Error("予期しない取得先: ", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer registry.Close()
	local, _ := url.Parse(registry.URL)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		target := *r.URL
		target.Scheme, target.Host = local.Scheme, local.Host
		clone.URL = &target
		clone.Host = local.Host
		return transport.RoundTrip(clone)
	})}
	outputSize := int64(len(css))
	style := assets.Record{Archive: "assets/main.css", SHA256: digest(css), Size: int64(len(css)), MIME: "text/css", Licenses: []string{"licenses/source.txt"}, OutputSHA256: digest(css), OutputSize: &outputSize}
	original := assets.Record{URL: "https://gitlab.com/license", SHA256: digest(license), Size: int64(len(license)), MIME: "text/plain", Licenses: []string{"licenses/source.txt"}}
	icon := assets.Record{Archive: "assets/icons.svg", SHA256: digest(sprite), Size: int64(len(sprite)), MIME: "image/svg+xml", Licenses: []string{"licenses/source.txt"}}
	lock := assets.Lock{Schema: 1, Version: "1.2.3", Archive: assets.Archive{Registry: "https://registry.gitlab.com", Repository: "gitlab-org/gitlab-foss/gitlab-assets-ce", IndexSHA256: digest(index), ManifestSHA256: digest(manifest), SHA256: digest(blob), Size: int64(len(blob))}, Files: map[string]assets.Record{"main.css": style, "licenses/source.txt": original, "icons.svg": icon}, Styles: assets.Styles{Light: []string{"main.css", "main.css", "main.css", "main.css"}, Dark: []string{"main.css", "main.css", "main.css", "main.css"}}, Sprite: "icons.svg"}
	lockBytes, _ := json.Marshal(lock)
	deps := Dependencies{StyleInstallOptions: assets.InstallOptions{LockBytes: lockBytes, Client: client}}
	var out, errout bytes.Buffer
	for _, check := range []bool{true, false, true} {
		before := requests.Load()
		out.Reset()
		errout.Reset()
		args := []string{"install-style", "--dir", dir}
		if check {
			args = append(args, "--check")
		}
		code := Run(t.Context(), args, IO{Stdout: &out, Stderr: &errout}, deps)
		if before == 0 && check {
			if code != 1 || errout.String() != "mdpreview: 公式スタイルが未導入または破損しています\n" {
				t.Fatal(code, errout.String())
			}
		} else if code != 0 {
			t.Fatal(code, errout.String())
		}
		if check && requests.Load() != before {
			t.Fatal("--check が通信しました")
		}
		if !check && requests.Load() != 5 {
			t.Fatal("偽のレジストリの取得回数: ", requests.Load())
		}
		if out.Len() != 0 {
			t.Fatal("標準出力へ知らせを書きました")
		}
	}
}
