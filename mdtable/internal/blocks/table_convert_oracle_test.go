package blocks

import "testing"

// 変換と削除の前後の例を、既存の comrak との照合にも通す。
func TestTableConvertAndRemovalComrak(t *testing.T) {
	checkComrak(t, []string{
		"前\n\n| 見出し |\n| --- |\n| 本文 |\n\n後\n",
		"前\n\n<table>\n  <tr>\n    <th>見出し</th>\n  </tr>\n  <tr>\n    <td>本文</td>\n  </tr>\n</table>\n\n後\n",
		"> | 見出し |\r\n> | --- |\r\n> | 本文 |\r\n",
		"> <table>\r\n> <tr><th>見出し</th></tr>\r\n> <tr><td>本文</td></tr>\r\n> </table>\r\n",
		"- 項目\n\n  | 見出し |\n  | --- |\n  | 本文 |\n\n- 次\n",
		"- 項目\n\n  <table>\n  <tr><th>見出し</th></tr>\n  <tr><td>本文</td></tr>\n  </table>\n\n- 次\n",
		"前\n\n後\n\n| 残す |\n| --- |\n| 表 |\n",
		"- 項目\r\n\r\n- 次\r\n\r\n  続き\r\n",
	}, []string{"本文のパイプ", "本文の HTML", "引用のパイプ", "引用の HTML", "リストのパイプ", "リストの HTML", "削除後の本文", "削除後のリスト"})
}
