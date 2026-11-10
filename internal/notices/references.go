package notices

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

func slugEdge(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
func slugBody(b byte) bool { return slugEdge(b) || b == '.' || b == '-' }
func slugEnd(s string, start int) int {
	if start >= len(s) || !slugEdge(s[start]) {
		return start
	}
	end := start + 1
	for end < len(s) && end-start < 255 && slugBody(s[end]) {
		end++
	}
	for end > start+1 && !slugEdge(s[end-1]) {
		end--
	}
	return end
}
func projectStart(s string, marker, minimum int) int {
	start := marker
	for start > minimum && (slugBody(s[start-1]) || s[start-1] == '/') {
		start--
	}
	parts := strings.Split(s[start:marker], "/")
	if len(parts) < 2 || len(parts) > 21 {
		return marker
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 255 || !slugEdge(part[0]) || !slugEdge(part[len(part)-1]) {
			return marker
		}
	}
	return start
}
func nameEnd(s string, start int) int {
	if start >= len(s) {
		return start
	}
	if s[start] == '"' {
		count := 0
		for i, r := range s[start+1:] {
			if r == '\n' {
				break
			}
			if r == '"' {
				if count > 0 && count <= 255 {
					return start + 1 + i + 1
				}
				return start
			}
			count++
			if count > 255 {
				break
			}
		}
		return start
	}
	r, size := utf8.DecodeRuneInString(s[start:])
	if r != '_' && !unicode.IsLetter(r) {
		return start
	}
	end := start + size
	last := end
	count := 1
	for end < len(s) && count < 255 {
		r, size = utf8.DecodeRuneInString(s[end:])
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !strings.ContainsRune("_.-", r) {
			break
		}
		end += size
		count++
		if r != '.' && r != '-' {
			last = end
		}
	}
	return last
}

// 印を見つけてから名前を読む。長い平文や a/a/… の各位置で名前全体を再検索しない。
// 回数・長さ・Unicode の範囲は元の referencePattern と同じにする。
func referenceMatches(ctx context.Context, s string) ([][2]int, error) {
	matches := [][2]int{}
	cursor := 0
	for scan := 0; scan < len(s); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		offset := strings.IndexAny(s[scan:], "#!&$@~%[")
		if offset < 0 {
			break
		}
		marker := scan + offset
		start, end := marker, marker
		ch := s[marker]
		switch ch {
		case '#', '!', '&', '$':
			i := marker + 1
			if i < len(s) && s[i] >= '1' && s[i] <= '9' {
				i++
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
				project := projectStart(s, marker, cursor)
				if ch != '$' || project < marker || i-marker-1 >= 3 {
					start, end = project, i
				}
			}
		case '~', '%':
			end = nameEnd(s, marker+1)
			if end > marker+1 {
				start = projectStart(s, marker, cursor)
			} else {
				end = marker
			}
		case '@':
			end = slugEnd(s, marker+1)
			if end == marker+1 {
				end = marker
			} else {
				for end < len(s) && s[end] == '/' {
					next := slugEnd(s, end+1)
					if next == end+1 {
						break
					}
					end = next
				}
			}
		case '[':
			for _, prefix := range []string{"[issue:", "[merge_request:", "[epic:", "[snippet:"} {
				if !strings.HasPrefix(s[marker:], prefix) {
					continue
				}
				i := marker + len(prefix)
				if i < len(s) && s[i] >= '1' && s[i] <= '9' {
					i++
					for i < len(s) && s[i] >= '0' && s[i] <= '9' {
						i++
					}
					if i < len(s) && s[i] == ']' {
						end = i + 1
					}
				}
				break
			}
		}
		if end > marker {
			matches = append(matches, [2]int{start, end})
			cursor = end
			scan = end
		} else {
			scan = marker + 1
		}
	}
	return matches, nil
}
