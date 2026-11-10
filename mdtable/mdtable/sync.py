"""書き出し（Markdown → Excel）と取り込み（Excel → Markdown）の手順。"""

import difflib
import os
from copy import copy

from . import __version__, blocks, html_table, pipe_table, workbook
from .cell_syntax import NotRepresentable
from .changes import compare_models
from .document import (MarkdownError, MarkdownFile, Replacement, apply_replacements,
                       find_tables, select_tables, write_text_atomic)
from .model import TableModel


class MdTableError(Exception):
    pass


class Report(object):
    """利用者に出す結果。messages は表ごとの結果、warnings は警告、errors は止める理由。"""

    def __init__(self):
        self.messages = []
        self.warnings = []
        self.errors = []
        self.diff = ""
        self.changed = False
        self.output = None


def _occurrences(regions):
    counts = {}
    result = {}
    for region in regions:
        if region.excluded is not None:
            continue
        key = region.source_hash()
        result[region.number] = counts.get(key, 0)
        counts[key] = counts.get(key, 0) + 1
    return result


def _range_text(region):
    return "%d〜%d 行" % (region.start + 1, region.end + 1)


def list_tables(md_path):
    md = MarkdownFile.load(md_path)
    return find_tables(md)


def default_output(md_path):
    stem = os.path.splitext(os.path.basename(md_path))[0]
    return os.path.join(os.path.dirname(os.path.abspath(md_path)), stem + ".tables.xlsx")


def export(md_path, xlsx_path=None, selectors=None, force=False):
    report = Report()
    md = MarkdownFile.load(md_path)
    regions = find_tables(md)
    if not regions:
        raise MdTableError("表がありません: %s" % md_path)
    chosen = select_tables(regions, selectors)
    occurrences = _occurrences(regions)
    same = {}
    for region in regions:
        if region.excluded is None:
            key = region.source_hash()
            same[key] = same.get(key, 0) + 1
    tables = []
    excluded = []
    for region in chosen:
        if region.excluded is None:
            for cell in region.parsed.model.cells:
                reason = workbook.check_cell_text(cell.text)
                if reason:
                    region.excluded = "%d 行 %d 列のセルに%s" % (cell.row + 1, cell.col + 1, reason)
                    break
        if region.excluded:
            excluded.append(region)
            report.warnings.append("表 %d（%s）は対象外: %s" % (region.number, _range_text(region), region.excluded))
            continue
        for warning in region.warnings:
            report.warnings.append("表 %d: %s" % (region.number, warning))
        tables.append(workbook.ExportTable(region, region.parsed.model, occurrences[region.number],
                                           same[region.source_hash()]))
    if not tables:
        raise MdTableError("書き出せる表がありません")
    xlsx_path = os.path.abspath(xlsx_path or default_output(md_path))
    if os.path.exists(xlsx_path) and not force:
        raise MdTableError("ブックが既にあります（上書きするなら --force）: %s" % xlsx_path)
    workbook.write(xlsx_path, os.path.abspath(md_path), tables, excluded, __version__)
    for table in tables:
        report.messages.append("表 %d（%s）→ シート「%s」" % (table.region.number, _range_text(table.region),
                                                          table.sheet_name))
    report.output = xlsx_path
    return report


def _normalized_sheet_model(sheet_model, region):
    """シートのモデルを、元の表の種類に合わせて比べられる形にする。"""
    model = TableModel(region.kind, sheet_model.rows, sheet_model.cols,
                       [copy(cell) for cell in sheet_model.cells])
    export_model = region.parsed.model
    if region.kind == "pipe":
        for cell in model.cells:
            cell.header = cell.row == 0
    else:
        # 元の表でセルの無かった位置が、空のままならセルの無い位置として扱う
        holes = set()
        kept = []
        for cell in model.cells:
            position = (cell.row, cell.col)
            if (position in export_model.holes and not cell.text and cell.rowspan == 1
                    and cell.colspan == 1 and not cell.header and cell.align is None):
                holes.add(position)
            else:
                kept.append(cell)
        model = TableModel(region.kind, model.rows, model.cols, kept, holes)
    return model


def _render(region, model, warnings, keep_kind=False):
    """新しい行と、HTML の表に変えたときの理由（変えなければ None）。"""
    if region.kind == "pipe":
        try:
            return pipe_table.render(region, model, warnings), None
        except NotRepresentable as error:
            if keep_kind:
                raise
            return html_table.render_from_pipe(region, model, warnings), str(error)
    return html_table.render(region, model, warnings), None


