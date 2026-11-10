package engine

import "embed"

// LicenseFiles は .wasm に入る crate と gem の、元の場所のライセンス原文。
//
//go:embed all:licenses
var LicenseFiles embed.FS
