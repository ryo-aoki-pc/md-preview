# 表の操作の見本

パイプの表を HTML の表に変えた形（`mdtable convert` と同じ）:

<table>
<thead>
<tr>
<th align="left">名前</th>
<th align="right">数</th>
<th align="center">備考</th>
</tr>
</thead>
<tbody>
<tr>
<td align="left">

**りんご**

</td>
<td align="right">1</td>
<td align="center">

[リンク](https://example.com)

</td>
</tr>
<tr>
<td align="left">

`みかん`

</td>
<td align="right">20</td>
<td align="center">縦棒 | 入り</td>
</tr>
<tr>
<td align="left">空</td>
<td align="right"></td>
<td align="center"></td>
</tr>
</tbody>
</table>

結合したセル（覆ったセルは消し、左上だけを残す）:

<table>
<thead>
<tr>
<th colspan="2">結合した見出し</th>
<th>3</th>
</tr>
</thead>
<tbody>
<tr>
<td rowspan="2" colspan="2">2 行 2 列の結合</td>
<td>右上</td>
</tr>
<tr>
<td>右下</td>
</tr>
<tr>
<th rowspan="2">見出しセルの結合</th>
<td>a</td>
<td>b</td>
</tr>
<tr>
<td>c</td>
<td>d</td>
</tr>
</tbody>
</table>

行を全部覆う結合（空の行を残す）:

<table>
<tbody>
<tr>
<td rowspan="2" colspan="3">2 行を全部結合</td>
</tr>
<tr>
</tr>
<tr>
<td>a</td>
<td>b</td>
<td>c</td>
</tr>
</tbody>
</table>

結合を解いた形（左上と同じタグ・揃えの空のセル）:

<table>
<tbody>
<tr>
<td align="center">解いた左上</td>
<td align="center"></td>
</tr>
<tr>
<td align="center"></td>
<td align="center"></td>
</tr>
</tbody>
</table>

本文の見出しセルと、揃えの書き方:

<table>
<thead>
<tr>
<th>列</th>
<th align="right">右</th>
<th style="text-align: center">中央</th>
</tr>
</thead>
<tbody>
<tr>
<th>本文の見出しセル</th>
<td style="text-align:right;">右（空白なし）</td>
<td style="text-align: left; color: red">左と色</td>
</tr>
<tr>
<th align="left">左の見出しセル</th>
<td>既定</td>
<td align="center" style="text-align: right">両方</td>
</tr>
</tbody>
<tfoot>
<tr>
<td>合計</td>
<td align="right">21</td>
<td></td>
</tr>
</tfoot>
</table>

HTML の表の中のタスク:

<table>
<tbody>
<tr>
<td>[ ]</td>
<td>

- [ ] 作業

</td>
</tr>
</tbody>
</table>
