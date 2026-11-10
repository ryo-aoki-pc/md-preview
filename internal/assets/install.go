package assets

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// InstallOptions は、保存先と出力先。LockBytes・Client・ArchivePath は試験・組み込み用。
// CLI は、任意のロックや取得元を受け付けない。
type InstallOptions struct {
	Dir         string
	Stdout      io.Writer
	Stderr      io.Writer
	LockBytes   []byte
	Client      *http.Client
	ArchivePath string
}

// Install は、公式資産を取得・検証してから入れ替える。途中の知らせも標準エラーへ出す。
func Install(ctx context.Context, options InstallOptions) int {
	stderr := options.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	report := func(message string) { fmt.Fprintln(stderr, "mdpreview: "+message) }
	report("GitLab の公式の CSS・フォント・ライセンスを導入します（未導入のときは、registry.gitlab.com などから約 785 MB を取得します）")
	_, err := install(ctx, options, report)
	if err != nil {
		if ctx.Err() != nil {
			report("公式スタイルの導入を中断しました")
		} else {
			message := strings.SplitN(strings.ReplaceAll(err.Error(), "\r", "\n"), "\n", 2)[0]
			report("公式スタイルの導入に失敗しました: " + message)
		}
		return 1
	}
	return 0
}

func uuid() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}

func safeDirectory(parent, name string) (string, error) {
	target := filepath.Join(parent, name)
	if err := os.MkdirAll(target, 0700); err != nil {
		return "", err
	}
	stat, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(target)
	if err != nil || !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 || !samePath(real, target) {
		return "", errors.New("導入先にシンボリックリンクがあります。")
	}
	return target, nil
}

