package mdpreview

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ryo-aoki-pc/md-tools/engine"
	"github.com/ryo-aoki-pc/md-tools/internal/version"
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
	if components[0].Name != "md-preview" || components[0].License != "MIT" || components[0].Version != version.Version {
		t.Fatal("先頭は md-preview 自身の版とライセンスにしてください")
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
	if err := fs.WalkDir(engine.LicenseFiles, "licenses", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !files["engine/"+name] {
			t.Errorf("一覧に無い engine の原文: %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := fs.WalkDir(licenseFiles, "licenses/go", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !files[name] {
			t.Errorf("一覧に無い Go の原文: %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLicenseGoModules(t *testing.T) {
	modules := executableGoModules(t, "./cmd/mdpreview", mdpreviewTargets)
	checkLicenseGoModules(t, modules, licenseGoModules(t, licenseManifest(t)))
}

var mdpreviewTargets = [][2]string{{"windows", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}}

func licenseGoModules(t *testing.T, components []LicenseComponent) map[string]string {
	t.Helper()
	versions := map[string]string{}
	for _, component := range components {
		if len(component.Files) > 0 && strings.HasPrefix(component.Files[0], "licenses/go/") && component.Name != "Go" {
			if _, exists := versions[component.Name]; exists {
				t.Fatalf("一覧の Go のモジュールが重複しています: %s", component.Name)
			}
			versions[component.Name] = component.Version
		}
	}
	return versions
}

func goDependencies(t *testing.T, pkg string, target [2]string, format string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-f", format, pkg)
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "CGO_ENABLED=0", "GOOS="+target[0], "GOARCH="+target[1])
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s の依存を取得できません (%s/%s): %v\n%s", pkg, target[0], target[1], err, stderr.String())
	}
	return strings.Split(strings.TrimSpace(string(output)), "\n")
}

func executableGoModules(t *testing.T, pkg string, targets [][2]string) map[string]string {
	t.Helper()
	modules := map[string]string{}
	for _, target := range targets {
		for _, line := range goDependencies(t, pkg, target, "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			if len(fields) != 2 {
				t.Fatalf("Go のモジュールの出力が不正です: %s", line)
			}
			if version, exists := modules[fields[0]]; exists && version != fields[1] {
				t.Fatalf("対象によって Go のモジュールの版が違います: %s %s / %s", fields[0], version, fields[1])
			}
			modules[fields[0]] = fields[1]
		}
	}
	if len(modules) == 0 {
		t.Fatal("実行ファイルの Go のモジュールがありません")
	}
	return modules
}

func checkLicenseGoModules(t *testing.T, modules, versions map[string]string) {
	t.Helper()
	for name, version := range modules {
		if versions[name] != version {
			t.Errorf("Go のモジュールの版が違うか一覧にありません: %s 依存=%s 一覧=%s", name, version, versions[name])
		}
	}
	for name := range versions {
		if _, exists := modules[name]; !exists {
			t.Errorf("依存に無い Go のモジュールが一覧にあります: %s", name)
		}
	}
}

func TestLicenseGoRequirements(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?m)^\s*(replace|exclude)\s`).Match(data) {
		t.Fatal("replace・exclude を使うときは、同梱ライセンスと依存の照合方法を見直してください")
	}
	versions := licenseGoModules(t, licenseManifest(t))
	manifest, err := os.ReadFile("mdtable/licenses/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var components []LicenseComponent
	if err := json.Unmarshal(manifest, &components); err != nil {
		t.Fatal(err)
	}
	for name, version := range licenseGoModules(t, components) {
		if previous, exists := versions[name]; exists && previous != version {
			t.Fatalf("実行ファイルの一覧で Go のモジュールの版が違います: %s %s / %s", name, previous, version)
		}
		versions[name] = version
	}
	// 直接・間接の require と両実行ファイルの一覧の和を比べ、使わない依存も検知する。
	modules := map[string]string{}
	for _, module := range goModRequirements(string(data)) {
		modules[module[1]] = module[2]
	}
	if len(modules) == 0 {
		t.Fatal("go.mod のモジュールがありません")
	}
	checkLicenseGoModules(t, modules, versions)
}

func TestExecutableDependencies(t *testing.T) {
	const module = "github.com/ryo-aoki-pc/md-tools/"
	for _, test := range []struct {
		pkg     string
		targets [][2]string
		blocked []string
	}{
		{"./cmd/mdpreview", mdpreviewTargets, []string{"github.com/xuri/", module + "mdtable/internal/workbook", module + "mdtable/internal/sync", module + "mdtable/internal/cli"}},
		{"./mdtable/cmd/mdtable", [][2]string{{"windows", "amd64"}}, []string{module + "engine", module + "page", module + "assets", module + "internal"}},
	} {
		t.Run(test.pkg, func(t *testing.T) {
			for _, target := range test.targets {
				t.Run(target[0]+"/"+target[1], func(t *testing.T) {
					for _, dependency := range goDependencies(t, test.pkg, target, "{{.ImportPath}}") {
						dependency = strings.TrimSpace(dependency)
						for _, blocked := range test.blocked {
							if dependency == blocked || strings.HasPrefix(dependency, strings.TrimSuffix(blocked, "/")+"/") {
								t.Errorf("%s に不要な依存があります: %s", test.pkg, dependency)
							}
						}
					}
				})
			}
		})
	}
}

// goModRequirements は、go.mod の require を、かっこでまとめた形と 1 行の形の両方から読む
// (モジュールが 1 つだけのまとまりは、1 行の「require <モジュール> <版>」になる)。
func goModRequirements(data string) [][]string {
	requirement := regexp.MustCompile(`(?m)^\s*(?:require\s+)?([^\s()]+)\s+(v[^\s]+)(?:\s+//[^\n]*)?$`)
	return requirement.FindAllStringSubmatch(data, -1)
}

func TestGoModRequirements(t *testing.T) {
	data := "module example.com/app\n\ngo 1.27.3\n\ntoolchain go1.27.3\n\n" +
		"require (\n\texample.com/a v1.2.3\n\texample.com/b/v2 v2.0.0 // indirect\n)\n\n" +
		"require example.com/c v0.1.0\n\nrequire example.com/d v0.0.0-20260101000000-abcdef012345 // indirect\n\n" +
		"replace example.com/x v1.0.0 => example.com/y v1.0.1\n"
	got := map[string]string{}
	for _, module := range goModRequirements(data) {
		got[module[1]] = module[2]
	}
	want := map[string]string{"example.com/a": "v1.2.3", "example.com/b/v2": "v2.0.0", "example.com/c": "v0.1.0", "example.com/d": "v0.0.0-20260101000000-abcdef012345"}
	if len(got) != len(want) {
		t.Fatalf("読んだモジュール: %v", got)
	}
	for name, version := range want {
		if got[name] != version {
			t.Errorf("%s: %s (期待 %s)", name, got[name], version)
		}
	}
}

// 配布のビルドは go.mod の版の Go を使う (GitHub Actions は go.mod から版を決める)。写した元の版も、それに合わせる。
func TestLicenseGoVersion(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	directive := regexp.MustCompile(`(?m)^go (\d+\.\d+\.\d+)$`).FindStringSubmatch(string(data))
	if directive == nil {
		t.Fatal("go.mod の go の行は、修正版まで含む版にしてください")
	}
	for _, component := range licenseManifest(t) {
		if component.Name == "Go" {
			if component.Version != directive[1] {
				t.Fatalf("Go の版が違います: 一覧=%s go.mod=%s", component.Version, directive[1])
			}
			return
		}
	}
	t.Fatal("一覧に Go がありません")
}

func TestLicenseGoOriginals(t *testing.T) {
	output, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Skipf("このホストでは go を動かせないので、原文の写しを元と比べられません: %v", err)
	}
	cache := strings.TrimSpace(string(output))
	for _, component := range licenseManifest(t) {
		if !strings.HasPrefix(component.Files[0], "licenses/go/") {
			continue
		}
		t.Run(component.Name, func(t *testing.T) {
			var source string
			if component.Name == "Go" {
				// 原文は修正版の間で変わらない前提で、このホストの Go の版にかかわらず比べる (変わったら、ここで知らせる)。
				source = runtime.GOROOT()
			} else {
				// Go のモジュールキャッシュでは大文字を ! と小文字にする。
				var b strings.Builder
				for _, r := range component.Name {
					if r >= 'A' && r <= 'Z' {
						b.WriteByte('!')
						b.WriteRune(r + ('a' - 'A'))
					} else {
						b.WriteRune(r)
					}
				}
				source = filepath.Join(cache, filepath.FromSlash(b.String()+"@"+component.Version))
			}
			for _, file := range component.Files {
				t.Run(filepath.Base(file), func(t *testing.T) {
					original, err := os.ReadFile(filepath.Join(source, filepath.Base(file)))
					if os.IsNotExist(err) {
						t.Skipf("このホストに原文がありません: %s / %s", component.Name, filepath.Base(file))
					}
					if err != nil {
						t.Fatal(err)
					}
					copy, err := ReadLicense(file)
					if err != nil || !bytes.Equal(original, copy) {
						t.Errorf("原文の写しがバイト単位で一致しません: %s (%v)", file, err)
					}
				})
			}
		})
	}
}

