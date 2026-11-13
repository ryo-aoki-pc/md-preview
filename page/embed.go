// Package page はブラウザへ配るページとライブラリを埋め込む。
package page

import "embed"

// Files はページと lib/ の全資産を、page/ からの相対名で公開する。
// Go のソースは埋め込まない。
//
//go:embed index.html preview.js copy-code.js edit-grid.js edit-targets.js edit.js review.js preview.css fallback.css all:lib
var Files embed.FS
