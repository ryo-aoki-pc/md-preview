# 固定する範囲と表の表示の決め方

## Markdown の外で当てる理由

GitLab は、Markdown の HTML に書いた `position: sticky`、`class`、`<style>` を取り除く。残る `rowspan`・`colspan` だけでは、先頭行・先頭列を固定できない。

見る人のブラウザーで、GitLab が描いた表へブックマークレットを当てる。外部へ通信せず、外部のスクリプトも読み込まない。ページを書き換えるものは、1 つの `javascript:` URL に入る。

## GitLab の版による違い

GitLab の表の部品は、`div.gl-table-shadow-overlay > div[data-sticky-header] > table` の形で表を包む。内側の `div` が縦横のスクロールの範囲になる。現在の gitlab.com は、`thead` の 1 行目と `tbody` の 1 行目の `th` に `position: sticky; top: 0` を当てる。

19.5.0 は `thead` のある表だけを部品に置き換える。見出しは 1 行目だけで、セルの結合を引き継がず、本文も最初の `tbody` だけを使う。ブックマークレットは、失われたセルを戻せない。`thead` の無い表は置き換えずに残るので、結合した見出しは `tbody` の先頭の `th` だけの行でも書ける。

master の 2026-10-19 の変更後は、入れ子でない表をすべて置き換える。見出しの全行、複数の `tbody`、`tfoot`、`caption`、表の属性を残す。結合セルなどが無く並べ替えのできる表では、見出しの中にボタンが入る。固定は、ボタンの中身でなくセルに当てる。

master の 2026-10-21 の変更後は、さらに外側に `div.gl-my-5.gl-w-fit.gl-max-w-full` を置く。`.gl-table-shadow-overlay` には `gl-my-5` を付けない。`[data-sticky-header]` には `gl-max-h-[70vh] gl-w-fit gl-max-w-full gl-overflow-x-auto gl-overflow-y-auto print:gl-max-h-none` を付ける。

表の高さが画面の 70% を超えるときだけ、`.gl-table-shadow-overlay` の後ろに `div.gl-mt-2.gl-flex.gl-justify-center` を置き、その中へ `button[data-testid="markdown-table-height-toggle"]` を出す。文字は「Show full table」または「Collapse table」にする。押すと、ページの全部の表の `gl-max-h-[70vh]` を外す、または付け直す。選択は GitLab 自身が `localStorage` の `markdown_table_expanded` に保存し、`window` に `CustomEvent("gl-markdown-table-expanded", { detail: { isExpanded } })` を出す。この道具は、その保存先を読まない・書かない。

これらの違いは、提供された GitLab の `render_markdown_tables.js` と `markdown_table.vue` のソースで確かめた。試験の `mimic-master.js`・`mimic-19.5.js` は、その振る舞いを Vue を使わずにまねる。実物の画面での確かめは、非公開の検証記録に残す。

## 格子を組む

入力は `thead`・`tbody`・`tfoot` のまとまりと、その中の行・セル。DOM からは `cell.colSpan`・`cell.rowSpan` を読む。

各行の次の空いた列にセルを置く。列の結合は 1〜1000、行の結合は 0〜65534 に直す。不正な値は 1 にする。列の 0 も 1 にする。行の 0 は、そのまとまりの終わりまで伸ばす。まとまりを越える行の結合は、まとまりの終わりで切る。

行数・列数、セルが始まる行と列、結合した大きさ、`th` かどうか、まとまりの種類を残す。各マス目にはセルの番号を入れる。同じマス目にセルが重なるときは `overlap: true` にする。ブラウザーの並べ方を決められないので、その表は固定しない。

## 行の範囲 R

次のうち最も大きい数から始める。

- `thead` の行数
- 先頭から、その行で始まるセルがすべて `th` になっている行の数
- 1

R より前に始まり、R の境目を `rowspan` でまたぐセルがあれば、そのセルの終わりまで R を広げる。またがなくなるまで繰り返す。全行になったら R を 0 にし、行を固定しない。

## 列の範囲 C

R 以降の行を見て、列 0 から `th` でそろう列の数 H を求める。1 つのセルで全列を覆う区切りの行は、数える対象から外す。どの対象行でも列 0〜h−1 が `th` に覆われる、全列より小さい最大の h が H になる。

C は `max(1, H)` から始める。固定する行に、C の境目を `colspan` でまたぐセルがあれば、そのセルの終わりまで広げる。ただし全列になるときは広げない。またがなくなるまで繰り返す。C が全列になったら 0 にし、列を固定しない。

列が自動のときは、実際に固定する行数を決めてから H と C を計算する。行を手で変えたときも、その行数で行の見出しの列 H、見出しの結合による C の拡張、全列になったときの 0 を決め直す。自動の行が半分の上限で 0 になったときも、その実際の行数を使う。

各セルは、全体が R より前なら `top`、全体が C より前なら `left` を持つ。両方を持つものは角になる。`col < C < col + colspan` のセルは `cross` になる。固定する行のセルは `top` と `cross` を両方持つこともある。

## 表のそばのツールバーと手動の範囲

対象の表のそばには、`div[data-md-table-sticky-bar]` を 1 つ出す。ツールバー、検索の行、列のチェックボックスはスクロールの範囲の外へ置く。GitLab の部品なら `div.gl-table-shadow-overlay` の前に、無ければ `[data-sticky-header]` の前に置く。自分で包んだ表なら、その包みの前に置く。並びはツールバー、開いている検索の行、列のチェックボックス、表の枠とし、GitLab の Vue の包みの中へツールバー、検索の行、チェックボックスを入れない。表を縦横へスクロールしてもツールバーは流れない。マウスを載せていないときも見せる。

