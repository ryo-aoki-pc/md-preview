// Package assets は、公式資産の固定情報だけを埋め込む。公式資産そのものは含めない。
package assets

import _ "embed"

//go:embed assets.lock.json
var lock []byte

// Lock は、改行も含めた元の固定情報の写しを返す。
func Lock() []byte { return append([]byte(nil), lock...) }
