// ファイルを開かず、ルートからの相対パスだけを組み立てる。
import { posix } from 'node:path';
export function includeBase(file) {
  if (typeof file !== 'string' || !file) return '';
  // IncludeFilter.resolve_relative_path と同じく、拡張子のある文書だけ dirname を取る。
  return posix.extname(file) ? posix.dirname(file) : file;
}
export function resolvePath(target, base = '') {
  if (typeof target !== 'string' || typeof base !== 'string') return { path: '', safe: false };
  // GitLab は POSIX パスだけを扱う。Windows の呼び出し元へドライブ名・逆斜線を
  // 渡してルートをすり抜けないよう、OS によって意味が変わるパスは読み込ませない。
  if (/[\\\0]/.test(target) || /^[a-z]:/i.test(target) || /[\\\0]/.test(base) || /^[a-z]:/i.test(base))
    return { path: target, safe: false };
  const canonical = posix.normalize(target.startsWith('/') ? target : posix.join(base, target));
  const path = canonical.replace(/^\/+/, '');
  // 点と空白だけの区切り (「.. 」や「...」) は、正規化では親のフォルダーにならないが、Windows は末尾の点と空白を
  // 捨てて開くので「..」になる。どの OS でも名前として意味が無いので、読み込ませない。
  const odd = path.split('/').some((segment) => segment !== '.' && /^[. ]+$/.test(segment));
  return { path, safe: !/^\.\.(?:\/|$)/.test(path) && !/^[a-z]:/i.test(path) && !odd };
}
