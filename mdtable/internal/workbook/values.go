package workbook

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ryo-aoki-pc/md-table-excel/internal/pycompat"
)

// 組み込み番号も openpyxl の BUILTIN_FORMATS と同じ文字列で判定する。
var builtinFormats = map[int]string{
	0:  "General",
	1:  "0",
	2:  "0.00",
	3:  "#,##0",
	4:  "#,##0.00",
	5:  "\"$\"#,##0_);(\"$\"#,##0)",
	6:  "\"$\"#,##0_);[Red](\"$\"#,##0)",
	7:  "\"$\"#,##0.00_);(\"$\"#,##0.00)",
	8:  "\"$\"#,##0.00_);[Red](\"$\"#,##0.00)",
	9:  "0%",
	10: "0.00%",
	11: "0.00E+00",
	12: "# ?/?",
	13: "# ??/??",
	14: "mm-dd-yy",
	15: "d-mmm-yy",
	16: "d-mmm",
	17: "mmm-yy",
	18: "h:mm AM/PM",
	19: "h:mm:ss AM/PM",
	20: "h:mm",
	21: "h:mm:ss",
	22: "m/d/yy h:mm",
	37: "#,##0_);(#,##0)",
	38: "#,##0_);[Red](#,##0)",
	39: "#,##0.00_);(#,##0.00)",
	40: "#,##0.00_);[Red](#,##0.00)",
	41: "_(* #,##0_);_(* \\(#,##0\\);_(* \"-\"_);_(@_)",
	42: "_(\"$\"* #,##0_);_(\"$\"* \\(#,##0\\);_(\"$\"* \"-\"_);_(@_)",
	43: "_(* #,##0.00_);_(* \\(#,##0.00\\);_(* \"-\"??_);_(@_)",
	44: "_(\"$\"* #,##0.00_)_(\"$\"* \\(#,##0.00\\)_(\"$\"* \"-\"??_)_(@_)",
	45: "mm:ss",
	46: "[h]:mm:ss",
	47: "mmss.0",
	48: "##0.0E+0",
	49: "@",
}

var stripFormats = regexp.MustCompile(`".*?"|\[[^\]]*\]`)
var durationFormat = regexp.MustCompile(`(?i)\[(?:h{1,2}|m{1,2}|s{1,2})\]`)
var retainedFormat = regexp.MustCompile(`^\[(?:h{1,2}|m{1,2}|s{1,2})\]$`)

func isDateFormat(format string) bool {
	format = strings.Split(format, ";")[0]
	format = stripFormats.ReplaceAllStringFunc(format, func(s string) string {
		if retainedFormat.MatchString(s) {
			return s
		}
		return ""
	})
	for i, ch := range format {
		if strings.ContainsRune("dmhysDMHYS", ch) && (i == 0 || format[i-1] != '_' && format[i-1] != '\\') {
			return true
		}
	}
	return false
}

func formatNumber(raw string) string {
	if !strings.ContainsAny(raw, ".Ee") {
		if n, ok := new(big.Int).SetString(raw, 10); ok {
			return n.String()
		}
		return raw
	}
	f, e := strconv.ParseFloat(raw, 64)
	if e != nil && !math.IsInf(f, 0) {
		return raw
	}
	if math.Trunc(f) == f && math.Abs(f) < 1e15 {
		if f == 0 {
			return "0"
		}
		return strconv.FormatFloat(f, 'f', 0, 64)
	}
	return pycompat.FloatRepr(f)
}

func formatTime(t time.Time, fraction bool) string {
	s := t.Format("15:04:05")
	if fraction && t.Nanosecond() != 0 {
		s += fmt.Sprintf(".%06d", t.Nanosecond()/1000)
	}
	return s
}

// Excel の日数をミリ秒へ銀行家の丸めで変換する。
func excelDate(value float64, date1904, duration bool) (string, error) {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return "", fmt.Errorf("日付の値が範囲外です")
	}
	if duration {
		// timedelta(days=...) はまずマイクロ秒、次にミリ秒へ丸める。
		day := math.Floor(value)
		micros := math.RoundToEven((value - day) * 86400 * 1e6)
		ms := math.RoundToEven(micros / 1000)
		day += math.Floor(ms / 86400000)
		ms = math.Mod(ms, 86400000)
		if day < -999999999 || day > 999999999 {
			return "", fmt.Errorf("日付の値が範囲外です")
		}
		seconds := int64(ms) / 1000
		micro := int64(ms) % 1000 * 1000
		text := fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
		if micro != 0 {
			text += fmt.Sprintf(".%06d", micro)
		}
		if day != 0 {
			unit := "days"
			if day == 1 || day == -1 {
				unit = "day"
			}
			text = fmt.Sprintf("%.0f %s, %s", day, unit, text)
		}
		return text, nil
	}
	day := math.Floor(value)
	ms := math.RoundToEven((value - day) * 86400 * 1000)
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	if date1904 {
		base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if value >= 0 && value < 1 && ms < 86400000 {
		return formatTime(base.Add(time.Duration(ms)*time.Millisecond), true), nil
	}
	if value > 0 && value < 60 && !date1904 {
		day++
	}
	if day < -1000000 || day > 4000000 {
		return "", fmt.Errorf("日付の値が範囲外です")
	}
	t := base.AddDate(0, 0, int(day)).Add(time.Duration(ms) * time.Millisecond)
	if t.Year() < 1 || t.Year() > 9999 {
		return "", fmt.Errorf("日付の値が範囲外です")
	}
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return t.Format("2006-01-02"), nil
	}
	return t.Format("2006-01-02 15:04:05"), nil
}

func isoDate(value string) string {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02", "15:04:05.999999999"} {
		if t, e := time.Parse(layout, value); e == nil {
			if !strings.Contains(value, "-") {
				return formatTime(t, true)
			}
			if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
				return t.Format("2006-01-02")
			}
			return t.Format("2006-01-02 15:04:05")
		}
	}
	return value
}
