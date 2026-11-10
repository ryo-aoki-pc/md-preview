import { homedir } from 'node:os';
import { posix, win32 } from 'node:path';

export function defaultAssetsDirectory(platform = process.platform, env = process.env, home = homedir()) {
  if (platform === 'win32')
    return win32.join(env.LOCALAPPDATA || win32.join(env.USERPROFILE || home, 'AppData', 'Local'), 'md-preview-assets');
  const base =
    platform === 'darwin'
      ? posix.join(home, 'Library', 'Application Support')
      : env.XDG_DATA_HOME && posix.isAbsolute(env.XDG_DATA_HOME)
        ? env.XDG_DATA_HOME
        : posix.join(home, '.local', 'share');
  return posix.join(base, 'md-preview-assets');
}