ツールバーには「固定」、行と列それぞれの「−」「+」と実際に当てている数、「自動」「セルを選んで固定」、区切り、「横スクロール」「高さを制限」「行と列を強調」「コンパクト」のチェックボックス、「すべて折り返す」「すべて折り返さない」「検索」「コピー」「全幅で表示」のボタンを並べる。「高さを制限」は GitLab の全体表示のときだけ出す。自動の向きは数の後ろに「（自動）」を付ける。全体表示で高さを制限していないときの行だけ、自動なら「（自動・全体表示中）」、手動なら「（全体表示中）」を付ける。

操作は `data-md-table-sticky-action` で区別する。「横スクロール」は `scroll-x`、「高さを制限」は `fit-height`、「セルを選んで固定」は `pick`、「行と列を強調」は `highlight`、「コンパクト」は `compact`、一括の折り返し、検索、コピー、全幅表示は `wrap-all`・`nowrap-all`・`search`・`copy`・`float` にする。チェックボックスは `label` の中に `input[type=checkbox]` と文字を置き、`aria-label`・`title` と `setStyles` で今の横スクロールと同じ作りにする。新しいチェックはすべて既定でオフにし、値は表の記録だけに持つ。保存しない。`changeCount(record, action)` から `write()` の中でその表を `refresh()` する。ボタンは `button[type=button]` にし、`aria-label` と `title` で何を変えるかを示す。進めない方向と、自動に戻す必要のないときの「自動」は `disabled` にする。全幅表示中は「全幅で表示」を「閉じる」に替え、`aria-label` と `title` を「全幅表示を閉じる（Esc）」にする。小さな文字と少ない上下の余白にし、色は GitLab の CSS 変数と代わりの値を使う。ツールバーにも CSSOM の `setProperty` と `important` で当てる。

表の記録に `override = { rows: null, cols: null }` を持つ。`null` の向きは、従来の自動の決め方を使う。数が入った向きは、その数を使い、半分の上限では外さない。自動の向きだけ、上限を適用する。「自動」を押すと両方を `null` に戻す。選んだ数はメモリー内だけに置き、`localStorage` などの GitLab の保存領域には書かない。ページの再読み込みや、解除してからの再実行では自動から始める。

使える境目の計算は、DOM に触れない純粋な関数に分ける。行の境目 k は `0 <= k < 行数` のうち、`cell.row < k < cell.row + cell.rowspan` のセルが無いものにする。0 はいつも使える。列の境目 k は `0 <= k < 列数` のうち、先に決めた固定する行の中に、`cell.col < k < cell.col + cell.colspan` のセルが無いものにする。本文の結合は列の境目を制限せず、従来どおり `cross` として中身を左に残す。

「+」は今の実際の数より大きい最も近い境目へ進める。「−」は小さい最も近い境目へ戻す。全行・全列になる境目へは進めず、0 では戻せない。表 A の行は、本文の `rowspan=6` も守り、2 の次は 8、2 から減らすと 0 になる。実際に固定する行数を決めてから、手で選んでいた列の境目を確かめる。行を変えて、その境目が使えなくなったときは、選んでいた数以下で最も近い使える境目へ列を戻す。操作のたびにその表だけをすぐ測り直す。行とセルの組が変わらなければ、罫線の記録は使い回す。

ツールバーは、記録のある対象のすべての表に出す。セルが重なる表、セルの無い表、先頭行にセルの無い表には、ツールバーと列のチェックボックスを出さない。表の形だけで R・C がともに 0 になる表では、「固定」のラベル、行と列の操作、「自動」「セルを選んで固定」を隠し、折り返し・横スクロール・高さを制限・行と列を強調・コンパクト・検索・コピー・全幅表示の操作を残す。半分の上限で実際の行・列がともに 0 になっても固定の操作は残し、「0（自動）」を表示する。「+」から手動の数を選べるようにする。実際の行・列がともに 0 になったときは、固定と罫線の付け直しを戻す。折り返しの値、ツールバー、列のチェックボックス、スクロールの包みは残し、列の位置も測る。GitLab 自身の先頭行の固定と、本文の `th` の固定を外す扱いは、行・列とも固定しないときと同じにする。

ツールバーの追加・削除は `write()` の中で行い、自分の `MutationObserver` に拾わせない。解除したとき、表が切り離されたとき、固定の対象から外れたときにツールバーを消す。GitLab の包みへ置き換わったときは、ツールバーもその包みの前へ移す。印刷前は `display: none` にし、印刷後は戻す。

## セルを選んで固定

「セルを選んで固定」は、Excel の「ウィンドウ枠の固定」と同じく、選んだセルより上の行と左の列を固定する。ボタンの `aria-label` と `title` は「クリックしたセルより上の行と左の列を固定する（Esc で取り消し）」にする。選ぶ状態は `record.picking` に持ち、ボタンの `aria-pressed` を `"true"` または `"false"` にする。選ぶ間は、表の `desired` に `cursor: crosshair` を足す。

一度に選べる表は 1 つにし、`state.picking` に持つ。別の表で選び始めると前の表をやめる。同じボタンの押し直し、Esc、表のセル以外の押下でもやめる。選ぶ間だけ、`window` に capture の `click` と `keydown` の受け手を付ける。受け手は `state.pickHandlers` などに持ち、選ぶのをやめたとき、`cleanup`、`state.stop` で外す。

押した対象から `closest("td, th")` を求め、そのセルの `closest("table")` が選んでいる表と同じときだけ、セルを選ぶ。`preventDefault()`・`stopPropagation()`・`stopImmediatePropagation()` を呼び、リンク、GitLab の並べ替え、説明の編集へ流さない。ツールバーの「セルを選んで固定」の押下は止めず、ボタンの受け手で選ぶのをやめる。それ以外の押下も止めない。Esc では `preventDefault()`・`stopPropagation()` を呼び、選ぶ状態だけをやめる。

境目は DOM に触れない `pickBoundaries(model, cellIndex)` で求める。行は `lowerBoundary(usableRowBoundaries(model), cell.row)`、列はその行を使う `lowerBoundary(usableColBoundaries(model, 行), cell.col)` にする。全行・全列になる境目は選ばない。結果を `record.override.rows`・`record.override.cols` に入れ、表を測り直してから選ぶ状態をやめる。以後は手動の数と同じ扱いにし、結合による丸めは行うが半分の上限は当てない。