func takeLock(root, id string) (func() error, error) {
	name := filepath.Join(root, ".lock-"+id)
	for attempt := 0; attempt < 3; attempt++ {
		err := os.Mkdir(name, 0700)
		if err == nil {
			owner, _ := json.Marshal(struct {
				PID int `json:"pid"`
			}{os.Getpid()})
			if err := writeNew(filepath.Join(name, "owner.json"), owner); err != nil {
				_ = os.Remove(name)
				return nil, err
			}
			return func() error { return os.RemoveAll(name) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		stat, err := os.Lstat(name)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		real, err := filepath.EvalSymlinks(name)
		if err != nil || !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 || !samePath(real, name) {
			return nil, errors.New("導入ロックが不正です。")
		}
		data, err := readBoundedFile(filepath.Join(name, "owner.json"), 1024)
		if err != nil {
			return nil, errors.New("別の導入処理が実行中か、導入ロックが残っています。")
		}
		var owner struct {
			PID int `json:"pid"`
		}
		if json.Unmarshal(data, &owner) != nil || owner.PID < 1 {
			return nil, errors.New("導入ロックが不正です。")
		}
		alive, err := processAlive(owner.PID)
		if err != nil {
			return nil, err
		}
		if alive {
			return nil, errors.New("同じ版の導入処理が実行中です。")
		}
		suffix, err := uuid()
		if err != nil {
			return nil, err
		}
		stale := filepath.Join(root, ".stale-"+suffix)
		if err := os.Rename(name, stale); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := os.RemoveAll(stale); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("導入ロックを取得できませんでした。")
}

var uuidPattern = regexp.MustCompile(`^[a-f0-9-]{36}$`)

func install(ctx context.Context, options InstallOptions, log func(string)) (installed string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir := options.Dir
	if dir == "" {
		dir = DefaultDirectory()
	}
	if dir == "" {
		return "", errors.New("保存先が必要です。")
	}
	info, err := ParseLock(lockBytes(options.LockBytes))
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	release, err := takeLock(root, info.ID)
	if err != nil {
		return "", err
	}
	defer func() {
		if releaseErr := release(); err == nil && releaseErr != nil {
			installed, err = "", releaseErr
		}
	}()
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	prefix := ".stage-" + info.ID + "-"
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) && uuidPattern.MatchString(strings.TrimPrefix(entry.Name(), prefix)) {
			abandoned := filepath.Join(root, entry.Name())
			real, err := filepath.EvalSymlinks(abandoned)
			if err == nil && samePath(real, abandoned) {
				if err := os.RemoveAll(abandoned); err != nil {
					return "", err
				}
			}
		}
	}
	target := filepath.Join(root, info.ID)
	if verifyInstallation(target, info) {
		log("導入済みの公式資産を確認しました。")
		return target, nil
	}
	suffix, err := uuid()
	if err != nil {
		return "", err
	}
	stage, err := safeDirectory(root, prefix+suffix)
	if err != nil {
		return "", err
	}
	defer func() {
		if stage != "" {
			if cleanupErr := os.RemoveAll(stage); err == nil && cleanupErr != nil {
				installed, err = "", cleanupErr
			}
		}
	}()
	archive := filepath.Join(stage, ".archive.tar.gz")
	d := newDownloader(options.Client)
	if options.ArchivePath != "" {
		archive = options.ArchivePath
		f, err := os.Open(archive)
		if err != nil {
			return "", err
		}
		hash := sha256.New()
		size, readErr := io.Copy(hash, contextReader{ctx, f})
		closeErr := f.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if size != info.Lock.Archive.Size || hex.EncodeToString(hash.Sum(nil)) != info.Lock.Archive.SHA256 {
			return "", errors.New("試験アーカイブの固定値が一致しません。")
		}
	} else {
		log("公式資産を取得します（" + formatInteger(info.Lock.Archive.Size) + " バイト）。")
		if err := d.archive(ctx, info.Lock.Archive, archive); err != nil {
			return "", err
		}
	}
	if err := ExtractSelected(ctx, archive, info.Lock.Files, stage); err != nil {
		return "", err
	}
	if options.ArchivePath == "" {
		if err := os.Remove(archive); err != nil {
			return "", err
		}
	}
	for _, name := range sortedNames(info.Lock.Files) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		r := info.Lock.Files[name]
		destination := filepath.Join(stage, filepath.FromSlash(name))
		if r.URL != "" {
			data, err := d.small(ctx, r)
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				return "", err
			}
			if err := writeNew(destination, data); err != nil {
				return "", err
			}
		}
		if r.MIME == "text/css" {
			data, err := readSized(destination, r.Size)
			if err != nil {
				return "", err
			}
			css, err := RewriteCSS(string(data), name, info.Lock.Files, nil)
			if err != nil {
				return "", err
			}
			want := output(r)
			if err := checkedBytes([]byte(css), want.SHA256, want.Size, name); err != nil {
				return "", err
			}
			if err := os.WriteFile(destination, []byte(css), 0600); err != nil {
				return "", err
			}
		}
	}
	manifestJSON, err := installationJSON(info)
	if err != nil {
		return "", err
	}
	if err := writeNew(filepath.Join(stage, "install.json"), append(manifestJSON, '\n')); err != nil {
		return "", err
	}
	if !verifyInstallation(stage, info) {
		return "", errors.New("導入後の検査に失敗しました。")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	previous := ""
	if _, err := os.Lstat(target); err == nil {
		suffix, err := uuid()
		if err != nil {
			return "", err
		}
		previous = filepath.Join(root, ".previous-"+info.ID+"-"+suffix)
		if err := os.Rename(target, previous); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(stage, target); err != nil {
		if previous != "" {
			if restoreErr := os.Rename(previous, target); restoreErr != nil {
				return "", fmt.Errorf("導入先を復元できませんでした: %w", restoreErr)
			}
		}
		return "", err
	}
	stage = ""
	log("公式資産を導入しました: " + target)
	return target, nil
}

func installationJSON(info *LockInfo) ([]byte, error) {
	m := expectedManifest(info)
	var files bytes.Buffer
	files.WriteByte('{')
	order := info.fileOrder
	if len(order) == 0 {
		order = sortedNames(m.Files)
	}
	for i, name := range order {
		if i != 0 {
			files.WriteByte(',')
		}
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(m.Files[name])
		if err != nil {
			return nil, err
		}
		files.Write(key)
		files.WriteByte(':')
		files.Write(value)
	}
	files.WriteByte('}')
	return json.MarshalIndent(struct {
		Schema     int             `json:"schema"`
		ID         string          `json:"id"`
		Version    string          `json:"version"`
		LockSHA256 string          `json:"lock_sha256"`
		Files      json.RawMessage `json:"files"`
		Styles     Styles          `json:"styles"`
		Sprite     string          `json:"sprite"`
	}{m.Schema, m.ID, m.Version, m.LockSHA256, files.Bytes(), m.Styles, m.Sprite}, "", "  ")
}

func formatInteger(n int64) string {
	text := strconv.FormatInt(n, 10)
	for at := len(text) - 3; at > 0; at -= 3 {
		text = text[:at] + "," + text[at:]
	}
	return text
}
