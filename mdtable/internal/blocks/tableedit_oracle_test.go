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
