// Package version はビルド時に差し替えられるアプリの版を持つ。
package version

// Version は -ldflags "-X github.com/ryo-aoki-pc/md-preview/internal/version.Version=..." で変更する。
var Version = "0.2.0-dev"
