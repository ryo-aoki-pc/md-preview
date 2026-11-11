package blocks

import "testing"

// 行・列の操作で作る空セルが comrak でも同じブロックになることを確かめる。
func TestTableEditEmptyShapesComrak(t *testing.T) {
	checkComrak(t, []string{
		"| a | b |\n| --- | --- |\n| | |\n",
		"| a | |\n| --- | --- |\n| b | |\n",
		"<table><tr><td></td><td></td></tr></table>\n",
		"> <table>\n> <tr><td></td></tr>\n> </table>\n",
		"> <table>\r\n>   <tr>\r\n>     <td></td>\r\n>     <th rowspan=\"2\">区分</th>\r\n>     <td>a</td>\r\n>   </tr>\r\n>   <tr>\r\n>     <td></td>\r\n>     <td>b</td>\r\n>   </tr>\r\n> </table>",
		"- <table>\r\n    <tr>\r\n      <td>a</td>\r\n      <td></td>\r\n    </tr>\r\n  </table>",
		"> <table>\r\n>   <tr>\r\n>     <th>\r\n>       区分\r\n>     </th>\r\n>     <td>a</td>\r\n>   </tr>\r\n> </table>",
	}, []string{"空の行", "右端の空の列", "HTML の空のセル", "引用の空のセル", "引用でセルが 1 行ずつの表", "リストでセルが 1 行ずつの表", "引用で複数行のセル"})
}

// 写しの挿入と複製の原文が comrak と同じブロックとして読まれることを確かめる。
func TestTableEditPiecesComrak(t *testing.T) {
	checkComrak(t, []string{
		"> <table>\r\n>   <tr>\r\n>     <td class='x'>a</td>\r\n>   </tr>\r\n>   <tr>\r\n>     <td class='x'>a</td>\r\n>   </tr>\r\n> </table>\r\n",
		"- <table>\n  <tr><td>元</td></tr>\n  <tr class='copy'><td>\n\n  - a\n  - b\n\n  </td></tr>\n  </table>\n",
		"a|b\n---|---\nc|d\n|-||\n",
		"a|a|b\n---|---|---\n|<ul><li>x</li></ul>|<ul><li>x</li></ul>|c\n",
	}, []string{"引用内の複数行の行の複製", "リスト内の Markdown のセルの挿入", "行頭の縦棒を足した行", "行頭が HTML の列の組み直し"})
}
