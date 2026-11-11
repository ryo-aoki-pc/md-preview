//go:build !windows

package workbook

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
)

func fontInstalled(family string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, _ := exec.CommandContext(ctx, "fc-list", ":", "family").Output()
	for _, line := range strings.Split(string(output), "\n") {
		for _, name := range strings.Split(line, ",") {
			if pycompat.Lower(pycompat.Strip(name)) == pycompat.Lower(family) {
				return true
			}
		}
	}
	return false
}
