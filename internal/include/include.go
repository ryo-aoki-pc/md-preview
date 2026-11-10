// Package include は、読み込みを呼び出し元に任せて include を一度だけ展開する。
package include

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ryo-aoki-pc/md-preview/internal/paths"
	"github.com/ryo-aoki-pc/md-preview/internal/sanitize"
)

const MaxIncludes = 32
const MaxExpandedBytes = 16 * 1024 * 1024

type Kind int

const (
	Text Kind = iota
	Missing
	NotReadable
	TooLarge
	Invalid
)

type Read struct {
	Kind Kind
	Text string
}
type Item struct {
	Line  int    `json:"line"`
	Path  string `json:"path"`
	Lines int    `json:"lines"`
	OK    bool   `json:"ok"`
}
type span struct{ index, newlines int }
type Expansion struct {
	Text     string
	Includes []Item
	spans    []span
}

func (e Expansion) OriginalLine(line int) int {
	if line < 1 {
		return 0
	}
	shift := 0
	for _, s := range e.spans {
		start := e.Includes[s.index].Line + shift
		if line < start {
			break
		}
		if line <= start+s.newlines {
			return e.Includes[s.index].Line
		}
		shift += s.newlines
	}
	return line - shift
}
func (e Expansion) IsIncludedLine(line int) bool {
	shift := 0
	for _, s := range e.spans {
		start := e.Includes[s.index].Line + shift
		if line < start {
			return false
		}
		if line <= start+s.newlines {
			return e.Includes[s.index].OK
		}
		shift += s.newlines
	}
	return false
}
func EscapeInline(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 32 {
			fmt.Fprintf(&b, "&#%d;", r)
		} else {
			if strings.ContainsRune("*_[]#<>\\~!&-+=.()\"`", r) {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
func EscapeLinkDestination(s string) string {
	return "<" + strings.NewReplacer("<", "\\<", ">", "\\>", "\n", "%0A", "\r", "%0D").Replace(s) + ">"
}
func markdownLink(s string) string {
	return "[" + EscapeInline(s) + "](" + EscapeLinkDestination(s) + ")"
}
func errorMessage(s, reason string) string {
	return "**Error including '" + markdownLink(s) + "': " + reason + "**"
}

var httpPattern = regexp.MustCompile(`(?i)^https?://`)
var badPercent = regexp.MustCompile(`%([^a-fA-F0-9]|[a-fA-F0-9]([^a-fA-F0-9]|$)|$)`)

func IsHTTP(s string) bool {
	if !httpPattern.MatchString(s) || badPercent.MatchString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return sanitize.PermitURL(s)
}
func boundary(s string) bool {
	if s == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s)
	return strings.ContainsRune("\n\v\f\r\u0085\u2028\u2029", r)
}
func call(reader func(string) Read, p string) (r Read) {
	r.Kind = NotReadable
	defer func() {
		if recover() != nil {
			r = Read{Kind: NotReadable}
		}
	}()
	return reader(p)
}
func Expand(ctx context.Context, source, file string, reader func(string) Read) (Expansion, error) {
	e := Expansion{Includes: []Item{}}
	base := paths.IncludeBase(file)
	cache := map[string]Read{}
	cursor, sourceLine, size, count := 0, 1, len(source), 0
	var out strings.Builder
	for start := 0; start <= len(source); {
		if err := ctx.Err(); err != nil {
			return Expansion{}, err
		}
		end := strings.IndexByte(source[start:], '\n')
		if end < 0 {
			end = len(source)
		} else {
			end += start
		}
		if strings.HasPrefix(source[start:], "::include{file=") {
			nameStart := start + 15
			limit := end
			if limit > len(source) {
				limit = len(source)
			}
			close := -1
			for i := limit - 1; i >= nameStart; i-- {
				if source[i] == '}' && boundary(source[i+1:]) {
					if utf8.RuneCountInString(source[nameStart:i]) <= 1024 && i > nameStart {
						close = i
					}
					break
				}
			}
			if close >= 0 {
				filename := source[nameStart:close]
				raw := source[start : close+1]
				sourceLine += strings.Count(source[cursor:start], "\n")
				remote := IsHTTP(filename)
				resolved := paths.Resolve(filename, base)
				if remote {
					resolved = paths.Resolved{Path: filename, Safe: false}
				}
				item := Item{Line: sourceLine, Path: resolved.Path}
				replacement := raw
				if reader != nil && count < MaxIncludes {
					count++
					if remote {
						replacement = markdownLink(filename)
					} else {
						r := Read{Kind: Missing}
						if resolved.Safe {
							v, ok := cache[filename]
							if !ok {
								v = call(reader, resolved.Path)
								cache[filename] = v
							}
							r = v
						}
						if r.Kind == Text {
							if size-len(raw)+len(r.Text) > MaxExpandedBytes {
								replacement = errorMessage(filename, "too large")
							} else {
								replacement = r.Text
								item.OK = true
								item.Lines = strings.Count(r.Text, "\n")
								if r.Text != "" && !strings.HasSuffix(r.Text, "\n") {
									item.Lines++
								}
							}
						} else {
							reason := "not readable"
							if r.Kind == Missing {
								reason = "not found"
							} else if r.Kind == TooLarge {
								reason = "too large"
							}
							replacement = errorMessage(filename, reason)
						}
					}
				}
				out.WriteString(source[cursor:start])
				out.WriteString(replacement)
				size += len(replacement) - len(raw)
				added := strings.Count(replacement, "\n")
				e.Includes = append(e.Includes, item)
				if item.OK || added != 0 {
					e.spans = append(e.spans, span{len(e.Includes) - 1, added})
				}
				cursor = close + 1
			}
		}
		if end == len(source) {
			break
		}
		start = end + 1
	}
	out.WriteString(source[cursor:])
	e.Text = out.String()
	return e, nil
}
