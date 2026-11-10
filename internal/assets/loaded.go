package assets

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
)

type manifest struct {
	Schema     int                     `json:"schema"`
	ID         string                  `json:"id"`
	Version    string                  `json:"version"`
	LockSHA256 string                  `json:"lock_sha256"`
	Files      map[string]outputRecord `json:"files"`
	Styles     Styles                  `json:"styles"`
	Sprite     string                  `json:"sprite"`
}
type loadedFile struct {
	body []byte
	mime string
}

// Loaded は、検査したバイト列だけを持つ。同時に読んでよい。
type Loaded struct {
	id, version, sprite string
	styles              Styles
	files               map[string]loadedFile
}

// ID は、版とロックの sha256 の先頭 12 桁からなる導入 ID。
func (l *Loaded) ID() string {
	if l == nil {
		return ""
	}
	return l.id
}

// PageConfig は、ページへ渡す id・version・styles・sprite を返す。
func (l *Loaded) PageConfig() any {
	if l == nil {
		return nil
	}
	return struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		Styles  Styles `json:"styles"`
		Sprite  string `json:"sprite"`
	}{l.id, l.version, cloneStyles(l.styles), l.sprite}
}

// File は、許可リストにある検査済みのバイト列の写しと MIME を返す。ファイルは開き直さない。
func (l *Loaded) File(name string) (body []byte, mime string, ok bool) {
	if l == nil {
		return nil, "", false
	}
	f, ok := l.files[name]
	if !ok {
		return nil, "", false
	}
	return bytes.Clone(f.body), f.mime, true
}

func cloneStyles(s Styles) Styles {
	return Styles{append([]string(nil), s.Light...), append([]string(nil), s.Dark...)}
}

// DefaultDirectory は、アプリの導入先から独立した公式資産の既定の保存先。
func DefaultDirectory() string {
	home, _ := os.UserHomeDir()
	return DefaultDirectoryFor(os.Getenv, runtime.GOOS, home)
}

// DefaultDirectoryFor は、環境を明示して既定の保存先を求める。保存先へは触れない。
func DefaultDirectoryFor(env func(string) string, goos, home string) string {
	if env == nil {
		env = os.Getenv
	}
	if goos == "windows" {
		base := env("LOCALAPPDATA")
		if base == "" {
			profile := env("USERPROFILE")
			if profile == "" {
				profile = home
			}
			base = windowsJoin(profile, "AppData", "Local")
		}
		return windowsJoin(base, "md-preview-assets")
	}
	if goos == "darwin" {
		// macOS では Application Support の下に置く。
		return path.Join(home, "Library", "Application Support", "md-preview-assets")
	}
	base := env("XDG_DATA_HOME")
	if !strings.HasPrefix(base, "/") {
		base = path.Join(home, ".local", "share")
	}
	return path.Join(base, "md-preview-assets")
}

func windowsJoin(parts ...string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(parts...)
	}
	value := strings.Join(parts, "/")
	value = strings.ReplaceAll(value, "\\", "/")
	unc := strings.HasPrefix(value, "//")
	value = path.Clean(value)
	if unc {
		value = "/" + value
	}
	return strings.ReplaceAll(value, "/", "\\")
}

var loadName = regexp.MustCompile(`^[\w./@-]+$`)
var loadVersion = regexp.MustCompile(`^[\w.-]+$`)
var noticeVersion = regexp.MustCompile(`^[\w][\w.-]*-[a-f0-9]{12}$`)

