// Package sticky は md-table-sticky の原文を埋め込む。
package sticky

import _ "embed"

// Source はページへ配る、ブックマークレットと共通の原文。
//
//go:embed src/sticky-table.js
var Source string
