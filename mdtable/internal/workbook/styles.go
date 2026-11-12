package workbook

import "github.com/xuri/excelize/v2"

type fontFlag struct {
	Value string `xml:"val,attr"`
}

func (f *fontFlag) enabled() bool {
	return f != nil && f.Value != "0" && f.Value != "false" && f.Value != "f"
}

type rawStyles struct {
	Fonts []struct {
		Bold      *fontFlag `xml:"b"`
		Italic    *fontFlag `xml:"i"`
		Strike    *fontFlag `xml:"strike"`
		Underline *fontFlag `xml:"u"`
	} `xml:"fonts>font"`
	Fills []struct {
		Pattern struct {
			Type string `xml:"patternType,attr"`
		} `xml:"patternFill"`
	} `xml:"fills>fill"`
	Formats []struct {
		ID   int    `xml:"numFmtId,attr"`
		Code string `xml:"formatCode,attr"`
	} `xml:"numFmts>numFmt"`
	Xfs []struct {
		Font      int `xml:"fontId,attr"`
		Fill      int `xml:"fillId,attr"`
		Format    int `xml:"numFmtId,attr"`
		Alignment *struct {
			Horizontal string `xml:"horizontal,attr"`
		} `xml:"alignment"`
	} `xml:"cellXfs>xf"`
}

// openpyxl は applyFont などの属性によらず、セルの書式番号の内容を読む。
func (s *rawStyles) style(id int) (*excelize.Style, error) {
	result := &excelize.Style{}
	if len(s.Xfs) == 0 && id == 0 {
		return result, nil
	}
	if id < 0 || id >= len(s.Xfs) {
		return nil, failure("ブックのセルの書式番号が壊れています: %d", id)
	}
	xf := s.Xfs[id]
	if xf.Font >= 0 && xf.Font < len(s.Fonts) {
		font := s.Fonts[xf.Font]
		result.Font = &excelize.Font{Bold: font.Bold.enabled(), Italic: font.Italic.enabled(), Strike: font.Strike.enabled()}
		if font.Underline != nil {
			result.Font.Underline = font.Underline.Value
			if result.Font.Underline == "" {
				result.Font.Underline = "single"
			}
		}
	}
	if xf.Fill >= 0 && xf.Fill < len(s.Fills) && s.Fills[xf.Fill].Pattern.Type == "solid" {
		result.Fill = excelize.Fill{Type: "pattern", Pattern: 1}
	}
	if xf.Alignment != nil {
		result.Alignment = &excelize.Alignment{Horizontal: xf.Alignment.Horizontal}
	}
	result.NumFmt = xf.Format
	for _, f := range s.Formats {
		if f.ID == xf.Format {
			code := f.Code
			result.CustomNumFmt = &code
			break
		}
	}
	return result, nil
}