## 列のチェックボックス

`div[data-md-table-sticky-columns]` をツールバーと検索の行の後ろ、表の枠のすぐ前に置く。中身は `display: flex` の 1 行とし、列ごとに `label` と `input[type=checkbox][data-md-table-sticky-column]` を置く。チェックありは折り返す、チェックなしは折り返さない。列の幅が 64px 以上なら入力の後ろに「折り返す」を出し、狭ければ入力だけにする。

列の名前は、`thead` の行数と、先頭から `th` だけで始まる行数の大きい方を見出しの行数として求める。固定のための結合による拡張は行わない。各列を覆う見出しの文字を上から「 / 」でつなぎ、空白をまとめ、40 文字を超えたら切って「…」を付ける。`aria-label` と `title` は「列 3（2025 年 / 1 月）を折り返す」の形にし、名前が無ければ「列 3 を折り返す」にする。

列の左端は、固定する位置と同じ `colOffsets` から求める。列 c の幅は次の列の左端との差、最後の列は表の右端との差とし、ラベルを `flex: 0 0 <幅>px; box-sizing: border-box` にする。外側は `overflow: hidden` にし、幅を `record.scroll` の見えている幅に合わせる。中身の左端を、横スクロール量 0 のときの表の左端へそろえる。スクロールの範囲の `scroll` を `passive` で受け、チェックボックスの列の `scrollLeft` を合わせる。受け手を記録に持ち、範囲が替わるときと解除するときに外す。

固定する列のラベルは背景を付けて `position: sticky` にする。`left` は、スクロールの範囲の内側の左端とチェックボックスの列の左端の差に、`colOffsets[c]` を足す。表の固定したセルと同じ画面上の位置へ残すため。閉じた `details` などで表の大きさが 0 の間はチェックボックスの列を隠し、測れたら出す。印刷前も隠す。

初めの状態は、この道具の style を外した状態の計算値から決める。`headerRowCount` で求めた見出しの行数以降に始まる、その列の `colspan` が 1 のセルを優先する。それらが 1 つ以上あり、すべて `white-space: nowrap` ならチェックなし、それ以外はチェックありにする。見出しより後に該当するセルが無い列だけ、すべての行からその列で始まる `colspan` が 1 のセルを集め、同じように決める。該当するセルが 1 つも無ければチェックありにする。GitLab が見出しを描き直すときに `nowrap` を落としても、本文の計算値から初めの状態を決められるようにする。列数が変わったら決め直す。操作で選んだ値は保存領域へ書かず、表の記録だけに持つ。

作業項目の説明では Vue が説明の根で `change` を受けるが、`task-list-item-checkbox` のクラスを持つ入力だけを説明の編集として扱う。この道具の列、ツールバー、検索の行の入力には、そのクラスを付けない。

## セルの折り返しと横スクロール

セルが覆う列をすべて「折り返さない」にしたときだけ、そのセルを `white-space: nowrap` にする。それ以外は折り返すセルとし、`white-space: normal` を当てる。結合セルも同じ決まりを使う。横スクロールは表の記録の `scrollX` に持ち、既定は `false` にする。

横スクロール「なし」では、折り返すセルへ `overflow-wrap: anywhere` を当て、表を枠の幅に収める。折り返さない列だけで枠の幅を超えた場合は、枠で横にスクロールする。横スクロール「あり」でも、折り返すセルへ `overflow-wrap: anywhere` を当て、長い URL などの語を途中で折り返す。さらに、折り返すセルのうち `colspan` が 1 のものへ、その列の最大の内容の幅と 12em の小さい方を `min-width` として当てる。12em は表の計算された `font-size` の 12 倍にする。セルの `box-sizing` が `content-box` なら、測った列幅から左右の padding と border を引いて `min-width` にする。

最大の内容の幅は、同じ同期の処理の間だけ表を `width: max-content` にし、枠の `max-width` も外して列の幅を測る。測り終わったら style とスクロール位置を元へ戻す。Chromium の表で `min-width` が列の幅へ効くことは、ブラウザーの試験で確かめる。

折り返しと最小の幅も、罫線・固定の値と同じ `desired` の Map にまとめ、`setProperty` の `important` で当てる。`originals` と `applied` を使って元の style へ戻す。処理は次の順にする。

1. スクロールの範囲を決める。
2. `ensureBorders`・`updateBase` で罫線と元の状態を読み、元の `white-space` も記録する。
3. 列ごとの初めの折り返しを決める。
4. 絞り込む行を決め、行の非表示、折り返し、コンパクト表示、固定に使う罫線を当てる。
5. 横スクロール「あり」なら最大の内容の幅を測り、`min-width` を当てる。
6. 行と列の位置を測り、固定を当てる。
7. 列のチェックボックスの位置と幅を合わせる。

折り返しとコンパクト表示によって行の高さと列の位置が変わるため、固定の位置より先に当てる。固定しない表でも、共通の測る処理を使って列のチェックボックスをそろえる。列・一括の折り返しと横スクロールを変える操作も `write()` の中でその表だけをすぐ `refresh()` する。

## 行・列の強調

「行と列を強調」の `aria-label` と `title` は「マウスを合わせたセルの行と列に色を付ける」にする。オンの表だけ `pointerover` と `pointerleave` を受け、`record.hover = { index, onOver, onLeave }` に記録する。対象のセルは `closest("td, th")` で求め、`closest("table")` が自分の表と同じものだけ使う。セルの番号が変わったときだけ更新し、表から出たら消す。オフ、`cleanup`、`state.stop` で受け手を外す。セルを選ぶ状態の間も強調してよい。

