package plantumlsource

import (
	"strings"
	"testing"
)

func TestSource(t *testing.T) {
	if ID("a") != "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb" {
		t.Fatal(ID("a"))
	}
	for _, c := range [][2]string{{"a -> b", "@startuml\na -> b\n@enduml\n"}, {"@startuml\na -> b\n@enduml", "@startuml\na -> b\n@enduml\n"}, {"\n@startuml\na -> b\n@enduml\n", "@startuml\n\n@startuml\na -> b\n@enduml\n@enduml\n"}, {"a\r", "@startuml\na\r@enduml\n"}} {
		if got := Input(c[0]); got != c[1] {
			t.Fatal(c, got)
		}
	}
	if len(ID(strings.Repeat("😀", 10))) != 64 {
		t.Fatal("UTF-8 の識別子")
	}
}
