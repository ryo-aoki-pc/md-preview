// 本物の PlantUML や Java を使わず、標準入力とプロセスの制御を確かめる。
import { chmodSync, existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { createPlantumlRenderer } from '../lib/plantuml.mjs';
import { temp } from './helpers.mjs';

export const SVG = '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="40"><text y="20">試験の図</text></svg>';

export function fakePlantuml(t, options = {}) {
  const root = temp(t),
    log = join(root, 'calls.jsonl'),
    script = join(root, 'fake.mjs');
  const command = join(root, process.platform === 'win32' ? 'plantuml.cmd' : 'plantuml');
  writeFileSync(
    script,
    `
import { appendFileSync } from 'node:fs';
import { spawn } from 'node:child_process';
import { setTimeout as delay } from 'node:timers/promises';
let source = '';
for await (const chunk of process.stdin) source += chunk;
const record = (value) => appendFileSync(${JSON.stringify(log)}, JSON.stringify(value) + '\\n');
record({ kind: 'start', pid: process.pid, source, args: process.argv.slice(2), cwd: process.cwd(),
  security: process.env.PLANTUML_SECURITY_PROFILE, java: process.env.JAVA_TOOL_OPTIONS });
if (source.includes('tree')) {
  const child = spawn(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], { windowsHide: true });
  record({ kind: 'child', pid: child.pid });
}
if (source.includes('hang') || source.includes('tree')) await new Promise(() => setInterval(() => {}, 1000));
if (source.includes('slow')) await delay(180);
const delimiter = process.argv[process.argv.indexOf('-pipedelimitor') + 1];
if (source.includes('png')) process.stdout.write(Buffer.from([137, 80, 78, 71, 13, 10]));
else if (source.includes('plain')) process.stdout.write('画像ではありません');
else if (source.includes('invalid-utf8')) process.stdout.write(Buffer.from([60,115,118,103,62,255,60,47,115,118,103,62]));
else if (source.includes('oversize')) process.stdout.write('x'.repeat(40000));
else if (!source.includes('empty')) {
  process.stdout.write(' \\n' + ${JSON.stringify(SVG)} + '\\n' + delimiter + '\\r\\n');
  if (source.includes('multiple')) process.stdout.write('<svg>二つ目</svg>\\n' + delimiter + '\\n');
}
process.stderr.write('試験用の標準エラー');
record({ kind: 'end', pid: process.pid });
if (source.includes('error200')) process.exitCode = 200;
`,
  );
  if (process.platform === 'win32') writeFileSync(command, '@"' + process.execPath + '" "' + script + '" %*\r\n');
  else {
    // 本物と同じくシェルの起動役にする。exec を使わず、起動役・Node.js・その子が同じプロセスグループに並ぶ形にして、
    // グループごと止める経路を確かめる。
    const quote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
    writeFileSync(command, '#!/bin/sh\n' + quote(process.execPath) + ' ' + quote(script) + ' "$@"\n');
    chmodSync(command, 0o755);
  }
  const renderer = createPlantumlRenderer({ command, ...options });
  t.after(() => renderer.close());
  return {
    root,
    command,
    renderer,
    calls: () =>
      existsSync(log)
        ? readFileSync(log, 'utf8')
            .trim()
            .split('\n')
            .filter(Boolean)
            .map((line) => JSON.parse(line))
        : [],
  };
}
