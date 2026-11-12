package tableedit

import "strings"

const (
	extraCells        = "見出しより多いセルがある（GitLab では表示されない）"
	extraCellsEdit    = "。この行を編集すると消える"
	extraCellsDropped = "見出しより多いセルを消した（GitLab では表示されていなかった）"
)

// editWarnings は CLI の警告を変えずに、公開 API に合う文へ直して重複を除く。
// セルの編集と行の操作では、見出しより多いセルは消えない。
func editWarnings(warnings []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, warning := range warnings {
		warning = strings.TrimSuffix(warning, extraCellsEdit)
		if !seen[warning] {
			seen[warning] = true
			out = append(out, warning)
		}
	}
	return out
}

// droppedCells は列の操作で消える、見出しより多いセルの知らせを作る。
func droppedCells(warnings []string) []string {
	out := []string{}
	for _, warning := range warnings {
		if strings.HasSuffix(warning, extraCells+extraCellsEdit) {
			out = append(out, strings.TrimSuffix(warning, extraCells+extraCellsEdit)+extraCellsDropped)
		}
	}
	return out
}
