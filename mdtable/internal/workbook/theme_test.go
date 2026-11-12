package workbook

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"path/filepath"
	"testing"

	"github.com/ryo-aoki-pc/md-table-excel/internal/table"
)

// workbook.write が openpyxl.writer.theme.theme_xml と同じテーマを結び付けることを確かめる。
func TestWorkbookTheme(t *testing.T) {
	directory := t.TempDir()
	markdown := filepath.Join(directory, "document.md")
	filename := filepath.Join(directory, "book.xlsx")
	region := table.FindTables(table.NewMarkdown(markdown, "| h |\n| --- |\n| value |\n", false))[0]
	if e := Write(filename, markdown, []*ExportTable{{Region: region, Model: region.Model(), Same: 1}}, nil, "test"); e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(filename)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	parts := map[string][]byte{}
	for _, part := range z.File {
		f, e := part.Open()
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(f)
		f.Close()
		if e != nil {
			t.Fatal(e)
		}
		parts[part.Name] = data
	}
	theme := parts["xl/theme/theme1.xml"]
	if !bytes.Equal(theme, []byte(defaultTheme)) {
		t.Fatal("テーマの XML が変わった")
	}
	decoder := xml.NewDecoder(bytes.NewReader(theme))
	japanese := 0
	for {
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "font" {
			continue
		}
		script, typeface := "", ""
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "script":
				script = a.Value
			case "typeface":
				typeface = a.Value
			}
		}
		if script == "Jpan" {
			japanese++
			if typeface != "ＭＳ Ｐゴシック" {
				t.Errorf("日本語の字体 %q", typeface)
			}
		}
	}
	if japanese != 2 {
		t.Fatalf("日本語の字体の指定が %d 個", japanese)
	}
	var types struct {
		Parts []struct {
			Name string `xml:"PartName,attr"`
			Type string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if e = xml.Unmarshal(parts["[Content_Types].xml"], &types); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, p := range types.Parts {
		if p.Name == "/xl/theme/theme1.xml" && p.Type == "application/vnd.openxmlformats-officedocument.theme+xml" {
			found = true
		}
	}
	if !found {
		t.Fatal("テーマのコンテンツ型がない")
	}
	var rels struct {
		Items []struct {
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if e = xml.Unmarshal(parts["xl/_rels/workbook.xml.rels"], &rels); e != nil {
		t.Fatal(e)
	}
	found = false
	for _, r := range rels.Items {
		if r.Type == "http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" && r.Target == "theme/theme1.xml" {
			found = true
		}
	}
	if !found {
		t.Fatal("ブックからテーマへの参照がない")
	}
}