func expectedManifest(info *LockInfo) manifest {
	files := make(map[string]outputRecord, len(info.Lock.Files))
	for name, r := range info.Lock.Files {
		files[name] = output(r)
	}
	return manifest{1, info.ID, info.Lock.Version, info.SHA256, files, info.Lock.Styles, info.Lock.Sprite}
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
func inside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func resolvedFile(root, name string) (string, error) {
	if err := relativePath(name); err != nil {
		return "", err
	}
	for _, part := range strings.Split(name, "/") {
		if strings.EqualFold(part, ".git") {
			return "", errors.New("公式資産の検査に失敗しました")
		}
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil || !inside(root, full) {
		return "", errors.New("公式資産の検査に失敗しました")
	}
	stat, err := os.Stat(full)
	if err != nil || !stat.Mode().IsRegular() {
		return "", errors.New("公式資産の検査に失敗しました")
	}
	return full, nil
}

func readSized(name string, size int64) ([]byte, error) {
	stat, err := os.Stat(name)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != size {
		return nil, errors.New("公式資産の検査に失敗しました")
	}
	// 大きさを調べた後にファイルが伸びても、必要以上にメモリーへ取り込まない。
	return readBoundedFile(name, size)
}

// Load は、保存先のロック・導入情報・全ファイルを、通信せずに検査する。失敗したら nil。
func Load(dir string) *Loaded { return LoadWithLock(dir, nil) }

// LoadWithLock は、指定した固定情報で検査する。置換ロックは試験・組み込み用で、CLI からは受け付けない。
func LoadWithLock(dir string, lockJSON []byte) *Loaded {
	if dir == "" {
		dir = DefaultDirectory()
	}
	lockJSON = lockBytes(lockJSON)
	var lock Lock
	if json.Unmarshal(lockJSON, &lock) != nil || !loadVersion.MatchString(lock.Version) || len(lock.Files) == 0 {
		return nil
	}
	hash := digest(lockJSON)
	id := lock.Version + "-" + hash[:12]
	folder := filepath.Join(dir, id)
	stat, err := os.Lstat(folder)
	if err != nil || !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	root, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return nil
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil
	}
	manifestPath, err := resolvedFile(root, "install.json")
	if err != nil {
		return nil
	}
	data, err := readBoundedFile(manifestPath, 4*1024*1024)
	if err != nil {
		return nil
	}
	var m manifest
	if json.Unmarshal(data, &m) != nil || m.Schema != 1 || m.ID != id || m.Version != lock.Version || m.LockSHA256 != hash || m.Files == nil || !reflect.DeepEqual(m.Styles, lock.Styles) || m.Sprite != lock.Sprite {
		return nil
	}
	files := make(map[string]loadedFile, len(m.Files))
	for name, r := range m.Files {
		if !loadName.MatchString(name) || relativePath(name) != nil {
			return nil
		}
		expected, exists := lock.Files[name]
		want := output(expected)
		if !exists || r.Size != want.Size || r.SHA256 != want.SHA256 || checkRecord(r.SHA256, r.Size) != nil {
			return nil
		}
		mime := mimeFor(name)
		if mime == "" {
			return nil
		}
		target, err := resolvedFile(root, name)
		if err != nil {
			return nil
		}
		body, err := readSized(target, r.Size)
		if err != nil || checkedBytes(body, r.SHA256, r.Size, name) != nil {
			return nil
		}
		files[name] = loadedFile{body, mime}
	}
	if len(files) != len(lock.Files) {
		return nil
	}
	for _, styles := range [][]string{m.Styles.Light, m.Styles.Dark} {
		if len(styles) < 4 {
			return nil
		}
		for _, name := range styles {
			if files[name].mime != "text/css; charset=utf-8" {
				return nil
			}
		}
	}
	if _, ok := files[m.Sprite]; !ok {
		return nil
	}
	return &Loaded{id, lock.Version, lock.Sprite, cloneStyles(m.Styles), files}
}

func mimeFor(name string) string {
	return map[string]string{"css": "text/css; charset=utf-8", "woff2": "font/woff2", "woff": "font/woff", "ttf": "font/ttf", "eot": "application/vnd.ms-fontobject", "svg": "image/svg+xml", "png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp", "avif": "image/avif", "ico": "image/x-icon", "txt": "text/plain; charset=utf-8", "md": "text/plain; charset=utf-8", "json": "application/json; charset=utf-8"}[strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))]
}

// Notice は、版のフォルダーがあるのに使えないときだけ知らせる。
func Notice(loaded *Loaded, dir string) string {
	if loaded != nil || dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && noticeVersion.MatchString(entry.Name()) {
			return "公式スタイルを使えません（壊れているか、この版のものではありません）。mdpreview install-style で取得し直せます"
		}
	}
	return ""
}

// Check は、通信せずに導入状態を検査し、終了コードと利用者向けの文を返す。
func Check(dir string) (int, string) { return CheckWithLock(dir, nil) }

// CheckWithLock は、試験・組み込み用の置換ロックで、通信せずに検査する。
func CheckWithLock(dir string, lockJSON []byte) (int, string) {
	if LoadWithLock(dir, lockJSON) != nil {
		return 0, "公式スタイルを利用できます"
	}
	return 1, "公式スタイルが未導入または破損しています"
}

func verifyInstallation(directory string, info *LockInfo) bool {
	stat, err := os.Lstat(directory)
	if err != nil || !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 {
		return false
	}
	root, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return false
	}
	data, err := readBoundedFile(filepath.Join(root, "install.json"), 4*1024*1024)
	if err != nil {
		return false
	}
	var m manifest
	if json.Unmarshal(data, &m) != nil || !reflect.DeepEqual(m, expectedManifest(info)) {
		return false
	}
	found := make(map[string]bool)
	err = filepath.WalkDir(root, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("導入先にシンボリックリンクがあります。")
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if name == "install.json" {
			return nil
		}
		r, ok := m.Files[name]
		if !ok || !entry.Type().IsRegular() {
			return errors.New("公式資産の検査に失敗しました")
		}
		body, err := readSized(full, r.Size)
		if err != nil {
			return err
		}
		if err := checkedBytes(body, r.SHA256, r.Size, name); err != nil {
			return err
		}
		found[name] = true
		return nil
	})
	return err == nil && len(found) == len(m.Files)
}