強調するセルは、DOM に触れない `crossCells(model, cellIndex)` で求める。載ったセルの行の範囲 `[row, row + rowspan)` か、列の範囲 `[col, col + colspan)` と交わるセルの番号を返す。載ったセル自身も含める。結合セルに載ったときも、結合セルが対象の行・列と交わるときも、この決まりを使う。

色は `background-image: linear-gradient(色, 色)` で重ねる。検索の一致と重なるときは、黄色と青の 2 枚の画像を重ねる。交わるセルは `rgba(31, 117, 254, 0.10)`、載ったセルは `rgba(31, 117, 254, 0.22)` を使う。固定セルの不透明な `background-color`、罫線、`box-shadow` は変えない。

マウスの移動では `refresh()` を呼ばない。`refresh()` の最後に、そのとき当てた Map を `record.desired` に残す。強調の更新では、変わったセルの値だけに強調を足す、または除き、`applyOne` で当てる。`refresh()` で `desired` を作るときも今の強調を含め、測り直しで消さない。重ね塗りと今の一致の枠を作る処理は 1 か所にまとめ、`addHighlights` と `updateHover` の両方から使う。マウスの移動でも検索の色と枠を残す。

## 表内検索

ツールバーの「検索」は `aria-pressed` を持つボタンとし、`aria-label` と `title` を「この表の中を検索する」にする。押すと `div[data-md-table-sticky-search]` をツールバーのすぐ後ろへ出し、検索の欄へフォーカスを置く。閉じている間は検索の行を置かない。検索の行は `display: flex; flex-wrap: wrap; align-items: center; gap` で並べ、ツールバーと同じ小さな文字と色を使う。

中身は次の順にする。操作は `data-md-table-sticky-action` で区別する。

| 要素 | action | 説明 |
| --- | --- | --- |
| `input[type=search][data-md-table-sticky-search-input]` | — | `aria-label` と `placeholder` は「表の中を検索」 |
| 「前」のボタン | `search-prev` | `aria-label` と `title` は「前の一致へ（Shift+Enter）」 |
| 「次」のボタン | `search-next` | `aria-label` と `title` は「次の一致へ（Enter）」 |
| `span[data-md-table-sticky-search-count]` | — | `aria-live="polite"` を付ける |
| 「一致した行だけ表示」のチェックボックス | `search-filter` | `aria-label` と `title` は「一致するセルのある行だけを表示する」 |
| 「閉じる」のボタン | `search-close` | `aria-label` と `title` は「検索を閉じる（Esc）」 |

状態は `record.search = { open, query, index, filter, composing }` に持つ。保存領域へ書かない。ツールバーの「検索」の押し直し、検索の「閉じる」、欄の Esc で閉じる。閉じると欄、一致、今の番号、「一致した行だけ表示」を空に戻し、重ね塗りと絞り込みを外し、ツールバーの「検索」へフォーカスを戻す。欄の Esc は `preventDefault()` と `stopPropagation()` を呼ぶ。セルを選ぶ状態の Esc は、先に選ぶ状態をやめる。

一致は DOM に触れない `searchMatches(texts, query)` で求める。セルの `textContent` と欄の文字を `normalize("NFKC")`、小文字、連続する空白を 1 つの空白、前後の空白を除いた形にそろえ、部分一致するセルの番号を行・列の順に返す。空の欄では空の配列を返す。件数は、空の欄では空、一致が無ければ「0 件」、あれば「M / N 件」にする。M は今の一致の番号、N は一致したセルの数にする。

一致したセルには `rgba(255, 196, 0, 0.35)` の黄色を `background-image` で重ねる。今の一致には、さらに `outline: 2px solid rgba(255, 140, 0, 0.9)` と `outline-offset: -2px` を当てる。マウスによる青の強調と同じ処理で重ね塗りを作り、`desired` へ入れる。

`input` では、`event.isComposing` または `compositionstart` から `compositionend` までの変換中は一致を取り直さない。変換が終わったとき、または変換中でない入力から 150 ms 待って、今の番号を 0 にし、その表だけ `write()` の中で `refresh()` する。

Enter と「次」で次へ、Shift+Enter と「前」で前へ進める。移動と絞り込みの操作では、150 ms の待機を取り消して欄の今の文字を取り込む。文字が変わったときだけ今の番号を 0 に戻す。変換中の文字は取り込まず、変換中の Enter は無視する。最後の次は最初、最初の前は最後へ回る。番号を変えて `refresh()` した後、今の一致のセルを `scrollIntoView({ block: "nearest", inline: "nearest" })` で見える位置へ移す。`record.layout.roles` が `top` か `left` のセルなら、枠の中の位置を変えず、`record.scroll.scrollIntoView({ block: "nearest", inline: "nearest" })` で枠だけをページの見える位置へ移す。それ以外はセルへ呼び、枠の `scroll-padding` で固定した部分の下へ隠れるのを防ぐ。

GitLab の並べ替えなどで行が動いたら、今の見張りによる `refresh()` で今の DOM の文字から取り直す。今の番号が一致の数を超えたら 0 に戻す。空の欄では重ね塗りも絞り込みもしない。

## 一致した行だけ表示

「一致した行だけ表示」にチェックがあり、欄が空でないときだけ行を隠す。隠す行の番号は DOM に触れない `filterRows(model, matched, keepRows)` で求め、昇順で返す。`matched` は一致したセルの番号にする。`keepRows` は `headerRowCount(model)` と、手動なら `record.override.rows`、自動なら `record.region.rows` の大きい方にする。半分の上限で実際には固定しない場合も、この先頭の範囲は残す。

先頭の `keepRows` 行、`thead` の行、`tfoot` の行は隠さない。残りの行は、あるセルの `[row, row + rowspan)` を同じまとまりとし、推移的につながる行もまとめる。まとまりの行で始まる一致したセルがあれば、そのまとまりを残し、無ければ隠す。`col === 0 && colspan === colCount && rowspan === 1` のセルだけの行は区切りとし、それ自身が一致するか、その後ろから次の区切りまでに残るまとまりがあれば残す。一致が 0 件なら、必ず残す行を除いて本文の行をすべて隠す。

