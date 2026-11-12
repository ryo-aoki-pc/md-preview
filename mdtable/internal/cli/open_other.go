//go:build !windows

package cli

import "fmt"

func openWorkbook(path string) error {
	return fmt.Errorf("edit は Windows だけで使えます（export と import を使ってください）")
}
