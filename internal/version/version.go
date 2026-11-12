// Package version は mdpreview と mdtable で共通の、ビルド時に差し替えられる版を持つ。
package version

// Version は両コマンドの既定の版で、-ldflags "-X github.com/ryo-aoki-pc/md-tools/internal/version.Version=..." で変更する。
var Version = "0.14.0"
