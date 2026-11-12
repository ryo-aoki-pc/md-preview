package mdtable

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/internal/version"
)

func licenseManifest(t *testing.T) []LicenseComponent {
	t.Helper()
	components, err := LicenseManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(components) == 0 {
		t.Fatal("ライセンスの一覧が空です")
	}
	return components
}

func TestLicenseManifestFiles(t *testing.T) {
	components := licenseManifest(t)
	self := components[0]
	if self.Name != "md-table-excel" || self.License != "MIT" || self.Version != version.Version || len(self.Files) != 1 || self.Files[0] != "LICENSE" {
		t.Fatal("先頭は md-table-excel 自身の版と MIT ライセンスにしてください")
	}
	ids, files := map[string]bool{}, map[string]bool{}
	for _, c := range components {
		id := c.Name + "@" + c.Version
		if c.Name == "" || c.Version == "" || c.License == "" || len(c.Files) == 0 || ids[id] {
			t.Errorf("部品の一覧が不正です: %+v", c)
		}
		ids[id] = true
		for _, name := range c.Files {
			if !fs.ValidPath(name) || files[name] {
				t.Errorf("原文の場所が不正または重複しています: %s", name)
			}
			files[name] = true
			data, err := ReadLicense(name)
			if err != nil || len(bytes.TrimSpace(data)) == 0 {
				t.Errorf("埋め込んだ原文がありません: %s (%v)", name, err)
			}
			source, err := os.ReadFile(filepath.FromSlash(name))
			if err != nil || !bytes.Equal(data, source) {
				t.Errorf("埋め込んだ原文が元の場所と違います: %s (%v)", name, err)
			}
		}
	}
	if err := fs.WalkDir(licenseFiles, "licenses", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && name != "licenses/manifest.json" && !files[name] {
			t.Errorf("一覧に無い原文: %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLicenseGoModules(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	versions := map[string]string{}
	for _, c := range licenseManifest(t) {
		if c.Name != "Go" && strings.HasPrefix(c.Files[0], "licenses/go/") {
			versions[c.Name] = c.Version
		}
	}
	// 直接・間接の require を両方読み、追加・更新・削除時の同梱漏れを検知する。
	modules := goModRequirements(string(data))
	if len(modules) == 0 {
		t.Fatal("go.mod のモジュールがありません")
	}
	for _, module := range modules {
		if versions[module[1]] != module[2] {
			t.Errorf("Go のモジュールの版が違うか一覧にありません: %s go.mod=%s 一覧=%s", module[1], module[2], versions[module[1]])
		}
		delete(versions, module[1])
	}
	if len(versions) > 0 {
		t.Errorf("go.mod に無いモジュール: %v", versions)
	}
	if regexp.MustCompile(`(?m)^\s*(replace|exclude)\s`).Match(data) {
		t.Fatal("replace・exclude を使うときは、同梱ライセンスと依存の照合方法を見直してください")
	}
}

func goModRequirements(data string) [][]string {
	requirement := regexp.MustCompile(`(?m)^\s*(?:require\s+)?([^\s()]+)\s+(v[^\s]+)(?:\s+//[^\n]*)?$`)
	return requirement.FindAllStringSubmatch(data, -1)
}

func TestLicenseGoOriginals(t *testing.T) {
	output, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	cache := strings.TrimSpace(string(output))
	goFound := false
	for _, c := range licenseManifest(t) {
		if !strings.HasPrefix(c.Files[0], "licenses/go/") {
			continue
		}
		source := runtime.GOROOT()
		if c.Name == "Go" {
			goFound = true
			// 一覧は原文を写した元の版。別の Go で検査するときも原文の変更を検知する。
			if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(c.Version) {
				t.Errorf("Go の原文を写した版が不正です: %s", c.Version)
			}
		} else {
			var escaped strings.Builder
			for _, r := range c.Name {
				if r >= 'A' && r <= 'Z' {
					escaped.WriteByte('!')
					escaped.WriteRune(r + ('a' - 'A'))
				} else {
					escaped.WriteRune(r)
				}
			}
			source = filepath.Join(cache, filepath.FromSlash(escaped.String()+"@"+c.Version))
		}
		for _, file := range c.Files {
			original, err := os.ReadFile(filepath.Join(source, filepath.Base(file)))
			if err != nil {
				t.Errorf("原文を読めません: %s (%v)", file, err)
				continue
			}
			copy, err := ReadLicense(file)
			if err != nil || !bytes.Equal(original, copy) {
				t.Errorf("原文の写しがバイト単位で一致しません: %s (%v)", file, err)
			}
		}
	}
	if !goFound {
		t.Fatal("一覧に Go がありません")
	}
}

func TestLicenseTheme(t *testing.T) {
	source, err := os.ReadFile("internal/workbook/theme.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range licenseManifest(t) {
		if !strings.HasPrefix(c.Files[0], "licenses/openpyxl/") {
			continue
		}
		if c.License != "MIT" || !strings.Contains(string(source), "openpyxl "+c.Version+" の") {
			t.Fatal("テーマとライセンスの版が一致しません")
		}
		var original strings.Builder
		copying := false
		for _, line := range strings.Split(string(source), "\n") {
			if strings.HasPrefix(line, "// Copyright") {
				copying = true
			}
			if copying {
				if !strings.HasPrefix(line, "//") {
					break
				}
				original.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "//"), " "))
				original.WriteByte('\n')
			}
		}
		data, err := ReadLicense(c.Files[0])
		if err != nil || string(data) != original.String() {
			t.Fatalf("テーマの原文と同梱ライセンスが一致しません: %v", err)
		}
		return
	}
	t.Fatal("一覧に openpyxl のテーマがありません")
}

func TestWriteLicenses(t *testing.T) {
	var out bytes.Buffer
	if err := WriteLicenses(&out); err != nil {
		t.Fatal(err)
	}
	self, err := ReadLicense("LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	prefix := "============================================================\n名前: md-table-excel\n版: " + version.Version + "\n種類: MIT\n\n--- LICENSE ---\n" + string(self)
	if !strings.HasPrefix(out.String(), prefix) {
		t.Fatal("出力の先頭に md-table-excel 自身の MIT ライセンスの全文がありません")
	}
	for _, c := range licenseManifest(t) {
		v := c.Version
		switch c.Name {
		case "md-table-excel":
			v = version.Version
		case "Go":
			v = strings.TrimPrefix(runtime.Version(), "go")
		}
		if !strings.Contains(out.String(), "名前: "+c.Name+"\n版: "+v+"\n種類: "+c.License+"\n") {
			t.Errorf("部品の見出しがありません: %s", c.Name)
		}
		for _, file := range c.Files {
			data, err := ReadLicense(file)
			if err != nil || !bytes.Contains(out.Bytes(), data) {
				t.Errorf("原文の全文がありません: %s (%v)", file, err)
			}
		}
	}
	err = errors.New("試験用の書き込み失敗")
	if !errors.Is(WriteLicenses(licenseErrorWriter{err}), err) {
		t.Fatal("書き込みの失敗が返りません")
	}
}

func TestWriteLicensesBuildVersion(t *testing.T) {
	original := version.Version
	t.Cleanup(func() { version.Version = original })
	version.Version = "0.6.0-test"
	var out bytes.Buffer
	if err := WriteLicenses(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "名前: md-table-excel\n版: "+version.Version+"\n種類: MIT\n") {
		t.Fatal("自身のライセンスにビルド時の版が使われていません")
	}
}

type licenseErrorWriter struct{ err error }

func (w licenseErrorWriter) Write([]byte) (int, error) { return 0, w.err }
