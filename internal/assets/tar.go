package assets

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func tarText(data []byte) string {
	if at := bytes.IndexByte(data, 0); at >= 0 {
		data = data[:at]
	}
	return string(data)
}

func tarNumber(data []byte) (int64, error) {
	if len(data) == 0 {
		return 0, errors.New("tarの数値が不正です。")
	}
	if data[0]&0x80 != 0 {
		value := int64(data[0] & 0x7f)
		for _, b := range data[1:] {
			if value > (maxSafeInteger-int64(b))/256 {
				return 0, errors.New("tarのサイズが大きすぎます。")
			}
			value = value*256 + int64(b)
		}
		return value, nil
	}
	text := strings.TrimSpace(tarText(data))
	if text == "" {
		return 0, nil
	}
	for _, c := range text {
		if c < '0' || c > '7' {
			return 0, errors.New("tarの数値が不正です。")
		}
	}
	value, err := strconv.ParseInt(text, 8, 64)
	if err != nil || value > maxSafeInteger {
		return 0, errors.New("tarのサイズが大きすぎます。")
	}
	return value, nil
}

func paxFields(data []byte) (map[string]string, error) {
	fields := make(map[string]string)
	for cursor := 0; cursor < len(data); {
		space := bytes.IndexByte(data[cursor:], ' ')
		if space < 0 {
			return nil, errors.New("PAXヘッダーが不正です。")
		}
		space += cursor
		length, err := strconv.ParseInt(string(data[cursor:space]), 10, 64)
		if err != nil || length <= int64(space-cursor+1) || length > int64(len(data)-cursor) {
			return nil, errors.New("PAXヘッダーが不正です。")
		}
		end := cursor + int(length)
		entry := string(data[space+1 : end-1])
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			return nil, errors.New("PAX属性が不正です。")
		}
		fields[key] = value
		cursor = end
	}
	return fields, nil
}

func tarRead(input io.Reader, size int64, save bool) ([]byte, error) {
	if !save {
		_, err := io.CopyN(io.Discard, input, size)
		if err != nil {
			return nil, errors.New("tarアーカイブが途中で終わっています。")
		}
		return nil, nil
	}
	data := make([]byte, int(size))
	if _, err := io.ReadFull(input, data); err != nil {
		return nil, errors.New("tarアーカイブが途中で終わっています。")
	}
	return data, nil
}

// ExtractSelected は、tar を一度だけ順に読み、固定した通常ファイル以外を展開しない。
// PAX の共通属性・個別属性と GNU の長い名前を、今の取得処理と同じ順で適用する。
func ExtractSelected(ctx context.Context, archive string, files map[string]Record, directory string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(contextReader{ctx, f})
	if err != nil {
		return errors.New("gzipアーカイブが不正です。")
	}
	defer gz.Close()
	input := &expansionReader{reader: contextReader{ctx, gz}}
	selected := make(map[string]string)
	for name, r := range files {
		if r.Archive != "" {
			selected[r.Archive] = name
		}
	}
	found := make(map[string]bool)
	global, attributes := make(map[string]string), make(map[string]string)
	longName := ""
	for {
		header, err := tarRead(input, 512, true)
		if err != nil {
			return err
		}
		if bytes.Equal(header, make([]byte, 512)) {
			break
		}
		var sum int64
		for i, b := range header {
			if i >= 148 && i < 156 {
				sum += 32
			} else {
				sum += int64(b)
			}
		}
		want, err := tarNumber(header[148:156])
		if err != nil {
			return err
		}
		if sum != want {
			return errors.New("tarヘッダーの検査値が一致しません。")
		}
		name := tarText(header[:100])
		if prefix := tarText(header[345:500]); prefix != "" {
			name = prefix + "/" + name
		}
		size, err := tarNumber(header[124:136])
		if err != nil {
			return err
		}
		kind := header[156]
		if kind == 'x' || kind == 'g' || kind == 'L' {
			if size > 1024*1024 {
				return errors.New("tarの拡張ヘッダーが大きすぎます。")
			}
			data, err := tarRead(input, size, true)
			if err != nil {
				return err
			}
			if kind == 'L' {
				longName = strings.TrimSuffix(tarText(data), "\n")
			} else {
				fields, err := paxFields(data)
				if err != nil {
					return err
				}
				if kind == 'g' {
					for key, value := range fields {
						global[key] = value
					}
				} else {
					attributes = fields
				}
			}
			if _, err := tarRead(input, (512-size%512)%512, false); err != nil {
				return err
			}
			continue
		}
		attribute := func(key string) (string, bool) {
			value, ok := attributes[key]
			if !ok {
				value, ok = global[key]
			}
			return value, ok
		}
		if value, _ := attribute("path"); value != "" {
			name = value
		} else if longName != "" {
			name = longName
		}
		if value, ok := attribute("size"); ok {
			size, err = strconv.ParseInt(value, 10, 64)
			if err != nil || size < 0 || size > maxSafeInteger {
				return errors.New("PAXサイズが不正です。")
			}
		}
		attributes = make(map[string]string)
		longName = ""
		if outputName, ok := selected[strings.TrimPrefix(name, "./")]; ok {
			r := files[outputName]
			if (kind != '0' && kind != 0) || found[outputName] {
				return errors.New("固定した資産が通常ファイルでないか重複しています。")
			}
			if size != r.Size || size > 32*1024*1024 {
				return errors.New("固定した資産のサイズが一致しません。")
			}
			data, err := tarRead(input, size, true)
			if err != nil {
				return err
			}
			if err := checkedBytes(data, r.SHA256, r.Size, outputName); err != nil {
				return err
			}
			if err := relativePath(outputName); err != nil {
				return err
			}
			target := filepath.Join(directory, filepath.FromSlash(outputName))
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			if err := writeNew(target, data); err != nil {
				return err
			}
			found[outputName] = true
		} else if _, err := tarRead(input, size, false); err != nil {
			return err
		}
		if _, err := tarRead(input, (512-size%512)%512, false); err != nil {
			return err
		}
	}
	// 最初のゼロのヘッダーの後も、gzip 末尾の CRC の検証まで読み切る。
	if _, err := io.Copy(io.Discard, input); err != nil {
		return errors.New("gzipアーカイブが破損しています。")
	}
	if len(found) != len(selected) {
		return errors.New("固定した資産がアーカイブにありません。")
	}
	return ctx.Err()
}
