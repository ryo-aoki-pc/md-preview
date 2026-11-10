// Package assets は、公式資産を明示的に取得し、通信せずに検査したバイト列だけを提供する。
package assets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	embedded "github.com/ryo-aoki-pc/md-preview/assets"
)

const maxSafeInteger int64 = 9007199254740991

var (
	hexHash        = regexp.MustCompile(`^[a-f0-9]{64}$`)
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	deviceName     = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)`)
	allowedMIME    = map[string]bool{"text/css": true, "text/plain": true, "application/json": true, "font/woff2": true, "image/svg+xml": true, "image/png": true, "image/gif": true}
)

// Record は、固定したファイルの取得元・原文・変換後の大きさとハッシュ。
type Record struct {
	Archive      string   `json:"archive,omitempty"`
	URL          string   `json:"url,omitempty"`
	SHA256       string   `json:"sha256"`
	Size         int64    `json:"size"`
	MIME         string   `json:"mime"`
	Licenses     []string `json:"licenses,omitempty"`
	OutputSHA256 string   `json:"output_sha256,omitempty"`
	OutputSize   *int64   `json:"output_size,omitempty"`
}

// 大きさの省略と null は、0 バイトの固定情報として受け付けない。
func requiredSize(data []byte) error {
	var raw struct {
		Size *int64 `json:"size"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.Size == nil {
		return errors.New("取得するファイルの固定値が不正です。")
	}
	return nil
}

func (r *Record) UnmarshalJSON(data []byte) error {
	if err := requiredSize(data); err != nil {
		return err
	}
	type plain Record
	return json.Unmarshal(data, (*plain)(r))
}

// Archive は、公式レジストリの index・manifest・レイヤー、または試験用の取得元。
type Archive struct {
	Registry       string `json:"registry,omitempty"`
	Repository     string `json:"repository,omitempty"`
	IndexSHA256    string `json:"index_sha256,omitempty"`
	ManifestSHA256 string `json:"manifest_sha256,omitempty"`
	URL            string `json:"url,omitempty"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
}

func (a *Archive) UnmarshalJSON(data []byte) error {
	if err := requiredSize(data); err != nil {
		return err
	}
	type plain Archive
	return json.Unmarshal(data, (*plain)(a))
}

// Styles は、順序を保った明暗の CSS 一覧。
type Styles struct {
	Light []string `json:"light"`
	Dark  []string `json:"dark"`
}

// Lock は、取得する版とファイルの許可リスト。
type Lock struct {
	Schema  int               `json:"schema"`
	Version string            `json:"version"`
	Archive Archive           `json:"archive"`
	Files   map[string]Record `json:"files"`
	Styles  Styles            `json:"styles"`
	Sprite  string            `json:"sprite"`
}

// LockInfo は、元の JSON のバイト列から決まる導入 ID と固定情報。
type LockInfo struct {
	Lock      Lock
	SHA256    string
	ID        string
	fileOrder []string
}

func digest(bytes []byte) string { hash := sha256.Sum256(bytes); return hex.EncodeToString(hash[:]) }

func relativePath(value string) error {
	if value == "" || strings.ContainsAny(value, `\:`) || strings.HasPrefix(value, "/") {
		return errors.New("資産の相対パスが不正です。")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || deviceName.MatchString(part) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return errors.New("資産の相対パスが不正です。")
		}
		for _, c := range part {
			if c <= 32 || strings.ContainsRune(`<>"|?*`, c) {
				return errors.New("資産の相対パスが不正です。")
			}
		}
	}
	return nil
}

func validateURL(value, message string) error {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		return errors.New(message)
	}
	if u.User != nil {
		return errors.New("取得元URLに認証情報を指定できません。")
	}
	return nil
}

func checkRecord(hash string, size int64) error {
	if !hexHash.MatchString(hash) || size < 0 || size > maxSafeInteger {
		return errors.New("取得するファイルの固定値が不正です。")
	}
	return nil
}

