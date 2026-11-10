// worker と描画する側で、図の識別子と標準入力の作り方を共通にする。
import { createHash } from 'node:crypto';

export const plantumlId = (source) => createHash('sha256').update(source, 'utf8').digest('hex');

// GitLab は図の原文をそのまま PlantUML のサーバーへ送り、サーバー (UmlExtractor.getUmlSource) は、先頭が
// @start でなければ @startuml と @enduml で包む。同じ決まりにする (先頭の空行や字下げは落とさない。その形でも、
// 包んだ中の @startuml から図になる)。
export function plantumlInput(source) {
  const terminated = source + (source.endsWith('\n') || source.endsWith('\r') ? '' : '\n');
  return source.startsWith('@start') ? terminated : '@startuml\n' + terminated + '@enduml\n';
}