隠す `tr` は `desired` で `display: none` にする。検索の文字や絞り込みを変えるたびにその表を `refresh()` し、行を隠した後の寸法から固定の位置を測る。`refresh()` の初めに使わなくなった要素を戻して忘れるときは、表、包み、枠、ツールバーの基準、今のセル、今の内側の span に加え、今の行 `parts.rows` も残す。`tr` の `originals` を失わないため。

`measureLayout` で列の左端を求めるときは、非表示の行の矩形を使わず、見えている行のセルから求める。見えているセルが無い列だけ補間する。行の上端は、隠さない固定する行の位置だけに使う。固定する高さは、次の行の上端ではなく、固定する最後の行の下端から求める。絞り込みを外すと `originals` の style へ戻す。

## 表をコピーする

ツールバーの「コピー」の `aria-label` と `title` は「表示中の行を、Excel に貼り付けられる形でコピーする」にする。押下の中で、今の DOM の順から見出しと見えている行をコピーする。GitLab の並べ替えの後は、その並びを使う。次の DOM に触れない関数で組み立てる。

- `tableMatrix(model, texts, hiddenRows)` は隠した行を除き、`{ rowCount, colCount, cells: [{ row, col, rowspan, colspan, header, text }], grid }` を返す。`grid` は行 × 列の文字の配列とし、結合セルの値は左上のマスだけに入れ、ほかは空文字にする。行はまとまりごと隠すので、残すセルの `rowspan` は変えない。
- `toTsv(matrix)` は列をタブ、行を CRLF でつなぎ、最後にも CRLF を付ける。タブ、改行、`"` を含む値だけ `"` で囲み、値の中の `"` は `""` にする。
- `toHtml(matrix)` は `<table>` の中に `<tr>` と `<th>` または `<td>` を置く。`rowspan` と `colspan` は 1 より大きいときだけ付ける。文字の `&`・`<`・`>`・`"` を逃がし、値の中の改行は `<br>` にする。

コピーする文字は DOM の層で作る。セルの子をたどり、文字ノードはそのまま、`br` は改行、`p`・`div`・`li` の境目は改行にする。各行の連続する空白を 1 つにし、前後の空白と、値の前後の空の行を除く。`svg` と `aria-hidden="true"` の中は使わず、並べ替えのアイコンなどをコピーしない。

最初に `navigator.clipboard.write([new ClipboardItem({ "text/html": Blob, "text/plain": Blob })])` で HTML と TSV をコピーする。使えないか失敗したら `navigator.clipboard.writeText(TSV)` を使う。それも使えないか失敗したら、`.md` の外の `document.body` 直下へ一時の `textarea` を置き、TSV を選んで `document.execCommand("copy")` を呼ぶ。一時の要素の追加と削除は `write()` の中で行う。

右下の知らせも `write()` の中で出す。HTML と TSV をコピーしたら「表をコピーした（N 行 × M 列）」、TSV だけなら「表を文字だけコピーした（N 行 × M 列）」、すべて失敗したら「表をコピーできなかった」にする。N は見出しを含むコピーした行数、M は列数にする。HTML の `rowspan`・`colspan` を付けてコピーする。

## コンパクト表示

「コンパクト」の `aria-label` と `title` は「セルの余白と文字を小さくして、多くの行と列を表示する」にする。オンの表のすべてのセルへ、`padding: 2px 6px`・`line-height: 1.3`・`font-size: 0.875em` を `desired` で当てる。

折り返しと同じ段階で、`measureLayout` より前に当てる。固定する位置はコンパクト表示にした後の寸法から求める。横スクロールの最小の幅の 12em は、今までどおり表の計算された `font-size` から求める。

## 対象とスクロールの範囲

対象は `.md` の中の表。`.code` の表、`data-table-fields` の付いた JSON の表、入れ子の内側の表、セルが重なる表、セルの無い表は除く。先頭の `<tr>` にセルが無く、`parts.model.grid[0][0] === undefined` になる表も、測る基準のセルが無いため対象から外し、ツールバーと列のチェックボックスを出さない。入れ子かどうかは、`table.parentElement.closest("table, .md")` が `.md` かで確かめる。

表の親が `[data-sticky-header]` なら、その包みを使う。無ければ、自分の `div[data-md-table-sticky-wrap]` で包む。自分の包みには `max-height: 70vh; overflow: auto; width: fit-content; max-width: 100%; margin: 16px 0` を当てる。表・GitLab のスクロールの範囲・外側の包みの幅は、元の指定を保つ。GitLab の包みは `width: fit-content; max-width: 100%`、表は内容に応じた幅にする。表には `display: table; margin: 0; overflow: visible` を当て、横スクロール「あり」では表が広がれるよう `max-width: none` にする。

自分の包みの中へ GitLab の包みが後から入ったときは、自分の包みを外す。二重にスクロールする範囲を作らないため。

## GitLab の全体表示への対応

GitLab が全体表示にすると、`[data-sticky-header]` は `overflow-y: auto` のまま縦にスクロールしなくなる。行の `position: sticky; top` は、その枠のスクロールへ効くため、ページを縦にスクロールしても行は止まらない。

`refresh()` で、スクロールの枠が GitLab の `[data-sticky-header]` であり、自分の `record.wrapper` ではなく、全幅表示中でも印刷中でもないときに全体表示を調べる。`updateBase` と同じく、枠の style 属性をこの道具を当てる前の文字列へ一時に戻して計算された `max-height` を読み、すぐ直前の style 属性へ戻す。値が `none` のときだけ `record.nativeFull = true` にする。全幅表示中と印刷中は判定を休み、`nativeFull` と `fitHeight` を保持する。通常の表示に戻ってから全体表示でなくなったことを調べ、`fitHeight` を解除する。