def _already_applied(sheet_model, region, md):
    """シートの内容を今の表に書き戻しても変わらないか（取り込んだブックをもう一度取り込んだとき）。

    書き戻すときに列の配置を多いほうにそろえたり、セルの文字を収束する形に直したりするので、
    モデルではなく、書き戻した行が今の行と同じかで決める。
    """
    model = _normalized_sheet_model(sheet_model, region)
    if model.same_content(region.parsed.model):
        return True
    try:
        lines, _ = _render(region, model, [])
    except NotRepresentable:
        return False
    return lines == md.lines[region.start:region.end + 1]


def _trailing_empty(model):
    """結合も文字も無い末尾の行・列の数（パイプ表の見出し行は除く）。"""
    row_end = 1 if model.kind == "pipe" else 0
    col_end = 0
    for cell in model.cells:
        if cell.text or cell.rowspan > 1 or cell.colspan > 1:
            row_end = max(row_end, cell.row + cell.rowspan)
            col_end = max(col_end, cell.col + cell.colspan)
    return model.rows - row_end, model.cols - col_end


def import_workbook(xlsx_path, md_path=None, dry_run=False, force=False, keep_kind=False, strict_text=False,
                    tables=None):
    report = Report()
    data = workbook.read(xlsx_path, force=force, strict=strict_text, tables=tables)
    report.warnings.extend(data.warnings)
    if md_path is None:
        md_path = data.markdown_path
        if data.markdown_relpath:
            candidate = os.path.join(os.path.dirname(os.path.abspath(xlsx_path)), data.markdown_relpath)
            if os.path.exists(candidate):
                md_path = candidate
    if not md_path or not os.path.exists(md_path):
        raise MdTableError("書き戻す Markdown が見つかりません（--md で指定してください）: %s" % md_path)
    md = MarkdownFile.load(md_path)
    report.output = os.path.abspath(md_path)
    regions = find_tables(md)
    by_hash = {}
    for region in regions:
        if region.excluded is None:
            by_hash.setdefault(region.source_hash(), []).append(region)

    # ブックの表の元の本文。同じ番号の今の表がこのどれかなら、文書の中で表を入れ替えたとみる
    known_hashes = data.source_hashes
    replacements = []
    for table in data.tables:
        label = "表 %d" % table.number
        if table.missing:
            report.warnings.append("%s: %s。この表は変えない" % (label, table.missing))
            continue
        for warning in table.warnings:
            report.warnings.append("%s: %s" % (label, warning))
        for error in table.errors:
            report.errors.append("%s: %s" % (label, error))
        sheet_model = TableModel(None, table.model.rows, table.model.cols,
                                 [copy(cell) for cell in table.model.cells])
        if table.kind == "pipe":
            for cell in sheet_model.cells:
                cell.header = cell.row == 0
        if table.fingerprint and sheet_model.fingerprint() == table.fingerprint:
            report.messages.append("%s: 変更なし" % label)
            continue
        positional = regions[table.number - 1] if 0 < table.number <= len(regions) else None
        if positional is not None and positional.excluded is not None:
            positional = None
        if (positional is not None and positional.source_hash() != table.source_hash
                and positional.source_hash() not in known_hashes
                and _already_applied(table.model, positional, md)):
            report.messages.append("%s: 適用済み（変更なし）" % label)
            continue
        candidates = by_hash.get(table.source_hash, [])
        count_matches = table.same is None or len(candidates) == table.same
        region = candidates[table.occurrence] \
            if count_matches and 0 <= table.occurrence < len(candidates) else None
        if region is None:
            if positional is not None:
                if _already_applied(table.model, positional, md):
                    report.messages.append("%s: 適用済み（変更なし）" % label)
                    continue
                if force:
                    region = positional
                    report.warnings.append("%s: 書き出した後に Markdown の表が変わっていたが、--force でブックの内容で上書きする" % label)
            if region is None:
                if not count_matches:
                    report.errors.append("%s: 衝突: 同じ内容の表の数が、書き出したとき（%d 個）と違う（今は %d 個）ので、どの表か決められない（同じ番号の表をブックの内容で上書きするなら --force）"
                                         % (label, table.same, len(candidates)))
                else:
                    report.errors.append("%s: 衝突: 書き出した後に Markdown の表が変わっている（ブックの内容で上書きするなら --force）" % label)
                continue
        model = _normalized_sheet_model(table.model, region)
        if model.rows == 0 or not model.cells:
            report.warnings.append("%s: シートが空なので、この表は変えない" % label)
            continue
        if model.same_content(region.parsed.model):
            report.messages.append("%s（%s）: 変更なし" % (label, _range_text(region)))
            continue
        warnings = []
        try:
            lines, converted = _render(region, model, warnings, keep_kind=keep_kind)
        except NotRepresentable as error:
            if keep_kind and region.kind == "pipe":
                reason = str(error)
                if error.row is not None and error.col is not None:
                    reason = "%d 行 %d 列: %s" % (error.row, error.col, reason)
                report.errors.append(
                    "%s: パイプ表のままでは書けない（%s）。HTML の表に変えてよければ --keep-kind を外す"
                    % (label, reason))
            else:
                report.errors.append("%s: 書き戻せない: %s" % (label, error))
            continue
        for warning in warnings:
            report.warnings.append("%s: %s" % (label, warning))
        original = md.lines[region.start:region.end + 1]
        if lines == original:
            report.messages.append("%s（%s）: 変更なし" % (label, _range_text(region)))
            continue
        old_model = region.parsed.model
        # 行を消して空になった列と、列を消して空になった行は、中身を消したものではないので数えない
        for count, previous, unit, kept in zip(_trailing_empty(model), _trailing_empty(old_model), ("行", "列"),
                                               (model.cols >= old_model.cols, model.rows >= old_model.rows)):
            if count > previous and kept:
                report.warnings.append(
                    "%s: 末尾の %d %sが空になっている（%sごと消すなら、Excel で%sを削除する）"
                    % (label, count - previous, unit, unit, unit))
        replacements.append(Replacement(region, lines, model))
        changes = compare_models(old_model, model)
        summary = changes.summary()
        if converted:
            report.messages.append("%s（%s）: %sため HTML の表に変えて更新%s"
                                   % (label, _range_text(region), converted, summary))
            if region.tight_container:
                report.warnings.append("%s: 詰めたリストの中で HTML の表にしたので、GitLab では項目の間隔が広がる" % label)
        else:
            report.messages.append("%s（%s）: 更新%s" % (label, _range_text(region), summary))
        if dry_run:
            report.messages.extend(changes.details())

    if report.errors:
        report.messages.append("問題があるので書き戻していません")
        return report
    if not replacements:
        return report
    new_text = apply_replacements(md, replacements)
    problems = verify(md, new_text, replacements)
    if problems:
        report.errors.extend(problems)
        report.messages.append("書き戻した結果の確認に失敗したので書き戻していません")
        return report
    report.diff = "".join(difflib.unified_diff(
        md.text().splitlines(True), new_text.splitlines(True),
        fromfile=md_path, tofile=md_path))
    report.changed = True
    if not dry_run:
        write_text_atomic(md_path, new_text, bom=md.bom)
    return report


