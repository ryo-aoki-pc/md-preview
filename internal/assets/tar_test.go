package assets

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 拡張ヘッダーの優先順位とサイズを、archive/tar の自動補完を通さずに確かめる。
func rawTarEntry(name string, body []byte, kind byte, headerSize int64, modify func([]byte)) []byte {
	header := make([]byte, 512)
	copy(header, name)
	copy(header[100:], "0000644\x00")
	copy(header[108:], "0000000\x00")
	copy(header[116:], "0000000\x00")
	copy(header[124:], fmt.Sprintf("%011o\x00", headerSize))
	copy(header[136:], "00000000000\x00")
	header[156] = kind
	copy(header[257:], "ustar\x00")
	copy(header[263:], "00")
	if modify != nil {
		modify(header)
	}
	for i := 148; i < 156; i++ {
		header[i] = ' '
	}
	sum := 0
	for _, b := range header {
		sum += int(b)
	}
	copy(header[148:], fmt.Sprintf("%06o\x00 ", sum))
	result := append(header, body...)
	return append(result, make([]byte, (512-len(body)%512)%512)...)
}
func rawPAX(fields [][2]string) []byte {
	var result []byte
	for _, field := range fields {
		entry := field[0] + "=" + field[1] + "\n"
		length := len(entry) + 2
		for {
			text := strconv.Itoa(length) + " " + entry
			if len(text) == length {
				result = append(result, text...)
				break
			}
			length = len(text)
		}
	}
	return result
}
func gzipRaw(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(append(raw, make([]byte, 1024)...)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestTarExtendedHeadersAndNumbers(t *testing.T) {
	for _, kind := range []string{"global", "local-over-global", "gnu-long", "base256", "prefix", "nul-kind"} {
		t.Run(kind, func(t *testing.T) {
			name := "assets/name.txt"
			body := []byte("日本語の原文\n")
			var raw []byte
			appendEntry := func(name string, data []byte, flag byte, size int64, modify func([]byte)) {
				raw = append(raw, rawTarEntry(name, data, flag, size, modify)...)
			}
			switch kind {
			case "global", "local-over-global":
				pax := rawPAX([][2]string{{"path", name}, {"size", strconv.Itoa(len(body))}})
				if kind == "local-over-global" {
					pax = rawPAX([][2]string{{"path", "wrong.txt"}, {"size", "0"}})
				}
				appendEntry("global", pax, 'g', int64(len(pax)), nil)
				if kind == "local-over-global" {
					pax = rawPAX([][2]string{{"path", name}, {"size", strconv.Itoa(len(body))}})
					appendEntry("local", pax, 'x', int64(len(pax)), nil)
				}
				appendEntry("placeholder", body, '0', 0, nil)
			case "gnu-long":
				name = "assets/" + strings.Repeat("nested/", 20) + "name.txt"
				long := append([]byte(name), 0)
				appendEntry("long", long, 'L', int64(len(long)), nil)
				appendEntry("placeholder", body, '0', int64(len(body)), nil)
			case "base256":
				appendEntry(name, body, '0', int64(len(body)), func(header []byte) { clear(header[124:136]); header[124] = 0x80; header[135] = byte(len(body)) })
			case "prefix":
				appendEntry("name.txt", body, '0', int64(len(body)), func(header []byte) { copy(header[345:], "assets") })
			case "nul-kind":
				appendEntry(name, body, 0, int64(len(body)), nil)
			}
			dir := t.TempDir()
			archive := filepath.Join(dir, "archive.gz")
			writeFile(t, archive, gzipRaw(t, raw))
			files := map[string]Record{"saved.txt": {Archive: name, SHA256: digest(body), Size: int64(len(body))}}
			if err := ExtractSelected(t.Context(), archive, files, filepath.Join(dir, "out")); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(readFile(t, filepath.Join(dir, "out", "saved.txt")), body) {
				t.Fatal("拡張ヘッダーの原文が一致しません")
			}
		})
	}
}

func TestTarInvalidHeaderAndMetadata(t *testing.T) {
	for _, kind := range []string{"checksum", "numeric", "pax", "pax-size", "pax-large", "base256-large", "selected-large"} {
		t.Run(kind, func(t *testing.T) {
			body := []byte("原文")
			name := "assets/name.txt"
			var raw []byte
			switch kind {
			case "checksum":
				raw = rawTarEntry(name, body, '0', int64(len(body)), nil)
				raw[10] ^= 1
			case "numeric":
				raw = rawTarEntry(name, body, '0', int64(len(body)), func(h []byte) { h[124] = '9' })
			case "pax":
				raw = rawTarEntry("pax", []byte("invalid"), 'x', 7, nil)
			case "pax-size":
				pax := rawPAX([][2]string{{"size", "-1"}})
				raw = rawTarEntry("pax", pax, 'x', int64(len(pax)), nil)
				raw = append(raw, rawTarEntry(name, body, '0', int64(len(body)), nil)...)
			case "pax-large":
				raw = rawTarEntry("pax", nil, 'x', 1024*1024+1, nil)
			case "base256-large":
				raw = rawTarEntry(name, nil, '0', 0, func(h []byte) {
					for i := 124; i < 136; i++ {
						h[i] = 255
					}
				})
			case "selected-large":
				raw = rawTarEntry(name, nil, '0', 32*1024*1024+1, nil)
			}
			dir := t.TempDir()
			archive := filepath.Join(dir, "archive.gz")
			writeFile(t, archive, gzipRaw(t, raw))
			files := map[string]Record{"saved.txt": {Archive: name, SHA256: digest(body), Size: int64(len(body))}}
			requireFailure(t, ExtractSelected(t.Context(), archive, files, filepath.Join(dir, "out")), "")
		})
	}
}
