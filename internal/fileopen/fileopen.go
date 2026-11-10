// Package fileopen は、利用者が編集しているファイルを、編集の邪魔をせずに読む。
//
// Windows の os.Open は、開いている間、ほかのプロセスに名前の変更と削除を許さない。エディターや
// 整形の道具が保存のときにファイルを入れ替えると、読んでいる間だけ失敗する。ここでは許して開く。
// ほかの OS では os.Open と同じ。
package fileopen

import (
	"io"
	"os"
)

// Open は、通常のファイルを読み取り専用で開く。
func Open(path string) (*os.File, error) { return open(path) }

// ReadFile は、ファイルの全体を読む。
func ReadFile(path string) ([]byte, error) {
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