def _leaf_signature(doc, skip_ranges, shift):
    """表の範囲の外の葉のブロックの、種類・祖先・行（ずらした後）の並び。"""
    result = []
    for node in doc.root.walk():
        if node.kind not in (blocks.PARAGRAPH, blocks.HEADING, blocks.CODE_BLOCK,
                             blocks.HTML_BLOCK, blocks.THEMATIC_BREAK, blocks.TABLE):
            continue
        if any(start <= node.start_line <= end or start <= node.end_line <= end
               for start, end in skip_ranges):
            continue
        chain = tuple(a.kind for a in node.ancestors())
        result.append((node.kind, chain, shift(node.start_line), shift(node.end_line)))
    return result


def convert(md_path, selectors, target, dry_run=False):
    """セルの中身を保ったまま、選んだ表の種類だけを変える。"""
    if not selectors:
        raise MdTableError("変える表を --table で指定してください")
    if target not in ("html", "pipe"):
        raise MdTableError("変える先の種類は html か pipe を指定してください")
    report = Report()
    md = MarkdownFile.load(md_path)
    report.output = os.path.abspath(md_path)
    regions = find_tables(md)
    chosen = select_tables(regions, selectors)
    replacements = []
    kind_name = "HTML" if target == "html" else "Markdown"
    for region in chosen:
        label = "表 %d" % region.number
        if region.excluded is not None:
            report.errors.append("%s: 対象外なので変えられない（%s）" % (label, region.excluded))
            continue
        if region.kind == target:
            report.messages.append("%s（%s）: もう %s の表" % (label, _range_text(region), kind_name))
            continue
        warnings = []
        try:
            if target == "html":
                lines = html_table.render_from_pipe(region, region.parsed.model, warnings)
            else:
                lines = pipe_table.render_new(region, region.parsed.model, warnings)
        except NotRepresentable as error:
            reason = str(error)
            if error.row is not None and error.col is not None:
                reason = "%d 行 %d 列: %s" % (error.row, error.col, reason)
            report.errors.append("%s: %s の表にできない: %s" % (label, kind_name, reason))
            continue
        # 見出しより多いセルがある行など、表を読んだときの警告（種類を変えると、その分は消える）
        for warning in list(region.warnings) + warnings:
            report.warnings.append("%s: %s" % (label, warning))
        if target == "html" and region.tight_container:
            report.warnings.append("%s: 詰めたリストの中で HTML の表にしたので、GitLab では項目の間隔が広がる" % label)
        replacements.append(Replacement(region, lines, region.parsed.model, target_kind=target))
        report.messages.append("%s（%s）: %s の表に変える" % (label, _range_text(region), kind_name))
    if not report.errors and replacements:
        new_text = apply_replacements(md, replacements)
        report.errors.extend(verify(md, new_text, replacements))
    if report.errors:
        report.messages.append("問題があるので書き戻していません")
        return report
    if not replacements:
        return report
    report.diff = "".join(difflib.unified_diff(
        md.text().splitlines(True), new_text.splitlines(True), fromfile=md_path, tofile=md_path))
    report.changed = True
    if not dry_run:
        write_text_atomic(md_path, new_text, bom=md.bom)
    return report


