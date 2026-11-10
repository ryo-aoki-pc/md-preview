// 図の原文は標準入力だけに渡す。SVG は画像として配り、ページの DOM には入れない。
import { isUtf8 } from 'node:buffer';
import { spawn as spawnProcess } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { accessSync, constants, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { posix, win32, join, extname } from 'node:path';
import { plantumlId, plantumlInput } from './plantuml-source.mjs';

const environment = (env, name) => env[Object.keys(env).find((key) => key.toUpperCase() === name.toUpperCase())];
const unsafeCommand = /[%"\x00-\x1f\x7f]/;
const failure = (message = 'PlantUML の図を描けませんでした') => new Error(message);

export function findPlantuml({ env = process.env, platform = process.platform } = {}) {
  const windows = platform === 'win32';
  const path = windows ? win32 : posix;
  const extensions = windows
    ? (environment(env, 'PATHEXT') || '.EXE;.CMD;.BAT')
        .split(';')
        .map((value) => value.toLowerCase())
        .filter((value) => ['.exe', '.cmd', '.bat'].includes(value))
    : [''];
  for (const folder of ((windows ? environment(env, 'PATH') : env.PATH) || '').split(windows ? ';' : ':')) {
    if (!path.isAbsolute(folder) || (windows && unsafeCommand.test(folder))) continue;
    // ドライブの無いルート相対パスも、現在のドライブに依存するので使わない。
    if (windows && !/^(?:[a-z]:[\\/]|[\\/]{2}[^\\/]+[\\/][^\\/]+(?:[\\/]|$))/i.test(folder)) continue;
    for (const extension of extensions) {
      const command = path.join(folder, 'plantuml' + extension);
      try {
        if (!statSync(command).isFile()) continue;
        if (!windows) accessSync(command, constants.X_OK);
        return command;
      } catch {
        // 存在しないものと実行できないものは飛ばす。
      }
    }
  }
  return null;
}

export function createPlantumlRenderer({
  command,
  timeoutMs = 30000,
  maxSvgBytes = 8 * 1024 * 1024,
  maxCacheBytes = 32 * 1024 * 1024,
  spawn = spawnProcess,
}) {
  const jobs = new Map(),
    queue = [],
    cache = new Map();
  let active = null,
    closed = false,
    cacheBytes = 0;

  function remember(id, svg) {
    if (svg.length > maxCacheBytes) return;
    cache.set(id, svg);
    cacheBytes += svg.length;
    while (cacheBytes > maxCacheBytes) {
      const [old, bytes] = cache.entries().next().value;
      cache.delete(old);
      cacheBytes -= bytes.length;
    }
  }

  function execute(job) {
    return new Promise((resolve, reject) => {
      let child,
        timer,
        stopped,
        killing,
        done = false,
        bytes = 0;
      const chunks = [];
      const delimiter = 'mdpreview-' + randomBytes(24).toString('hex');
      const finish = async (error) => {
        if (done) return;
        done = true;
        clearTimeout(timer);
        await killing;
        if (stopped || error) return reject(stopped || error);
        const output = Buffer.concat(chunks);
        const split = output.indexOf(delimiter);
        const svg = split < 0 ? output : output.subarray(0, split);
        if (!isUtf8(svg)) return reject(failure());
        const value = svg.toString('utf8').trim();
        if (!Buffer.from(value).subarray(0, 1024).includes(Buffer.from('<svg')) || !value.endsWith('</svg>'))
          return reject(failure());
        resolve(Buffer.from(value, 'utf8'));
      };
      job.stop = (error) => {
        if (stopped || done) return;
        stopped = error;
        if (!child?.pid) return;
        if (process.platform === 'win32') {
          killing = new Promise((resolveKill) => {
            try {
              const killer = spawn(
                join(process.env.SystemRoot || 'C:\\Windows', 'System32', 'taskkill.exe'),
                ['/pid', String(child.pid), '/t', '/f'],
                { windowsHide: true, stdio: 'ignore' },
              );
              killer.once('error', () => {
                child.kill();
                resolveKill();
              });
              killer.once('close', resolveKill);
            } catch {
              child.kill();
              resolveKill();
            }
          });
        } else {
          try {
            process.kill(-child.pid, 'SIGKILL');
          } catch {
            child.kill('SIGKILL');
          }
        }
      };
      const args = ['-pipe', '-tsvg', '-charset', 'UTF-8', '-pipedelimitor', delimiter];
      // PlantUML は、安全の設定を Java のプロパティ → 環境変数の順で読む。利用者の JAVA_TOOL_OPTIONS に
      // 別の値があっても SANDBOX になるよう、環境変数に加えて、プロパティも最後に足す (後ろの指定が勝つ)。
      const env = {
        ...process.env,
        PLANTUML_SECURITY_PROFILE: 'SANDBOX',
        JAVA_TOOL_OPTIONS: [
          process.env.JAVA_TOOL_OPTIONS,
          '-Djava.awt.headless=true',
          '-DPLANTUML_SECURITY_PROFILE=SANDBOX',
        ]
          .filter(Boolean)
          .join(' '),
      };
      const options = {
        env,
        cwd: tmpdir(),
        windowsHide: true,
        detached: process.platform !== 'win32',
        stdio: ['pipe', 'pipe', 'pipe'],
      };
      try {
        if (process.platform === 'win32' && /\.(?:cmd|bat)$/i.test(extname(command))) {
          if (unsafeCommand.test(command)) throw failure();
          child = spawn(
            // ComSpec が無いときも、名前だけの cmd.exe (カレントディレクトリと PATH から探される) は使わない。
            process.env.ComSpec || join(process.env.SystemRoot || 'C:\\Windows', 'System32', 'cmd.exe'),
            ['/d', '/s', '/c', '""' + command + '" ' + args.join(' ') + '"'],
            { ...options, windowsVerbatimArguments: true },
          );
        } else child = spawn(command, args, options);
        child.once('error', () => void finish(failure()));
        child.once('close', () => void finish());
        child.stdout.on('data', (chunk) => {
          if (stopped || done) return;
          bytes += chunk.length;
          if (bytes > maxSvgBytes) {
            chunks.length = 0;
            job.stop(failure('PlantUML の図が大きすぎます'));
          } else chunks.push(chunk);
        });
        child.stderr.resume();
        // 入力側の EPIPE に原文や端末のパスを含む誤りを外へ出さない。
        child.stdin.on('error', () => {});
        child.stdin.end(plantumlInput(job.source), 'utf8');
        timer = setTimeout(() => job.stop(failure('PlantUML の描画が時間切れになりました')), timeoutMs);
      } catch {
        void finish(failure());
      }
    });
  }

  function drain() {
    if (closed || active) return;
    let job;
    while ((job = queue.shift())) {
      if (!job.waiters.size) continue;
      active = job;
      job.done = execute(job)
        .then(
          (svg) => {
            if (job.waiters.size && !closed) remember(job.id, svg);
            settle(job, null, svg);
          },
          (error) => settle(job, error),
        )
        .finally(() => {
          active = null;
          drain();
        });
      return;
    }
  }
  function settle(job, error, svg) {
    if (jobs.get(job.id) === job) jobs.delete(job.id);
    for (const waiter of job.waiters) {
      waiter.cleanup();
      if (error) waiter.reject(error);
      else waiter.resolve(svg);
    }
    job.waiters.clear();
  }

  return {
    render(source, { signal } = {}) {
      if (closed) return Promise.reject(failure('PlantUML の描画は終了しています'));
      if (signal?.aborted) return Promise.reject(failure('PlantUML の描画を中止しました'));
      const id = plantumlId(source);
      if (cache.has(id)) return Promise.resolve(cache.get(id));
      let job = jobs.get(id);
      if (!job) {
        job = { id, source, waiters: new Set() };
        jobs.set(id, job);
        queue.push(job);
      }
      return new Promise((resolve, reject) => {
        const abort = () => {
          if (!job.waiters.delete(waiter)) return;
          waiter.cleanup();
          reject(failure('PlantUML の描画を中止しました'));
          if (!job.waiters.size) {
            if (jobs.get(id) === job) jobs.delete(id);
            job.stop?.(failure('PlantUML の描画を中止しました'));
          }
        };
        const waiter = { resolve, reject, cleanup: () => signal?.removeEventListener('abort', abort) };
        job.waiters.add(waiter);
        signal?.addEventListener('abort', abort, { once: true });
        drain();
      });
    },
    async close() {
      closed = true;
      for (const job of jobs.values()) {
        job.stop?.(failure('PlantUML の描画を中止しました'));
        settle(job, failure('PlantUML の描画を中止しました'));
      }
      queue.length = 0;
      cache.clear();
      cacheBytes = 0;
      await active?.done;
    },
  };
}