func TestLicenseEngineVersions(t *testing.T) {
	data, err := os.ReadFile("engine/engine.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Gem    struct{ Name, Version string }
		Comrak string
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	cargo, err := os.ReadFile("engine/Cargo.lock")
	if err != nil {
		t.Fatal(err)
	}
	crateVersions := map[string]bool{}
	for _, pair := range regexp.MustCompile(`\[\[package\]\]\s+name = "([^"]+)"\s+version = "([^"]+)"`).FindAllStringSubmatch(string(cargo), -1) {
		crateVersions[pair[1]+"-"+pair[2]] = true
	}
	directories := map[string]bool{}
	for _, c := range licenseManifest(t) {
		if c.Name == lock.Gem.Name && (c.Version != lock.Gem.Version || c.Files[0] != "engine/licenses/gitlab-glfm-markdown.txt") {
			t.Errorf("gem の版か原文の場所が違います: %+v", c)
		}
		if !strings.HasPrefix(c.Files[0], "engine/licenses/") || c.Name == lock.Gem.Name {
			continue
		}
		if c.Name == "comrak" && c.Version != lock.Comrak {
			t.Errorf("engine.lock.json の comrak の版が違います: %s / %s", c.Version, lock.Comrak)
		}
		id := c.Name + "-" + c.Version
		if !crateVersions[id] {
			t.Errorf("Cargo.lock に無い crate の版: %s", id)
		}
		directories[id] = true
		for _, file := range c.Files {
			if !strings.HasPrefix(file, "engine/licenses/"+id+"/") {
				t.Errorf("crate のフォルダーの版が違います: %s", file)
			}
		}
	}
	entries, err := os.ReadDir("engine/licenses")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() && !directories[entry.Name()] {
			t.Errorf("一覧に無い crate のフォルダー: %s", entry.Name())
		}
	}
}

func TestLicenseBrowserVersions(t *testing.T) {
	data, err := os.ReadFile("page/lib/lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Packages map[string]struct{ Name, Version string }
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Packages["md-table-sticky"]; !ok {
		t.Fatal("表の固定のライブラリがありません")
	}
	versions := map[string]string{}
	for _, c := range licenseManifest(t) {
		versions[c.Name] = c.Version
	}
	for name, library := range lock.Packages {
		if versions[name] != library.Version {
			t.Errorf("ブラウザ用ライブラリの版が違います: %s 一覧=%s lock=%s", name, versions[name], library.Version)
		}
	}
}