全体表示のときだけ、ツールバーの「高さを制限」の `display` を切り替えて出す。`aria-label` と `title` は「この表だけ高さを画面の 70% までにして、見出しの行を固定する（GitLab の「Show full table」の設定は変えない）」にする。チェックを入れると `record.fitHeight = true` にし、スクロールの枠の `desired` へ `max-height: 70vh` を足す。全幅表示中と印刷中には足さない。GitLab の「Collapse table」で全体表示でなくなったら `record.fitHeight = false` に戻し、チェックボックスを隠す。自分の包みは今までどおり 70vh とし、この操作を出さない。

全体表示で `fitHeight` がオフなら、行の数に自動では「（自動・全体表示中）」、手動では「（全体表示中）」を付ける。行の固定がページの縦スクロールには効かないことを示す。それ以外の自動の向きは「（自動）」にする。

`window` の `gl-markdown-table-expanded` を受け、すべての表を `requestRefresh()` する。Vue の class の更新後に測るため、今の `requestAnimationFrame` でまとめる仕組みを使う。枠の大きさの変化も今の `ResizeObserver` で拾う。イベントの受け手は `state.stop` で外す。GitLab の `localStorage` は読まない・書かない。

## 測り方と位置

測る間だけ、表自身のすべてのセルを `position: static` にする。GitLab やこの道具が固定した後の位置から測ると、スクロール量が混じるため。同じ同期の処理で元へ戻し、途中の見た目を画面へ出さない。

行 r の上端は、その行の `tr` の上端。固定に使う行は絞り込みでも隠さない。列 c の左端は、その列に始まる見えているセルの左端。隠した行のセルは使わない。最初の見えているセルの左上を基準にした差を使う。`caption` の高さや表の外枠を位置に足さない。固定する高さは、固定する最後の行の下端 `measurement.rowEnds[rows - 1]` から求める。次の行が絞り込みで隠れていても、その行の上端は使わない。

| 役 | 当てる値 |
| --- | --- |
| 角 | `position: sticky; top: 行の上端; left: 列の左端; z-index: 3` |
| top だけ | `position: sticky; top: 行の上端; z-index: 2` |
| left だけ | `position: sticky; left: 列の左端; top: auto; z-index: 1` |

位置の単位は px。`left` だけのセルには `top: auto` が要る。gitlab.com は `tbody > tr:first-child > th` も `top: 0` で固定するので、そのままでは行の見出しが上に貼り付く。

GitLab が元から `position: sticky` を当てているセルでも、`top` と `left` の役をどちらも持たないセルには `position: static` を当てる。`tbody` の 1 行目の途中に `th` がある場合、そのセルだけが上に貼り付くのを防ぐため。ただし、実際に当てる R が 0 のときは、行 0 のセルに GitLab 自身の固定を残す。半分の上限で行の固定を外すときも、この扱いにする。

自動のときは、固定する高さ（固定する最後の行 R−1 の下端）がスクロールの範囲の `clientHeight` の半分を超えたら、行を固定しない。幅（列 C の左端）が `clientWidth` の半分を超えたら、列を固定しない。本文を動かせる範囲を残すため。手で選んだ向きには、この上限を当てない。ツールバーには、上限で外した場合も含めて、実際に当てている数を出す。この場合も GitLab 自身の先頭行の固定は残す。

閉じた `details` などで大きさが 0 の表は、測らずに待つ。`ResizeObserver` で大きさが変わってから測る。

## Ctrl+F とスクロールの余白

検索結果やフォーカスの移動先を、固定した行・列の下へ隠さないため、スクロールの枠へ `scroll-padding` を指定する。ツールバーの操作は足さない。最後の `apply()` の後、行を固定するときは `scroll-padding-top: <fixedHeight>px`、列を固定するときは `scroll-padding-left: <fixedWidth>px` を、枠の style へ `setProperty` の `important` で直に足す。固定しない向きは `removeProperty` で外す。

固定の高さ・幅は位置を測った後に決まる。`desired` に入れると、測る前と後で枠の style の組が変わり、測り直すたびに枠の style を元の文字列へ戻して当て直すことになるため、`desired` には入れない。枠の style を元へ戻すときに消えても、同じ `refresh()` の最後で足し直す。解除では `originals` の文字列へ戻すので残らない。

最後の `apply()` では、自分の包みの `overflow` が一時に外れ、スクロール位置を失うことがある。その後にも `scroll.scrollLeft`・`scroll.scrollTop` を、測る前の `scrollPosition` へ戻す。固定しない分岐と同じ扱いにする。検索の `window.find()` が余白を見て、隠れた本文を見出しの下へ動かすことは、ブラウザーの試験で確かめる。

## 背景と罫線

書き換えはすべて `el.style.setProperty(名前, 値, "important")` で行う。GitLab のクラスの CSS に負けず、CSP が `<style>` を止めるページでも CSSOM で当てるため。

固定するセルの背景のアルファが 0 なら、`var(--gl-background-color-default, 祖先の背景の色)` を当てる。祖先の色は、スクロールの範囲から `html` まで上がり、最初の透明でない背景を使う。無ければ白にする。

`border-collapse: collapse` では、罫線がセルと一緒に固定されず、元の格子の位置へ描かれる。Edge で、固定したセルの端のすき間から本文が透けて見えることと、固定した 2 列の間の罫線が消えることを確かめた。固定する表は `border-collapse: separate; border-spacing: 0` に替える。ただ替えるだけでは罫線が二重になるので、元の罫線を読み、各セルへ付け直す。

罫線の計算は、格子と各セルの 4 辺を受け、各セルの 4 辺を返す純粋な関数に分ける。次の順で決める。

