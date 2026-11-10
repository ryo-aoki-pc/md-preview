// worker と描画する側で、図の識別子と標準入力の作り方を共通にする。
import { createHash } from 'node:crypto';

export const plantumlId = (source) => createHash('sha256').update(source, 'utf8').digest('hex');

export function plantumlInput(source) {
  const terminated = source + (source.endsWith('\n') || source.endsWith('\r') ? '' : '\n');
  return source.startsWith('@start') ? terminated : '@startuml\n' + terminated + '@enduml\n';
}
