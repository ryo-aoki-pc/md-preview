package cli

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	mdpreview "github.com/ryo-aoki-pc/md-preview"
	"github.com/ryo-aoki-pc/md-preview/internal/server"
	"github.com/ryo-aoki-pc/md-preview/internal/version"
)

func TestLicensesAction(t *testing.T) {
	var out, errout bytes.Buffer
	code := Run(t.Context(), []string{"--licenses"}, IO{Stdout: &out, Stderr: &errout}, Dependencies{
		InitializeRenderer: func(context.Context) error { t.Fatal("描画エンジンを準備しました"); return nil },
		CreateDiagram: func(context.Context) (server.DiagramRenderer, error) {
			t.Fatal("PlantUML を準備しました")
			return nil, nil
		},
		LoadAssets: func(string) (server.OfficialAssets, string) {
			t.Fatal("公式資産を読みました")
			return nil, ""
		},
	})
	if code != 0 || errout.Len() != 0 {
		t.Fatal(code, errout.String())
	}
	components, err := mdpreview.LicenseManifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range components {
		v := c.Version
		switch c.Name {
		case "md-preview":
			v = version.Version
		case "Go":
			v = strings.TrimPrefix(runtime.Version(), "go")
		}
		if !strings.Contains(out.String(), "名前: "+c.Name+"\n版: "+v+"\n種類: "+c.License+"\n") {
			t.Errorf("部品の見出しがありません: %s", c.Name)
		}
		for _, file := range c.Files {
			data, err := mdpreview.ReadLicense(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(out.Bytes(), data) {
				t.Errorf("原文の先頭行を含む全文がありません: %s", file)
			}
		}
	}
	if !strings.HasSuffix(Help, "mdpreview --help | --version | --licenses\n") {
		t.Fatal("使い方の最後の行に --licenses がありません")
	}
	for _, args := range [][]string{{"--licenses", "a.md"}, {"--help", "--licenses"}, {"--licenses", "--version"}, {"--stdio", "--licenses"}} {
		out.Reset()
		errout.Reset()
		if code := Run(t.Context(), args, IO{Stdout: &out, Stderr: &errout}, Dependencies{}); code != 2 || out.Len() != 0 || !strings.Contains(errout.String(), "単独で指定してください") {
			t.Error(args, code, errout.String())
		}
	}
}

type licenseErrorWriter struct{}

func (licenseErrorWriter) Write([]byte) (int, error) {
	return 0, errors.New("試験用の書き込み失敗")
}

func TestLicensesOutputFailure(t *testing.T) {
	var errout bytes.Buffer
	if code := Run(t.Context(), []string{"--licenses"}, IO{Stdout: licenseErrorWriter{}, Stderr: &errout}, Dependencies{}); code != 1 || errout.String() != "mdpreview: ライセンスを書き込めません\n" {
		t.Fatal(code, errout.String())
	}
}