1. この道具の style を外した状態で、すべてのセルの計算された `border-*-width`・`border-*-style`・`border-*-color` を読む。元の `collapse` の状態で読む。
2. 各セルの右と下の線を決める。隣り合うセルの共有辺では、どちらかの style が `hidden` なら、その共有辺の線を消す。隣の線の太さより優先する。それ以外は、両側のうち太い線を使う。同じ太さなら、上か左のセルの線を使う。style が `none`、または幅が 0 の辺には線が無い。外周は、そのセルの辺を使う。結合セルの辺が複数のセルと接するときは、接する線のうち最も太い線を使う。ただし、1 つでも `hidden` の共有辺があれば、そのセルの右または下の辺全体を無しにする。セルごとに 1 本の線として当てるため。
3. 上は、直上の行でそのセルに接するマス目がすべて空いているときだけ、そのセルの上の辺を付ける。行 0 もこの扱いにする。左も、直左の列でそのセルに接するマス目がすべて空いているときだけ、そのセルの左の辺を付ける。列 0 もこの扱いにする。ほかの上と左は幅を 0 にする。一部だけ空いているときは、空いた部分の外周の線を省く。隣のセルの下や右の線と二重になるのを防ぐため。
4. 4 辺の幅・style・色を `important` で当てる。その後で位置を測る。`separate` へ替えると列の位置が端数だけ動くため。

罫線を付け直すのは、実際に固定する表だけ。セルが重なる表や、行・列とも固定しない表には、罫線の付け直しを当てない。測り直して行・列とも固定しなくなったときは、罫線への変更も戻す。境目の inset の影は足さない。固定したセルのうち、付け直した下の罫線があるものは、`box-shadow` を `none` にする。GitLab が先頭行の下へ付けた影の代わりに、その罫線を使う。付け直した下の罫線が無いもの（gitlab.com の CSS が、`thead` の 1 行目の `th` と `tbody` の 1 行目の `th` の両方の上下の罫線を 0 にし、影で線を描く境目）は、GitLab の影を残す。固定しないセルの影は変えない。

付け直した罫線の値は表の記録に残す。行とセルの要素の組・並びが変わったときだけ、元の style から読み直して作り直す。大きさだけの測り直しでは `collapse` に戻さない。毎回の読み直しの負荷と、見た目が切り替わるおそれを避けるため。解除すると、表とすべてのセルの style 属性を元の文字列へ戻す。

## 結合したセルの中身

次のセルは、子をすべて `span[data-md-table-sticky-inner]` へ移す。span に `display: inline-block; position: sticky` を当てる。セルの `text-align` はそのまま効かせる。

- `left` または `cross` を持ち、`top` を持たず、`rowspan > 1` のセル。span の `top` を、固定する行の高さ（固定する最後の行 R−1 の下端）にする。下へスクロールしても、縦に長い見出しの文字を固定した行の下に残す。
- `top` を持ち、`left` を持たず、`colspan > 1` のセル。span の `left` を、固定する列の幅（列 C の左端）にセルの左の padding を足した値にする。横へスクロールしても、横に長い見出しの文字を固定した列の右に残す。
- `cross` のセル。span の `left` を、セルの左端にセルの左の padding を足した値にする。大きな区切りのセル自体は横に流し、中身を見えている範囲の左に残す。横に長い見出しにも当たるときは、この値を使う。

R が 0、または半分の上限で行を固定しないときは、固定する行の高さを 0 にする。列も同じで、C が 0、または上限で列を固定しないときは、幅を 0 にする。複数の条件に当たるセルでも span は 1 つにし、必要な `top` と `left` を両方当てる。

外すときは span の子をセルへ戻し、span を消す。元の要素を移すので、子のイベントなどを作り直さない。

## 全幅表示

GitLab の本文の外側にある `div.panel-content` は `contain: layout`、`main#content-body` と `section.work-item-view` は `container-type: inline-size` を持つ。本文の中で `position: fixed` にしても、ブラウザーの窓全体を基準にできない。作業項目の説明には `overflow: hidden` や `overflow-x: auto` の祖先もある。そのため、全幅の背景は `document.body` の直下に作る。

背景は `div[data-md-table-sticky-float]` とし、`position: fixed; inset: 0; z-index: 2147483646; background: rgba(0, 0, 0, 0.4)` を当てる。右下の知らせより下に置き、`role="dialog"`・`aria-modal="true"`・`aria-label="表の全幅表示"` を付ける。中に `div.md[data-md-table-sticky-float-panel]` を作り、`position: absolute; left: 8px; right: 8px; top: 8px; max-height: calc(100vh - 16px)` を当てる。パネルは縦の flex、`box-sizing: border-box; padding: 8px; overflow: hidden` とし、背景・文字の色を GitLab の CSS 変数と代わりの値で決め、枠線・角の丸み・影を付ける。`.md` を付けて表の見た目と対象の判定を保つ。

開く前に、検索の行と列のチェックボックスを含め、ツールバーの上端から表の枠の下端までの高さを測る。ツールバーの前に同じ高さの `div[data-md-table-sticky-placeholder]` を置き、「この表は全幅で表示中（Esc で元に戻す）」を点線の枠・小さい文字・中央揃えで出す。その後、ツールバー、検索の行、列のチェックボックス、表の枠をこの順でパネルへ移す。元の要素を移すので、表の子やイベントを作り直さない。代わりの要素で元の場所の高さを保つ。

パネルの中でも表の枠は内容に応じた幅とし、パネルの幅を上限にする。ツールバー・検索の行・列のチェックボックス・表の枠に `align-self: flex-start` を当て、縦の flex による横への引き伸ばしを避ける。内容の長い表はパネルの幅まで広がり、その中で折り返す。表の枠の `max-height` を外し、`flex: 0 1 auto; min-height: 0` でパネルの残りの高さへ収める。GitLab の包みなら外の `.gl-table-shadow-overlay` を縦の flex にし、中の `[data-sticky-header]` の `max-height` を `none`、`min-height` を 0 にする。自分の包みなら、その包みを縮める。表の枠は余白を 0、`overscroll-behavior: contain` にする。これらの style も `originals` に覚えて戻す。通常の開閉では、その表だけをすぐ測り直し、固定・折り返し・横スクロールを継続する。

