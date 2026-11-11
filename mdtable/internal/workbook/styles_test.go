package workbook

import (
	"encoding/xml"
	"testing"
)

func TestStyleIgnoresApplyFlags(t *testing.T) {
	text := `<styleSheet><fonts><font><b/><i val="0"/><strike val="1"/><u/></font></fonts><fills><fill><patternFill patternType="solid"/></fill></fills><numFmts><numFmt numFmtId="164" formatCode="0.00"/></numFmts><cellXfs><xf fontId="0" fillId="0" numFmtId="164" applyFont="0" applyFill="0" applyAlignment="0"><alignment horizontal="right"/></xf></cellXfs></styleSheet>`
	var styles rawStyles
	if e := xml.Unmarshal([]byte(text), &styles); e != nil {
		t.Fatal(e)
	}
	s, e := styles.style(0)
	if e != nil {
		t.Fatal(e)
	}
	if !isHeader(s) || s.Font.Italic || !s.Font.Strike || s.Font.Underline != "single" || horizontal(s) != "right" || s.CustomNumFmt == nil || *s.CustomNumFmt != "0.00" {
		t.Fatalf("書式が異なる %#v", s)
	}
}
