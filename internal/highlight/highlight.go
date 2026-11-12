// Package highlight は、必要になった言語だけ Chroma で配色する。
package highlight

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	t "github.com/ryo-aoki-pc/md-tools/internal/htmltree"
	"golang.org/x/net/html"
)

var DiagramLanguages = strings.Fields("actdiag blockdiag bpmn bytefield c4plantuml d2 dbml ditaa erd excalidraw graphviz mermaid nomnoml nwdiag packetdiag pikchr plantuml rackdiag seqdiag svgbob symbolator umlet vega vegalite wavedrom structurizr diagramsnet wireviz")
var plain = func() map[string]bool {
	m := map[string]bool{"math": true, "suggestion": true}
	for _, v := range DiagramLanguages {
		m[v] = true
	}
	return m
}()

// highlight.js 11.12.0 の登録順・登録名・別名を固定する。
// Chroma 独自の言語を勝手に有効にしない。
var languageNames = func() map[string]string {
	m := map[string]string{}
	for _, group := range []string{
		"1c",
		"abnf",
		"accesslog",
		"actionscript as",
		"ada",
		"angelscript asc",
		"apache apacheconf",
		"applescript osascript",
		"arcade",
		"arduino ino",
		"armasm arm",
		"xml html xhtml rss atom xjb xsd xsl plist wsf svg",
		"asciidoc adoc",
		"aspectj",
		"autohotkey ahk",
		"autoit",
		"avrasm",
		"awk",
		"axapta x++",
		"bash sh zsh",
		"basic",
		"bnf",
		"brainfuck bf",
		"c h",
		"cal",
		"capnproto capnp",
		"ceylon",
		"clean icl dcl",
		"clojure clj edn",
		"clojure-repl",
		"cmake cmake.in",
		"coffeescript coffee cson iced",
		"coq",
		"cos cls",
		"cpp cc c++ h++ hpp hh hxx cxx",
		"crmsh crm pcmk",
		"crystal cr",
		"csharp cs c#",
		"csp",
		"css",
		"d",
		"markdown md mkdown mkd",
		"dart",
		"delphi dpr dfm pas pascal",
		"diff patch",
		"django jinja",
		"dns bind zone",
		"dockerfile docker",
		"dos bat batch cmd",
		"dsconfig",
		"dts",
		"dust dst",
		"ebnf",
		"elixir ex exs",
		"elm",
		"ruby rb gemspec podspec thor irb",
		"erb",
		"erlang-repl",
		"erlang erl",
		"excel xlsx xls",
		"fix",
		"flix",
		"fortran f90 f95",
		"freedesktop desktop systemd",
		"fsharp fs f#",
		"gams gms",
		"gauss gss",
		"gcode nc",
		"gherkin feature",
		"glsl",
		"gml",
		"go golang",
		"golo",
		"gradle",
		"graphql gql",
		"groovy",
		"haml",
		"handlebars hbs html.hbs html.handlebars htmlbars",
		"haskell hs",
		"haxe hx",
		"hsp",
		"http https",
		"hy hylang",
		"inform7 i7",
		"ini toml",
		"irpf90",
		"isbl",
		"java jsp",
		"javascript js jsx mjs cjs",
		"jboss-cli wildfly-cli",
		"json jsonc json5",
		"julia",
		"julia-repl jldoctest",
		"kotlin kt kts ktm ktx",
		"lasso ls lassoscript",
		"latex tex",
		"ldif",
		"leaf",
		"less",
		"lisp",
		"livecodeserver",
		"livescript ls",
		"llvm",
		"lsl",
		"lua pluto",
		"makefile mk mak make",
		"mathematica mma wl",
		"matlab",
		"maxima",
		"mel",
		"mercury m moo",
		"mipsasm mips",
		"mizar",
		"perl pl pm",
		"mojolicious",
		"monkey",
		"moonscript moon",
		"n1ql",
		"nestedtext nt",
		"nginx nginxconf",
		"nim",
		"nix nixos",
		"node-repl",
		"nsis",
		"objectivec mm objc obj-c obj-c++ objective-c++",
		"ocaml ml",
		"openscad scad",
		"oxygene",
		"parser3",
		"pf pf.conf",
		"pgsql postgres postgresql",
		"php",
		"php-template",
		"plaintext text txt",
		"pony",
		"powershell pwsh ps ps1",
		"processing pde",
		"profile",
		"prolog",
		"properties",
		"protobuf proto",
		"puppet pp",
		"purebasic pb pbi",
		"python py gyp ipython",
		"python-repl pycon",
		"q k kdb",
		"qml qt",
		"r",
		"reasonml re",
		"rib",
		"roboconf graph instances",
		"routeros mikrotik",
		"rsl",
		"ruleslanguage",
		"rust rs",
		"sas",
		"scala",
		"scheme scm",
		"scilab sci",
		"scss",
		"shell console shellsession",
		"smali",
		"smalltalk st",
		"sml ml",
		"sqf",
		"sql",
		"stan stanfuncs",
		"stata do ado",
		"step21 p21 step stp",
		"stylus styl",
		"subunit",
		"swift",
		"taggerscript",
		"yaml yml",
		"tap",
		"tcl tk",
		"thrift",
		"tp",
		"twig craftcms",
		"typescript ts tsx mts cts",
		"vala",
		"vbnet vb",
		"vbscript vbs",
		"vbscript-html",
		"verilog v sv svh",
		"vhdl",
		"vim",
		"wasm",
		"wren",
		"x86asm",
		"xl tao",
		"xquery xpath xq xqm",
		"zephir zep",
	} {
		names := strings.Fields(group)
		for _, name := range names {
			m[name] = names[0]
		}
	}
	m["shell"] = "bash"
	return m
}()

