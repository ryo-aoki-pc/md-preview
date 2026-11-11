//go:build !race

package tableedit_test

// raceEnabled は -race で試験しているときに真。並行の処理を含まない重い往復の試験を間引くのに使う。
const raceEnabled = false
