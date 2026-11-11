package cli

// Python 版のコマンドと同じ案内を表示する。
var commandHelp = map[string]string{
	"": `usage: mdtable [-h] [--version] コマンド ...

GLFM の Markdown にある表（パイプ表・HTML の表）を Excel で編集して書き戻す。

positional arguments:
  コマンド
    list      文書の表の一覧を出す
    export    表を Excel のブックに書き出す
    import    ブックの内容で Markdown の表を書き戻す
    convert   表の中身を保ったまま、表の種類を変える
    edit      Excel で開いて編集し、閉じたら書き戻す（Windows）

options:
  -h, --help  show this help message and exit
  --version   show program's version number and exit
`,
	"list": `usage: mdtable list [-h] [--json] markdown

positional arguments:
  markdown    Markdown のファイル

options:
  -h, --help  show this help message and exit
  --json      表の一覧を JSON で出す
`,
	"export": `usage: mdtable export [-h] [-o OUTPUT] [--table 番号|L行|見出し] [--force] markdown

positional arguments:
  markdown             Markdown のファイル

options:
  -h, --help           show this help message and exit
  -o, --output OUTPUT  書き出すブック（既定は <名前>.tables.xlsx）
  --table 番号|L行|見出し    書き出す表（番号・L行番号・直前の見出しの文字。複数指定できる）
  --force              既にあるブックを上書きする
`,
	"import": `usage: mdtable import [-h] [--md MD] [--table 番号] [--dry-run] [--force]
                      [--keep-kind] [--strict-text]
                      workbook

positional arguments:
  workbook       export で書き出したブック

options:
  -h, --help     show this help message and exit
  --md MD        書き戻す Markdown（既定はブックに記録したもの）
  --table 番号     取り込むブックの表の番号（複数指定できる）
  --dry-run      書き戻さずに差分を出す
  --force        衝突や、Excel が日付・数値に変えたセルがあっても書き戻す
  --keep-kind    パイプ表を HTML の表に変える必要があれば止める
  --strict-text  数値・真偽値・数式・エラー値のセルがあれば止める
`,
	"convert": `usage: mdtable convert [-h] --table 番号|L行|見出し --to {html,pipe} [--dry-run]
                       markdown

positional arguments:
  markdown           Markdown のファイル

options:
  -h, --help         show this help message and exit
  --table 番号|L行|見出し  変える表（番号・L行番号・直前の見出しの文字。複数指定できる）
  --to {html,pipe}   変える先の表の種類
  --dry-run          書き戻さずに差分を出す
`,
	"edit": `usage: mdtable edit [-h] [--table 番号|L行|見出し] [--dry-run] [--keep-kind]
                    [--strict-text]
                    markdown

positional arguments:
  markdown           Markdown のファイル

options:
  -h, --help         show this help message and exit
  --table 番号|L行|見出し  編集する表（番号・L行番号・直前の見出しの文字）
  --dry-run          書き戻さずに差分を出す
  --keep-kind        パイプ表を HTML の表に変える必要があれば止める
  --strict-text      数値・真偽値・数式・エラー値のセルがあれば止める
`,
}
