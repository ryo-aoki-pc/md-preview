// Package plantumlsource は、図の識別子と標準入力を共通の規則で作る。
package plantumlsource

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func ID(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}
func Input(source string) string {
	terminated := source
	if !strings.HasSuffix(source, "\n") && !strings.HasSuffix(source, "\r") {
		terminated += "\n"
	}
	if strings.HasPrefix(source, "@start") {
		return terminated
	}
	return "@startuml\n" + terminated + "@enduml\n"
}