def verify(md, new_text, replacements):
    """書き戻した文書を読み直し、表と前後の解釈が崩れていないか確かめる。"""
    problems = []
    new_md = MarkdownFile(md.path, new_text, md.bom)
    ordered = sorted(replacements, key=lambda r: r.region.start)
    old_ranges = []
    new_ranges = []
    delta = 0
    for replacement in ordered:
        region = replacement.region
        old_ranges.append((region.start, region.end))
        new_start = region.start + delta
        new_end = new_start + len(replacement.lines) - 1
        new_ranges.append((new_start, new_end))
        delta += len(replacement.lines) - region.line_count()

    def shift_old(line):
        offset = 0
        for (start, end), replacement in zip(old_ranges, ordered):
            if line > end:
                offset += len(replacement.lines) - (end - start + 1)
        return line + offset

    old_signature = _leaf_signature(md.doc, old_ranges, shift_old)
    new_signature = _leaf_signature(new_md.doc, new_ranges, lambda line: line)
    if old_signature != new_signature:
        problems.append("書き戻すと、表の前後の Markdown の解釈が変わる")

    new_regions = find_tables(new_md)
    for replacement, (start, end) in zip(ordered, new_ranges):
        region = replacement.region
        # 種類を変えたときに後ろへ足した空行は、表の範囲に入らない
        found = [r for r in new_regions if r.start == start and r.end <= end
                 and all(not new_md.lines[i].strip(" \t>") for i in range(r.end + 1, end + 1))]
        if not found or (replacement.target_kind is not None
                         and (len(found) != 1 or found[0].kind != replacement.target_kind)):
            problems.append("表 %d: 書き戻した表を読み直せない" % region.number)
            continue
        again = found[0]
        if again.excluded:
            problems.append("表 %d: 書き戻した表が対象外になる（%s）" % (region.number, again.excluded))
            continue
        if again.location != region.location:
            problems.append("表 %d: 書き戻すと表の場所（%s）が変わる" % (region.number, region.location or "本文"))
            continue
        if replacement.target_kind is not None:
            if not again.parsed.model.same_content(replacement.model):
                # どのセルがどう変わるかを 2 個まで出す（連続した空白が 1 つにまとまる、など）。
                # セルが同じなら、行・列の数の違いを出す
                changes = compare_models(replacement.model, again.parsed.model)
                details = [detail.strip() for detail in changes.details(limit=2)]
                problems.append("表 %d: 種類を変えると、表の中身が変わる%s"
                                % (region.number, "（%s）" % "、".join(details) if details else changes.summary()))
            continue
        if region.kind == "html" and again.kind == "html" and replacement.model is not None:
            def structure(model):
                return (model.rows, model.cols,
                        [(c.row, c.col, c.rowspan, c.colspan, c.header, c.align) for c in model.cells])

            if structure(again.parsed.model) != structure(replacement.model):
                problems.append("表 %d: 書き戻した表の形・見出し・配置が、ブックの内容と違う" % region.number)
                continue
        # 書き戻した表をもう一度書き出した内容を、元の表に書き戻しても同じ行になること（収束）
        model = _normalized_sheet_model(again.parsed.model, region)
        try:
            lines, _converted = _render(region, model, [])
        except NotRepresentable as error:
            problems.append("表 %d: 書き戻した表をもう一度書き戻せない（%s）" % (region.number, error))
            continue
        if lines != replacement.lines:
            problems.append("表 %d: 書き戻した結果が安定しない" % region.number)
    return problems
