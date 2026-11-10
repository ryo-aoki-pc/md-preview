// Package mdpreview は、単体の実行ファイルに同梱するライセンスを提供する。
package mdpreview

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"strings"

	"github.com/ryo-aoki-pc/md-preview/engine"
	"github.com/ryo-aoki-pc/md-preview/internal/version"
	"github.com/ryo-aoki-pc/md-preview/page"
)

// 元の場所で埋め込み、原文の写しを増やさない。
//
//go:embed LICENSE licenses/manifest.json all:licenses/go
var licenseFiles embed.FS

// LicenseComponent は、出力順に並べる部品と原文の場所。
type LicenseComponent struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	License string   `json:"license"`
	Files   []string `json:"files"`
}

// LicenseManifest は同梱する部品の一覧を返す。呼び出しごとに独立した写しになる。
func LicenseManifest() ([]LicenseComponent, error) {
	data, err := licenseFiles.ReadFile("licenses/manifest.json")
	if err != nil {
		return nil, err
	}
	var components []LicenseComponent
	err = json.Unmarshal(data, &components)
	return components, err
}

// ReadLicense は、リポジトリからの相対名で埋め込んだ原文を読む。
func ReadLicense(name string) ([]byte, error) {
	switch {
	case strings.HasPrefix(name, "engine/licenses/"):
		return engine.LicenseFiles.ReadFile(strings.TrimPrefix(name, "engine/"))
	case strings.HasPrefix(name, "page/lib/"):
		return page.Files.ReadFile(strings.TrimPrefix(name, "page/"))
	case name == "LICENSE" || strings.HasPrefix(name, "licenses/go/"):
		return licenseFiles.ReadFile(name)
	default:
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
}

// WriteLicenses は見出しと原文を順に書く。描画エンジンや外部コマンドは準備しない。
func WriteLicenses(w io.Writer) error {
	components, err := LicenseManifest()
	if err != nil {
		return err
	}
	for _, component := range components {
		v := component.Version
		switch component.Name {
		case "md-preview":
			v = version.Version
		case "Go":
			// 一覧の版は原文を写した元の版。出すのは、この実行ファイルをビルドした版。
			v = strings.TrimPrefix(runtime.Version(), "go")
		}
		if _, err := fmt.Fprintf(w, "============================================================\n名前: %s\n版: %s\n種類: %s\n", component.Name, v, component.License); err != nil {
			return err
		}
		for _, name := range component.Files {
			data, err := ReadLicense(name)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "\n--- %s ---\n", name); err != nil {
				return err
			}
			if _, err := w.Write(data); err != nil {
				return err
			}
			if _, err := io.WriteString(w, "\n\n"); err != nil {
				return err
			}
		}
	}
	return nil
}