閉じる操作は、ツールバーの「閉じる」、Esc、背景そのもののクリック、ブックマークの再実行とする。Esc は `window` の `keydown` を capture で受け、`preventDefault` と `stopPropagation` を呼ぶ。セルを選ぶ状態のとき、または対象が検索の行の中のときは、この受け手は何もしない。セルを選ぶ状態を先にやめ、検索の欄の Esc は先に検索を閉じる。その後の Esc で全幅表示を閉じる。閉じるときに受け手を外す。ツールバー、検索の行、列のチェックボックス、表の枠を代わりの要素の前へこの順で戻し、代わりの要素と背景を消す。検索の行や列のチェックボックスが無いときは、その要素を省いて移して戻す。`syncBar` と `syncColumns` でもこの並びを保つ。開いたときも閉じたときも、ツールバーの `float` のボタンへフォーカスを置く。

`state.floating` は一度に 1 つだけ持つ。別の表を開く前には前の表を閉じる。`scan()` の初めに代わりの要素が接続されているかを確かめ、GitLab の画面の移動などで切り離されたら背景ごと消し、表の記録を片付ける。印刷前にも全幅表示を閉じる。`state.stop` と `printMode(true)` では `closeFloating(true, false)` で閉じ、直後の復元または測り直しに任せて、閉じる処理での再計測を省く。要素の追加・移動・削除はすべて `write()` の中で行う。

## 見張りと元への戻し方

状態は `window.__mdTableSticky` に 1 つ持つ。処理済みの表は `WeakSet` と、元へ戻す記録の `Map` で覚える。書き換える前の `style` 属性の文字列を覚え、外すときに属性ごと戻す。元に無ければ属性を消す。Chromium は、CSSOM で style を書いた直後に `removeAttribute("style")` すると、後で空の `style=""` を書き戻す（Edge で確かめた）。消す前に `getAttribute("style")` を呼び、書き戻しを済ませてから消す。

`MutationObserver` は `document.body` の子の増減を見る。自分の書き換えは対象から外す。記録にある変化した節点から `closest("table")` をたどり、処理済みの表のうち変化のあったものを集める。変化が止まってから 300 ms 後に、その表と新しく見つかった表だけを測り直す。変化が続くときも、最初の変化から 1000 ms で `scan` を走らせる。`scan` が走ったら、最初の変化の時刻を忘れる。更新が続くページでも、新しい表の処理を待たせ続けないため。時刻の表示など、表の外だけの変化では、処理済みの表をすべて測り直さない。切り離された表の記録は捨てる。

自分の包みの中に GitLab の包みが入ったときも、中の表を変化のあった表として扱う。測り直すときに自分の包みを外し、GitLab の包みを使う。

`ResizeObserver` は表を見て、`requestAnimationFrame` で測り直しをまとめる。測った値が変わらなければ style を書き換えない。`refresh()` が同期で測り終えた表とスクロールの範囲の `width`・`height` を記録し、知らせの受け手では寸法を読む関数を呼ばずに各要素の `entry.borderBoxSize[0]` の `inlineSize`・`blockSize`（無ければ `contentRect`）と比べ、幅と高さの両方が 0.5px 以内なら測り直しを見送り、それ以外だけ `requestRefresh()` する。

印刷の前は全幅表示を閉じ、自分の包みの `max-height` を `none` にし、ツールバー、検索の行、列のチェックボックスを隠す。印刷の後は包みの高さと操作の表示を戻す。

初回は表を処理し、右下に 2.5 秒だけ `role="status"` の知らせを出す。重なった表を飛ばした数も知らせる。もう一度動かすと見張りを止め、全幅表示を閉じてから、すべてを戻し、状態を消す。検索の行と列のチェックボックスを消し、スクロール・セルを選ぶ操作・行と列の強調・検索・GitLab の全体表示の切り替えの受け手も外す。検索の 150 ms のタイマーも、検索を閉じるとき、対象から外れるとき、`cleanup`、`state.stop` で取り消す。絞り込みを含め、当てた `tr` とセルの style を元の文字列へ戻す。折り返し・横スクロール・全体表示への対応・セルを選んで固定・行と列を強調・コンパクト・検索・絞り込み・コピー・全幅表示を操作した後も、すべての `.md` の `innerHTML` が始める前と同じになることを試験で確かめる。

## 組み立てと試験

中核はクラシックスクリプトの IIFE にする。DOM に触れない格子と範囲、セルから選ぶ境目、強調するセル、見出しの行数、列の名前、初めの折り返し、セルの折り返し、列の最小の幅、検索の一致、隠す行、コピーする行列、TSV、HTML の関数は、Node の `module.exports` があるときだけ出す。`document` が無ければ DOM の処理には進まない。

組み立ては、行全体のコメント・空行・行頭の字下げを除く。末尾へ `void 0;` を足し、`encodeURIComponent` して `javascript:` の後へ置く。評価の結果を undefined にし、ページが結果の文字列に置き換わらないようにする。

Node の純粋な関数の試験、生成物とソースの一致の試験、生成物を CDP で動かすブラウザーの試験を使う。ブラウザーの試験は、GitLab の版の違いをまねたページを両方使う。ブラウザーの試験では、`Emulation.setDeviceMetricsOverride` で表示の大きさを 1248×804 に固定する。headless の Edge は、起動から十数秒後に窓の中の高さを 34px ほど縮めることがあり、70vh の枠と GitLab の全体表示のボタンの有無が試験の途中で変わるため（Edge で確かめた）。また headless の Edge は、ページを開いて約 20 秒後から数秒の間、ページを何度か隠し、その間のタイマーを遅らせる。見張りの待ちや測り直しの回数など、時間を測る確かめは、測る間に `visibilitychange` で隠れなかった回の結果を使い、隠れたら 10 回までやり直す（Edge で確かめた）。CSP、実際の罫線、ブックマークの登録は、実物の Edge・Chrome でも確かめる。