type lexerEntry struct{ lexer chroma.Lexer }

var lexerCache sync.Map

func selectLanguage(requested string) (string, chroma.Lexer) {
	if plain[requested] {
		return requested, nil
	}
	lower := t.Lower(requested)
	canonical, ok := languageNames[lower]
	if !ok {
		return "plaintext", nil
	}
	tag := canonical
	if canonical == "bash" {
		tag = "shell"
	} else if canonical == "xml" && (requested == "html" || requested == "xhtml") {
		tag = "html"
	} else if canonical == "ini" && requested == "toml" {
		tag = "toml"
	}
	if tag == "plaintext" {
		return tag, nil
	}
	if cached, ok := lexerCache.Load(canonical); ok {
		return tag, cached.(lexerEntry).lexer
	}
	lookup := canonical
	switch canonical {
	case "bash":
		lookup = "bash"
	case "xml":
		lookup = "xml"
	case "ini":
		lookup = "ini"
	case "csharp":
		lookup = "c#"
	case "objectivec":
		lookup = "objective-c"
	case "pgsql":
		lookup = "postgresql"
	}
	entry := lexerEntry{lexers.Get(lookup)}
	actual, _ := lexerCache.LoadOrStore(canonical, entry)
	return tag, actual.(lexerEntry).lexer
}

type Token struct{ Scope, Value string }

func TokenLines(tokens []Token) [][]Token {
	lines := [][]Token{}
	line := []Token{}
	for _, token := range tokens {
		parts := strings.Split(token.Value, "\n")
		for _, part := range parts[:len(parts)-1] {
			if part != "" {
				line = append(line, Token{token.Scope, part})
			}
			lines = append(lines, line)
			line = []Token{}
		}
		if last := parts[len(parts)-1]; last != "" {
			line = append(line, Token{token.Scope, last})
		}
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}
	return lines
}
func visible(ctx context.Context, s string) ([]*html.Node, error) {
	out := []*html.Node{}
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, t.Text(b.String()))
			b.Reset()
		}
	}
	for i, r := range s {
		if i&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if r == '\u00a0' || r == '\u1680' || r >= '\u2000' && r <= '\u200a' || r == '\u202f' || r == '\u205f' || r == '\u3000' {
			r = ' '
		}
		if r == '\u061c' || r == '\u200e' || r == '\u200f' || r >= '\u202a' && r <= '\u202e' || r >= '\u2066' && r <= '\u2069' {
			flush()
			out = append(out, t.Element("span", t.Attr("class", "unicode-bidi has-tooltip", "data-toggle", "tooltip", "title", "Potentially unwanted character detected: Unicode BiDi Control"), t.Text(string(r))))
		} else {
			b.WriteRune(r)
		}
	}
	flush()
	return out, nil
}
func tokenize(ctx context.Context, source string, lexer chroma.Lexer) (tokens []Token, err error) {
	defer func() {
		if recover() != nil {
			tokens = nil
			err = errLexing
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	iterator, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root", EnsureLF: false}, source)
	if err != nil {
		return nil, err
	}
	remaining := len(source)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token := iterator()
		if token == chroma.EOF {
			break
		}
		value := token.Value
		if len(value) > remaining {
			value = value[:remaining]
		}
		remaining -= len(value)
		if value != "" {
			scope := chroma.StandardTypes[token.Type]
			tokens = append(tokens, Token{scope, value})
		}
		if remaining == 0 {
			break
		}
	}
	return tokens, nil
}

var errLexing = &lexingError{}

type lexingError struct{}

func (*lexingError) Error() string { return "字句解析に失敗しました。" }

type Result struct {
	Language string
	Children []*html.Node
}

func Code(ctx context.Context, source, requested string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	language, lexer := selectLanguage(requested)
	tokens := []Token{{Value: source}}
	if lexer != nil {
		var err error
		tokens, err = tokenize(ctx, source, lexer)
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if err != nil {
			language = ""
			tokens = []Token{{Value: source}}
		}
	}
	result := Result{Language: language, Children: []*html.Node{}}
	for i, line := range TokenLines(tokens) {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if i > 0 {
			result.Children = append(result.Children, t.Text("\n"))
		}
		parts := []*html.Node{}
		for _, token := range line {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			value := token.Value
			if strings.HasSuffix(value, "\r\n") {
				value = value[:len(value)-2]
			} else if strings.HasSuffix(value, "\r") || strings.HasSuffix(value, "\n") {
				value = value[:len(value)-1]
			}
			if value == "" {
				continue
			}
			contents, err := visible(ctx, value)
			if err != nil {
				return Result{}, err
			}
			if token.Scope != "" {
				parts = append(parts, t.Element("span", t.Attr("class", token.Scope), contents...))
			} else {
				parts = append(parts, contents...)
			}
		}
		lang := language
		if lang == "" {
			lang = "plaintext"
		}
		result.Children = append(result.Children, t.Element("span", t.Attr("id", "LC"+strconv.Itoa(i+1), "class", "line", "data-lang", lang), parts...))
	}
	return result, nil
}
