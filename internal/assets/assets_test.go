package assets

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	embedded "github.com/ryo-aoki-pc/md-preview/assets"
)

const fixtureCSS = `/* url(/assets/ignored) */ .x{src:url(/assets/fonts/a.woff2);mask:url("./icons.svg#one")}`

var fixtureFont = []byte{0, 255, 9, 10, 13, 128}

type tarEntry struct {
	name string
	body []byte
	kind byte
	pax  map[string]string
	link string
}

func archiveBytes(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	w := tar.NewWriter(gz)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		size := int64(len(entry.body))
		if kind == tar.TypeSymlink || kind == tar.TypeLink {
			size = 0
		}
		header := &tar.Header{Name: entry.name, Mode: 0600, Size: size, Typeflag: kind, Linkname: entry.link, PAXRecords: entry.pax}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if size > 0 {
			if _, err := w.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func defaultEntries() []tarEntry {
	return []tarEntry{{name: "assets/main.css", body: []byte(fixtureCSS)}, {name: "assets/fonts/a.woff2", body: fixtureFont}, {name: "assets/icons.svg", body: []byte("<svg/>")}}
}
func int64ptr(n int64) *int64 { return &n }

type fixture struct {
	t          *testing.T
	dir        string
	data       string
	blob       []byte
	license    []byte
	lock       Lock
	server     *httptest.Server
	requests   atomic.Int32
	hold       bool
	held       chan struct{}
	once       sync.Once
	badArchive bool
	badLicense bool
}

func assetFixture(t *testing.T, blob []byte, hold bool) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir(), blob: blob, license: []byte("試験用の原文\n"), hold: hold, held: make(chan struct{})}
	f.data = filepath.Join(f.dir, "data")
	if f.blob == nil {
		f.blob = archiveBytes(t, defaultEntries())
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		switch r.URL.Path {
		case "/archive":
			if f.hold {
				_, _ = w.Write(f.blob[:min(8, len(f.blob))])
				w.(http.Flusher).Flush()
				f.once.Do(func() { close(f.held) })
				<-r.Context().Done()
				return
			}
			if f.badArchive {
				_, _ = w.Write(append(bytes.Clone(f.blob), 0))
				return
			}
			_, _ = w.Write(f.blob)
		case "/license":
			if f.badLicense {
				_, _ = w.Write([]byte("改変した原文"))
				return
			}
			_, _ = w.Write(f.license)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { f.server.CloseClientConnections(); f.server.Close() })
	record := func(body []byte, mime string) Record {
		return Record{SHA256: digest(body), Size: int64(len(body)), MIME: mime, Licenses: []string{"licenses/source.txt"}}
	}
	files := map[string]Record{"main.css": record([]byte(fixtureCSS), "text/css"), "fonts/a.woff2": record(fixtureFont, "font/woff2"), "icons.svg": record([]byte("<svg/>"), "image/svg+xml"), "licenses/source.txt": record(f.license, "text/plain")}
	for _, name := range []string{"main.css", "fonts/a.woff2", "icons.svg"} {
		r := files[name]
		r.Archive = "assets/" + name
		files[name] = r
	}
	r := files["licenses/source.txt"]
	r.URL = f.server.URL + "/license"
	files["licenses/source.txt"] = r
	css, err := RewriteCSS(fixtureCSS, "main.css", files, nil)
	if err != nil {
		t.Fatal(err)
	}
	r = files["main.css"]
	r.OutputSHA256 = digest([]byte(css))
	r.OutputSize = int64ptr(int64(len(css)))
	files["main.css"] = r
	// 起動側の「4 個以上」の既存の条件も、取得と同じ見本で検査する。
	f.lock = Lock{Schema: 1, Version: "1.2.3", Archive: Archive{URL: f.server.URL + "/archive", SHA256: digest(f.blob), Size: int64(len(f.blob))}, Files: files, Styles: Styles{[]string{"main.css", "main.css", "main.css", "main.css"}, []string{"main.css", "main.css", "main.css", "main.css"}}, Sprite: "icons.svg"}
	return f
}
func (f *fixture) lockJSON() []byte {
	f.t.Helper()
	data, err := json.MarshalIndent(f.lock, "", "  ")
	if err != nil {
		f.t.Fatal(err)
	}
	return append(data, '\n')
}
func (f *fixture) info() *LockInfo {
	f.t.Helper()
	info, err := ParseLock(f.lockJSON())
	if err != nil {
		f.t.Fatal(err)
	}
	return info
}
func (f *fixture) install(ctx context.Context) (string, error) {
	return install(ctx, InstallOptions{Dir: f.data, LockBytes: f.lockJSON()}, func(string) {})
}
func (f *fixture) installed() string {
	f.t.Helper()
	target, err := f.install(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	return target
}
func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func writeFile(t *testing.T, name string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, body, 0600); err != nil {
		t.Fatal(err)
	}
}
func requireFailure(t *testing.T, err error, part string) {
	t.Helper()
	if err == nil || part != "" && !strings.Contains(err.Error(), part) {
		t.Fatalf("期待する失敗 (%s): %v", part, err)
	}
}
func directoryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

func TestEmbeddedLock(t *testing.T) {
	data := embedded.Lock()
	info, err := ParseLock(data)
	if err != nil || info.ID != "19.5.1-d5c33f28bf66" {
		t.Fatalf("埋め込んだ固定情報: %v %v", info, err)
	}
	if !bytes.Equal(data, readFile(t, filepath.Join("..", "..", "assets", "assets.lock.json"))) {
		t.Fatal("固定情報のバイト列が変わりました")
	}
	data[0] = 'x'
	if embedded.Lock()[0] != '{' {
		t.Fatal("固定情報の実体を外へ渡しました")
	}
}

func TestManifestRetainsOriginalLockOrder(t *testing.T) {
	info, err := ParseLock(embedded.Lock())
	if err != nil {
		t.Fatal(err)
	}
	data, err := installationJSON(info)
	if err != nil {
		t.Fatal(err)
	}
	order := lockFileOrder(data)
	if len(order) < 2 || order[0] != "application_dark-a3de088d254f447173abe48a05ec6b7ac075668b58b5be1aa26c5012bb0f8395.css" || order[1] != "application-79f356d98146549c21e5657de00ff91a957f995329d1725db0bb2a7251d2b59a.css" {
		t.Fatalf("固定情報とファイルの順序が異なります: %v", order)
	}
}

func TestInstallRewriteBinaryAndOfflineReinstall(t *testing.T) {
	f := assetFixture(t, nil, false)
	target := f.installed()
	var m manifest
	if err := json.Unmarshal(readFile(t, filepath.Join(target, "install.json")), &m); err != nil {
		t.Fatal(err)
	}
	if m.ID != f.info().ID || !reflect.DeepEqual(m, expectedManifest(f.info())) {
		t.Fatal("導入情報が一致しません")
	}
	if !strings.Contains(string(readFile(t, filepath.Join(target, "main.css"))), `url("./fonts/a.woff2")`) {
		t.Fatal("CSS を相対化していません")
	}
	if !bytes.Equal(readFile(t, filepath.Join(target, "fonts", "a.woff2")), fixtureFont) || !bytes.Equal(readFile(t, filepath.Join(target, "licenses", "source.txt")), f.license) {
		t.Fatal("原文のバイト列が変わりました")
	}
	if _, err := os.Stat(filepath.Join(target, ".archive.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("取得用のアーカイブが残っています")
	}
	before := f.requests.Load()
	var stdout, stderr bytes.Buffer
	if code := Install(t.Context(), InstallOptions{Dir: f.data, LockBytes: f.lockJSON(), Stdout: &stdout, Stderr: &stderr}); code != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "導入済みの公式資産を確認しました。") {
		t.Fatalf("導入済みの確認: %d %q %q", code, stdout.String(), stderr.String())
	}
	if f.requests.Load() != before {
		t.Fatal("導入済みの確認から通信しました")
	}
}

func TestInstallCommandMessages(t *testing.T) {
	f := assetFixture(t, nil, false)
	var stdout, stderr bytes.Buffer
	if code := Install(t.Context(), InstallOptions{Dir: f.data, LockBytes: f.lockJSON(), Stdout: &stdout, Stderr: &stderr}); code != 0 || stdout.Len() != 0 {
		t.Fatalf("導入コマンド: %d %q", code, stderr.String())
	}
	for _, message := range []string{"mdpreview: GitLab の公式の CSS・フォント・ライセンスを導入します", "公式資産を取得します（" + formatInteger(int64(len(f.blob))) + " バイト）。", "公式資産を導入しました: "} {
		if !strings.Contains(stderr.String(), message) {
			t.Errorf("知らせがありません: %s", message)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stderr.Reset()
	if code := Install(ctx, InstallOptions{Dir: f.data, LockBytes: f.lockJSON(), Stderr: &stderr}); code != 1 || !strings.Contains(stderr.String(), "公式スタイルの導入を中断しました") {
		t.Fatalf("中断の知らせ: %d %q", code, stderr.String())
	}
}

func TestRepairAndVersionRetention(t *testing.T) {
	f := assetFixture(t, nil, false)
	old := f.installed()
	writeFile(t, filepath.Join(old, "fonts", "a.woff2"), []byte("破損"))
	if target := f.installed(); target != old || !bytes.Equal(readFile(t, filepath.Join(target, "fonts", "a.woff2")), fixtureFont) {
		t.Fatal("同じ版を修復できませんでした")
	}
	found := false
	for _, name := range directoryNames(t, f.data) {
		if strings.HasPrefix(name, ".previous-") {
			found = true
		}
	}
	if !found {
		t.Fatal("以前の内容を退避していません")
	}
	f.lock.Version = "1.2.4"
	current := f.installed()
	if current == old || !strings.Contains(string(readFile(t, filepath.Join(old, "install.json"))), `"1.2.3"`) {
		t.Fatal("旧版を残していません")
	}
}

func TestBadArchivePreservesOldInstallation(t *testing.T) {
	for _, kind := range []string{"hash", "short-size", "long-size", "actual-long"} {
		t.Run(kind, func(t *testing.T) {
			f := assetFixture(t, nil, false)
			old := f.installed()
			f.lock.Version = "1.2.4"
			switch kind {
			case "hash":
				f.lock.Archive.SHA256 = strings.Repeat("0", 64)
			case "short-size":
				f.lock.Archive.Size--
			case "long-size":
				f.lock.Archive.Size++
			case "actual-long":
				f.badArchive = true
			}
			_, err := f.install(t.Context())
			requireFailure(t, err, "")
			if !reflect.DeepEqual(directoryNames(t, f.data), []string{filepath.Base(old)}) {
				t.Fatal("失敗後に完了印か一時領域が残っています")
			}
			if !strings.Contains(string(readFile(t, filepath.Join(old, "install.json"))), `"1.2.3"`) {
				t.Fatal("取得の失敗で旧版を変えました")
			}
		})
	}
}

func TestSelectedTarLinksDuplicatesMissingAndCorruption(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "duplicate", "missing", "gzip-truncated", "gzip-crc", "tar-truncated", "member-size", "member-hash"} {
		t.Run(kind, func(t *testing.T) {
			entries := defaultEntries()
			switch kind {
			case "symlink":
				entries[0].kind = tar.TypeSymlink
				entries[0].link = "outside"
			case "hardlink":
				entries[0].kind = tar.TypeLink
				entries[0].link = "outside"
			case "duplicate":
				entries = append(entries, entries[0])
			case "missing":
				entries = entries[:1]
			case "member-size":
				entries[0].body = []byte("短い")
			case "member-hash":
				entries[0].body = bytes.Repeat([]byte("x"), len(fixtureCSS))
			}
			blob := archiveBytes(t, entries)
			switch kind {
			case "gzip-truncated":
				blob = blob[:len(blob)-4]
			case "gzip-crc":
				blob[len(blob)-8] ^= 255
			case "tar-truncated":
				var out bytes.Buffer
				w := gzip.NewWriter(&out)
				_, _ = w.Write([]byte("途中"))
				_ = w.Close()
				blob = out.Bytes()
			}
			f := assetFixture(t, blob, false)
			_, err := f.install(t.Context())
			requireFailure(t, err, "")
			if len(directoryNames(t, f.data)) != 0 {
				t.Fatal("不正なアーカイブを完了扱いにしました")
			}
		})
	}
}

func TestPAXLongNameAndUnselectedPaths(t *testing.T) {
	entries := defaultEntries()
	longName := "assets/" + strings.Repeat("nested/", 20) + "main.css"
	entries[0].name = longName
	entries[0].body = []byte(`a{src:url(/assets/fonts/a.woff2);mask:url(/assets/icons.svg#one)}`)
	entries = append(entries, tarEntry{name: "../../outside.txt", body: []byte("展開しない")}, tarEntry{name: "../outside-link", kind: tar.TypeSymlink, link: "../outside"})
	f := assetFixture(t, archiveBytes(t, entries), false)
	r := f.lock.Files["main.css"]
	r.Archive = longName
	r.SHA256 = digest(entries[0].body)
	r.Size = int64(len(entries[0].body))
	f.lock.Files["main.css"] = r
	css, err := RewriteCSS(string(entries[0].body), "main.css", f.lock.Files, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.OutputSHA256 = digest([]byte(css))
	r.OutputSize = int64ptr(int64(len(css)))
	f.lock.Files["main.css"] = r
	target := f.installed()
	if !strings.Contains(string(readFile(t, filepath.Join(target, "main.css"))), "fonts/a.woff2") {
		t.Fatal("PAX の path を読めませんでした")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "outside.txt")); !os.IsNotExist(err) {
		t.Fatal("選択外のパスを展開しました")
	}
}

func TestInterruptedAndConcurrentInstall(t *testing.T) {
	f := assetFixture(t, nil, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := f.install(ctx); first <- err }()
	select {
	case <-f.held:
	case <-time.After(5 * time.Second):
		t.Fatal("取得が始まりませんでした")
	}
	_, err := f.install(t.Context())
	requireFailure(t, err, "実行中")
	cancel()
	select {
	case err := <-first:
		requireFailure(t, err, "")
	case <-time.After(5 * time.Second):
		t.Fatal("取得を中断できませんでした")
	}
	if len(directoryNames(t, f.data)) != 0 {
		t.Fatal("中断後に一時領域か排他ロックが残っています")
	}
}

func TestLicenseAndOutputMismatch(t *testing.T) {
	for _, kind := range []string{"license-hash", "license-short", "license-long", "license-body", "output"} {
		t.Run(kind, func(t *testing.T) {
			f := assetFixture(t, nil, false)
			name := "licenses/source.txt"
			if kind == "output" {
				name = "main.css"
			}
			r := f.lock.Files[name]
			switch kind {
			case "license-hash":
				r.SHA256 = strings.Repeat("0", 64)
			case "license-short":
				r.Size--
			case "license-long":
				r.Size++
			case "license-body":
				f.badLicense = true
			case "output":
				r.OutputSHA256 = strings.Repeat("0", 64)
			}
			f.lock.Files[name] = r
			_, err := f.install(t.Context())
			requireFailure(t, err, "")
			if len(directoryNames(t, f.data)) != 0 {
				t.Fatal("原文の検査が失敗したのに導入しました")
			}
		})
	}
}

func TestStaleLockAndStageRecovery(t *testing.T) {
	f := assetFixture(t, nil, false)
	info := f.info()
	lock := filepath.Join(f.data, ".lock-"+info.ID)
	writeFile(t, filepath.Join(lock, "owner.json"), []byte(`{"pid":1073741823}`))
	writeFile(t, filepath.Join(f.data, ".stage-"+info.ID+"-00000000-0000-0000-0000-000000000000", ".archive.tar.gz"), []byte("中断した取得"))
	f.installed()
	if !reflect.DeepEqual(directoryNames(t, f.data), []string{info.ID}) {
		t.Fatal("終了済みの排他ロックと中断領域を回収していません")
	}
}

func TestLocalArchiveStillVerifies(t *testing.T) {
	f := assetFixture(t, nil, false)
	archive := filepath.Join(f.dir, "local.tar.gz")
	writeFile(t, archive, f.blob)
	options := InstallOptions{Dir: f.data, LockBytes: f.lockJSON(), ArchivePath: archive}
	if _, err := install(t.Context(), options, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if f.requests.Load() != 1 {
		t.Fatal("ローカルのアーカイブがあるときも原文の取得だけ行います")
	}
	f.lock.Version = "1.2.4"
	options.LockBytes = f.lockJSON()
	writeFile(t, archive, []byte("改変"))
	_, err := install(t.Context(), options, func(string) {})
	requireFailure(t, err, "試験アーカイブ")
}

func TestLoadAllRecordsAndImmutableAllowlist(t *testing.T) {
	f := assetFixture(t, nil, false)
	if LoadWithLock(f.data, f.lockJSON()) != nil {
		t.Fatal("未導入の資産を読みました")
	}
	target := f.installed()
	loaded := LoadWithLock(f.data, f.lockJSON())
	if loaded == nil || loaded.ID() != f.info().ID {
		t.Fatal("検査済みの資産を読めませんでした")
	}
	for _, item := range []struct{ name, mime string }{{"main.css", "text/css; charset=utf-8"}, {"fonts/a.woff2", "font/woff2"}, {"icons.svg", "image/svg+xml"}, {"licenses/source.txt", "text/plain; charset=utf-8"}} {
		if _, mime, ok := loaded.File(item.name); !ok || mime != item.mime {
			t.Errorf("MIME: %s %q %v", item.name, mime, ok)
		}
	}
	body, _, _ := loaded.File("fonts/a.woff2")
	body[0] = 99
	body, _, _ = loaded.File("fonts/a.woff2")
	if !bytes.Equal(body, fixtureFont) {
		t.Fatal("許可リストのバイト列が書き換わりました")
	}
	for _, name := range []string{"unknown.css", "install.json", "../lock.json", "%6dain.css", "/main.css", "MAIN.CSS"} {
		if _, _, ok := loaded.File(name); ok {
			t.Errorf("許可リスト外: %s", name)
		}
	}
	writeFile(t, filepath.Join(target, "fonts", "a.woff2"), []byte("後から改変"))
	body, _, _ = loaded.File("fonts/a.woff2")
	if !bytes.Equal(body, fixtureFont) {
		t.Fatal("検査後のファイルを開き直しました")
	}
	if LoadWithLock(f.data, f.lockJSON()) != nil {
		t.Fatal("改変したファイルを読み直しました")
	}
	config, err := json.Marshal(loaded.PageConfig())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal(config, &decoded) != nil || len(decoded) != 4 || decoded["id"] != loaded.ID() || decoded["version"] != "1.2.3" || decoded["sprite"] != "icons.svg" {
		t.Fatalf("ページ設定: %s", config)
	}
	var absent *Loaded
	if absent.ID() != "" || absent.PageConfig() != nil {
		t.Fatal("資産無しの設定が不正です")
	}
}

func TestLoadRejectsManifestAndFileTampering(t *testing.T) {
	for _, kind := range []string{"hash", "version", "id", "schema", "styles", "extra", "missing", "size", "sha", "file-missing", "file-same-size"} {
		t.Run(kind, func(t *testing.T) {
			f := assetFixture(t, nil, false)
			target := f.installed()
			var m manifest
			if err := json.Unmarshal(readFile(t, filepath.Join(target, "install.json")), &m); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "hash":
				m.LockSHA256 = strings.Repeat("0", 64)
			case "version":
				m.Version = "other"
			case "id":
				m.ID = "other"
			case "schema":
				m.Schema = 2
			case "styles":
				m.Styles.Light[0] = "missing.css"
			case "extra":
				m.Files["../outside.css"] = outputRecord{strings.Repeat("0", 64), 0, "text/css"}
			case "missing":
				delete(m.Files, "licenses/source.txt")
			case "size":
				r := m.Files["main.css"]
				r.Size++
				m.Files["main.css"] = r
			case "sha":
				r := m.Files["main.css"]
				r.SHA256 = strings.Repeat("0", 64)
				m.Files["main.css"] = r
			case "file-missing":
				if err := os.Remove(filepath.Join(target, "licenses", "source.txt")); err != nil {
					t.Fatal(err)
				}
			case "file-same-size":
				writeFile(t, filepath.Join(target, "fonts", "a.woff2"), []byte{0, 255, 8, 10, 13, 128})
			}
			data, _ := json.Marshal(m)
			writeFile(t, filepath.Join(target, "install.json"), data)
			if LoadWithLock(f.data, f.lockJSON()) != nil {
				t.Fatal("壊れた導入状態を受け付けました")
			}
		})
	}
}

func TestCheckHasNoNetwork(t *testing.T) {
	f := assetFixture(t, nil, false)
	for _, valid := range []bool{false, true, false} {
		if valid {
			f.installed()
		} else if _, err := os.Stat(f.data); err == nil {
			writeFile(t, filepath.Join(f.data, f.info().ID, "main.css"), []byte("破損"))
		}
		before := f.requests.Load()
		code, message := CheckWithLock(f.data, f.lockJSON())
		want := 1
		if valid {
			want = 0
		}
		if code != want || !strings.HasPrefix(message, "公式スタイル") {
			t.Fatalf("通信しない検査: %d %s", code, message)
		}
		if f.requests.Load() != before {
			t.Fatal("検査から通信しました")
		}
	}
}

func TestNoticeOnlyVersionDirectories(t *testing.T) {
	f := assetFixture(t, nil, false)
	for _, dir := range []string{"", filepath.Join(f.dir, "none"), f.dir} {
		if Notice(nil, dir) != "" {
			t.Fatal("何も導入されていないのに知らせました")
		}
	}
	stray := filepath.Join(f.dir, "stray")
	for _, name := range []string{".stage-1.2.3-0123456789ab-x", ".lock-1.2.3-0123456789ab"} {
		if err := os.MkdirAll(filepath.Join(stray, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(stray, "desktop.ini"), nil)
	writeFile(t, filepath.Join(stray, "1.2.3-0123456789ab"), []byte("版の名前のファイル"))
	if Notice(nil, stray) != "" {
		t.Fatal("作業用の領域か OS のファイルを版と見なしました")
	}
	target := f.installed()
	loaded := LoadWithLock(f.data, f.lockJSON())
	if Notice(loaded, f.data) != "" {
		t.Fatal("利用できる資産があるのに知らせました")
	}
	writeFile(t, filepath.Join(target, "main.css"), []byte("破損"))
	if !strings.Contains(Notice(nil, f.data), "mdpreview install-style") {
		t.Fatal("利用できない版のフォルダーの知らせがありません")
	}
}

func TestDefaultDirectories(t *testing.T) {
	for _, item := range []struct {
		os         string
		env        map[string]string
		home, want string
	}{
		{"linux", map[string]string{"XDG_DATA_HOME": "/data"}, "/home/test", "/data/md-preview-assets"},
		{"linux", map[string]string{"XDG_DATA_HOME": ""}, "/home/test", "/home/test/.local/share/md-preview-assets"},
		{"linux", map[string]string{"XDG_DATA_HOME": "relative"}, "/home/test", "/home/test/.local/share/md-preview-assets"},
		{"darwin", nil, "/home/test", "/home/test/Library/Application Support/md-preview-assets"},
		{"windows", map[string]string{"LOCALAPPDATA": `C:\Data`}, `C:\Home`, `C:\Data\md-preview-assets`},
		{"windows", map[string]string{"LOCALAPPDATA": `\\server\share`}, `C:\Home`, `\\server\share\md-preview-assets`},
		{"windows", map[string]string{"USERPROFILE": `C:\Home`}, `C:\Home`, `C:\Home\AppData\Local\md-preview-assets`},
		{"windows", nil, `C:\Home`, `C:\Home\AppData\Local\md-preview-assets`},
	} {
		got := DefaultDirectoryFor(func(name string) string { return item.env[name] }, item.os, item.home)
		if got != item.want {
			t.Errorf("既定の保存先: %s %q != %q", item.os, got, item.want)
		}
	}
	isolated := t.TempDir()
	t.Setenv("LOCALAPPDATA", isolated)
	t.Setenv("USERPROFILE", isolated)
	t.Setenv("HOME", isolated)
	t.Setenv("XDG_DATA_HOME", isolated)
	if runtime.GOOS != "darwin" && DefaultDirectory() != filepath.Join(isolated, "md-preview-assets") {
		t.Fatal("差し替えた環境から既定の保存先を求めていません")
	}
	if code, _ := Check(""); code != 1 {
		t.Fatal("一時の未導入の保存先を有効にしました")
	}
}

func TestLinksOutsideAndVersionFolder(t *testing.T) {
	for _, kind := range []string{"file", "folder", "manifest", "lock"} {
		t.Run(kind, func(t *testing.T) {
			f := assetFixture(t, nil, false)
			target := f.installed()
			if kind == "lock" {
				if err := os.RemoveAll(target); err != nil {
					t.Fatal(err)
				}
				external := filepath.Join(f.dir, "outside-lock")
				if err := os.Mkdir(external, 0700); err != nil {
					t.Fatal(err)
				}
				createTestLink(t, external, filepath.Join(f.data, ".lock-"+f.info().ID), true)
				_, err := f.install(t.Context())
				requireFailure(t, err, "導入ロック")
				return
			}
			switch kind {
			case "file":
				name := filepath.Join(target, "main.css")
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
				external := filepath.Join(f.dir, "outside.css")
				writeFile(t, external, []byte("外"))
				createTestLink(t, external, name, false)
			case "folder":
				moved := filepath.Join(f.dir, "moved")
				if err := os.Rename(target, moved); err != nil {
					t.Fatal(err)
				}
				createTestLink(t, moved, target, true)
			case "manifest":
				name := filepath.Join(target, "install.json")
				data := readFile(t, name)
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
				external := filepath.Join(f.dir, "outside-install.json")
				writeFile(t, external, data)
				createTestLink(t, external, name, false)
			}
			if LoadWithLock(f.data, f.lockJSON()) != nil {
				t.Fatal("外へのリンクか版のフォルダーのリンクを受け付けました")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRegistryIndexManifestAndLayer(t *testing.T) {
	for _, kind := range []string{"valid", "index-hash", "index-platform", "manifest-hash", "layer-size", "layer-hash", "layer-type", "layer-count", "auth"} {
		t.Run(kind, func(t *testing.T) {
			f := assetFixture(t, nil, false)
			layer := map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": "sha256:" + digest(f.blob), "size": len(f.blob)}
			switch kind {
			case "layer-size":
				layer["size"] = len(f.blob) + 1
			case "layer-hash":
				layer["digest"] = "sha256:" + strings.Repeat("0", 64)
			case "layer-type":
				layer["mediaType"] = "application/zip"
			}
			layers := []any{layer}
			if kind == "layer-count" {
				layers = append(layers, layer)
			}
			manifestData, _ := json.Marshal(map[string]any{"schemaVersion": 2, "layers": layers})
			platform := map[string]string{"os": "linux", "architecture": "amd64"}
			if kind == "index-platform" {
				platform["architecture"] = "arm64"
			}
			indexData, _ := json.Marshal(map[string]any{"manifests": []any{map[string]any{"digest": "sha256:" + digest(manifestData), "platform": platform}}})
			f.lock.Archive = Archive{Registry: "https://registry.gitlab.com", Repository: "gitlab-org/gitlab-foss/gitlab-assets-ce", IndexSHA256: digest(indexData), ManifestSHA256: digest(manifestData), SHA256: digest(f.blob), Size: int64(len(f.blob))}
			if kind == "index-hash" {
				f.lock.Archive.IndexSHA256 = strings.Repeat("0", 64)
			}
			if kind == "manifest-hash" {
				f.lock.Archive.ManifestSHA256 = strings.Repeat("0", 64)
				indexData, _ = json.Marshal(map[string]any{"manifests": []any{map[string]any{"digest": "sha256:" + f.lock.Archive.ManifestSHA256, "platform": platform}}})
				f.lock.Archive.IndexSHA256 = digest(indexData)
			}
			var visitedMu sync.Mutex
			var visited []string
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				visitedMu.Lock()
				visited = append(visited, r.URL.Path)
				visitedMu.Unlock()
				switch {
				case r.URL.Path == "/jwt/auth":
					if kind == "auth" {
						fmt.Fprint(w, `{}`)
					} else {
						fmt.Fprint(w, `{"token":"public-fixture"}`)
					}
				case strings.Contains(r.URL.Path, "/blobs/"):
					if r.Header.Get("Authorization") != "Bearer public-fixture" {
						t.Error("公開の認証応答を使っていません")
					}
					_, _ = w.Write(f.blob)
				case strings.HasSuffix(r.URL.Path, "/manifests/sha256:"+f.lock.Archive.IndexSHA256):
					_, _ = w.Write(indexData)
				default:
					_, _ = w.Write(manifestData)
				}
			}))
			defer registry.Close()
			local, _ := url.Parse(registry.URL)
			transport := http.DefaultTransport.(*http.Transport).Clone()
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "gitlab.com" || r.URL.Host == "registry.gitlab.com" {
					clone := r.Clone(r.Context())
					u := *r.URL
					u.Scheme = local.Scheme
					u.Host = local.Host
					clone.URL = &u
					clone.Host = local.Host
					return transport.RoundTrip(clone)
				}
				if r.URL.Hostname() != "127.0.0.1" {
					return nil, fmt.Errorf("試験外の通信先を拒みます")
				}
				return transport.RoundTrip(r)
			})}
			_, err := install(t.Context(), InstallOptions{Dir: f.data, LockBytes: f.lockJSON(), Client: client}, func(string) {})
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				visitedMu.Lock()
				defer visitedMu.Unlock()
				if len(visited) != 4 || !strings.Contains(visited[3], "/blobs/") {
					t.Fatalf("取得した順序: %v", visited)
				}
			} else {
				requireFailure(t, err, "")
				if len(directoryNames(t, f.data)) != 0 {
					t.Fatal("公式レジストリの検査に失敗したのに導入しました")
				}
			}
		})
	}
}