// ParseLock は、JSON のバイト列を変えずにハッシュを計算し、すべての固定情報を検査する。
func ParseLock(bytes []byte) (*LockInfo, error) {
	var lock Lock
	if err := json.Unmarshal(bytes, &lock); err != nil {
		return nil, errors.New("資産ロックの形式が不正です。")
	}
	if lock.Schema != 1 || !versionPattern.MatchString(lock.Version) || lock.Files == nil {
		return nil, errors.New("資産ロックの形式が不正です。")
	}
	if err := checkRecord(lock.Archive.SHA256, lock.Archive.Size); err != nil {
		return nil, err
	}
	if lock.Archive.Registry != "" {
		if lock.Archive.Registry != "https://registry.gitlab.com" || lock.Archive.Repository != "gitlab-org/gitlab-foss/gitlab-assets-ce" || !hexHash.MatchString(lock.Archive.IndexSHA256) || !hexHash.MatchString(lock.Archive.ManifestSHA256) {
			return nil, errors.New("CE公式レジストリの固定値が不正です。")
		}
	} else if err := validateURL(lock.Archive.URL, "取得元URLはHTTPSが必要です。"); err != nil {
		return nil, err
	}
	if len(lock.Files) == 0 || len(lock.Files) > 500 {
		return nil, errors.New("資産の個数が不正です。")
	}
	names, archiveNames := map[string]bool{}, map[string]bool{}
	for _, name := range sortedNames(lock.Files) {
		r := lock.Files[name]
		if err := relativePath(name); err != nil {
			return nil, err
		}
		if name == "install.json" || names[strings.ToLower(name)] {
			return nil, errors.New("重複した資産名です。")
		}
		names[strings.ToLower(name)] = true
		if err := checkRecord(r.SHA256, r.Size); err != nil {
			return nil, err
		}
		if !allowedMIME[r.MIME] || len(r.Licenses) == 0 {
			return nil, fmt.Errorf("%s の種別またはライセンス対応がありません。", name)
		}
		if (r.Archive != "") == (r.URL != "") {
			return nil, fmt.Errorf("%s の取得元が不正です。", name)
		}
		if r.Archive != "" {
			if err := relativePath(r.Archive); err != nil {
				return nil, err
			}
			if archiveNames[r.Archive] {
				return nil, errors.New("アーカイブ内の同じファイルを重複して指定できません。")
			}
			archiveNames[r.Archive] = true
		} else if err := validateURL(r.URL, "原文の取得元はHTTPSが必要です。"); err != nil {
			return nil, err
		}
		if r.MIME == "text/css" && (!hexHash.MatchString(r.OutputSHA256) || r.OutputSize == nil || *r.OutputSize < 0 || *r.OutputSize > maxSafeInteger) {
			return nil, fmt.Errorf("%s の変換後の固定値がありません。", name)
		}
	}
	for name, r := range lock.Files {
		for other := range lock.Files {
			if other != name && strings.HasPrefix(other, name+"/") {
				return nil, errors.New("ファイルとディレクトリの名前が衝突しています。")
			}
		}
		for _, license := range r.Licenses {
			l, ok := lock.Files[license]
			if !ok || (l.MIME != "text/plain" && l.MIME != "application/json") {
				return nil, fmt.Errorf("%s のライセンス原文が固定されていません。", name)
			}
		}
	}
	for _, list := range [][]string{lock.Styles.Light, lock.Styles.Dark} {
		if len(list) == 0 {
			return nil, errors.New("テーマのCSS一覧が不正です。")
		}
		for _, name := range list {
			if lock.Files[name].MIME != "text/css" {
				return nil, errors.New("テーマのCSS一覧が不正です。")
			}
		}
	}
	if lock.Files[lock.Sprite].MIME != "image/svg+xml" {
		return nil, errors.New("SVGスプライトが固定されていません。")
	}
	hash := digest(bytes)
	return &LockInfo{Lock: lock, SHA256: hash, ID: lock.Version + "-" + hash[:12], fileOrder: lockFileOrder(bytes)}, nil
}

// Node.js 版の再検査も同じ導入情報を使えるよう、固定 JSON のファイルの順序を保つ。
func lockFileOrder(data []byte) []string {
	var raw struct {
		Files json.RawMessage `json:"files"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw.Files))
	if _, err := decoder.Token(); err != nil {
		return nil
	}
	var result []string
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil
		}
		name, ok := token.(string)
		if !ok {
			return nil
		}
		var ignored json.RawMessage
		if decoder.Decode(&ignored) != nil {
			return nil
		}
		if !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	return result
}

func sortedNames[T any](files map[string]T) []string {
	result := make([]string, 0, len(files))
	for name := range files {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

type outputRecord struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	MIME   string `json:"mime,omitempty"`
}

func (r *outputRecord) UnmarshalJSON(data []byte) error {
	if err := requiredSize(data); err != nil {
		return err
	}
	type plain outputRecord
	return json.Unmarshal(data, (*plain)(r))
}

func output(r Record) outputRecord {
	hash, size := r.SHA256, r.Size
	if r.OutputSHA256 != "" {
		hash = r.OutputSHA256
	}
	if r.OutputSize != nil {
		size = *r.OutputSize
	}
	return outputRecord{hash, size, r.MIME}
}
func checkedBytes(bytes []byte, hash string, size int64, label string) error {
	if int64(len(bytes)) != size || digest(bytes) != hash {
		return fmt.Errorf("%s のサイズまたはSHA256が一致しません。", label)
	}
	return nil
}
func lockBytes(override []byte) []byte {
	if override != nil {
		return override
	}
	return embedded.Lock()
}
