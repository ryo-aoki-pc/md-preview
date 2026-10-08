#!/usr/bin/env python3
"""mdtable コマンドを導入する（Python 3.9 以降）。

リポジトリの外に専用の仮想環境を作って mdtable と openpyxl を入れ、起動用のコマンドを
~/.local/bin（Windows は %USERPROFILE%\\.local\\bin）に置く。リポジトリを更新したら再実行する。
"""

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
import venv
from pathlib import Path

REPOSITORY = Path(__file__).resolve().parent.parent


def xdg_directory(variable, fallback):
    value = os.environ.get(variable)
    return Path(value) if value and Path(value).is_absolute() else fallback


def default_install_directory():
    if sys.platform == "win32":
        base = Path(os.environ.get("LOCALAPPDATA") or Path.home() / "AppData/Local")
    elif sys.platform == "darwin":
        base = Path.home() / "Library/Application Support"
    else:
        base = xdg_directory("XDG_DATA_HOME", Path.home() / ".local/share")
    return base / "md-table-excel"


def default_bin_directory():
    return Path.home() / ".local" / "bin"


def venv_python(install_directory):
    if sys.platform == "win32":
        return install_directory / "venv" / "Scripts" / "python.exe"
    return install_directory / "venv" / "bin" / "python"


def venv_command(install_directory):
    if sys.platform == "win32":
        return install_directory / "venv" / "Scripts" / "mdtable.exe"
    return install_directory / "venv" / "bin" / "mdtable"


def launcher_path(bin_directory):
    return bin_directory / ("mdtable.exe" if sys.platform == "win32" else "mdtable")


def repository_version():
    namespace = {}
    with open(REPOSITORY / "mdtable" / "__init__.py", encoding="utf-8") as handle:
        exec(handle.read(), namespace)
    return namespace["__version__"]


def installed_version(install_directory):
    python = venv_python(install_directory)
    if not python.exists():
        return None
    result = subprocess.run(
        [str(python), "-c", "import mdtable, openpyxl; print(mdtable.__version__)"],
        capture_output=True, text=True, encoding="utf-8")
    if result.returncode != 0:
        return None
    return result.stdout.strip()


def inside(path, parent):
    try:
        Path(path).resolve().relative_to(Path(parent).resolve())
        return True
    except ValueError:
        return False


def on_path(directory):
    entries = os.environ.get("PATH", "").split(os.pathsep)
    target = os.path.normcase(str(Path(directory).resolve()))
    return any(entry and os.path.normcase(str(Path(entry).resolve())) == target for entry in entries)


def install(install_directory, bin_directory):
    if inside(install_directory, REPOSITORY) or inside(bin_directory, REPOSITORY):
        raise RuntimeError("--install-dir と --bin-dir はリポジトリの外を指定してください（同期対象に仮想環境を作らない）")
    install_directory.mkdir(parents=True, exist_ok=True)
    print("仮想環境を作ります: %s" % (install_directory / "venv"))
    venv.EnvBuilder(with_pip=True, clear=True).create(install_directory / "venv")
    python = venv_python(install_directory)
    # リポジトリにビルドの途中のファイルを作らないよう、写しから入れる
    work = Path(tempfile.mkdtemp(prefix="mdtable-setup-"))
    try:
        source = work / "md-table-excel"
        shutil.copytree(REPOSITORY, source, ignore=shutil.ignore_patterns(
            ".git", "__pycache__", "*.egg-info", "build", "dist", ".claude", ".stfolder"))
        command = [str(python), "-m", "pip", "install", "--disable-pip-version-check", "--no-cache-dir",
                   str(source)]
        result = subprocess.run(command)
        if result.returncode != 0:
            raise RuntimeError("pip install に失敗しました（上の出力を確かめてください）")
    finally:
        shutil.rmtree(work, ignore_errors=True)
    bin_directory.mkdir(parents=True, exist_ok=True)
    launcher = launcher_path(bin_directory)
    if launcher.exists() or launcher.is_symlink():
        launcher.unlink()
    if sys.platform == "win32":
        shutil.copy2(venv_command(install_directory), launcher)
    else:
        launcher.symlink_to(venv_command(install_directory))
    print("mdtable を置きました: %s" % launcher)
    if not on_path(bin_directory):
        print("注意: %s が PATH にありません。PATH に足すか、上のパスで起動してください" % bin_directory)


def check(install_directory, bin_directory):
    problems = []
    version = installed_version(install_directory)
    expected = repository_version()
    if version is None:
        problems.append("仮想環境に mdtable か openpyxl がありません: %s" % (install_directory / "venv"))
    elif version != expected:
        problems.append("導入済みの版（%s）がリポジトリの版（%s）と違います。導入し直してください" % (version, expected))
    launcher = launcher_path(bin_directory)
    if not launcher.exists():
        problems.append("起動用のコマンドがありません: %s" % launcher)
    if not on_path(bin_directory):
        problems.append("%s が PATH にありません" % bin_directory)
    for problem in problems:
        print("問題: " + problem)
    if not problems:
        print("導入済み: mdtable %s（%s）" % (version, launcher))
    return 1 if problems else 0


def main():
    # パイプやファイルへ出すときは UTF-8 にする（端末へはそのまま Unicode で出る）
    for stream in (sys.stdout, sys.stderr):
        try:
            if not stream.isatty():
                stream.reconfigure(encoding="utf-8", errors="replace")
        except (AttributeError, ValueError):
            pass
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--install-dir", type=Path, default=default_install_directory(),
                        help="仮想環境を作るフォルダー（既定: %(default)s）")
    parser.add_argument("--bin-dir", type=Path, default=default_bin_directory(),
                        help="起動用のコマンドを置くフォルダー（既定: %(default)s）")
    parser.add_argument("--check", action="store_true", help="導入の状態を確かめるだけで、何も変えない")
    args = parser.parse_args()
    try:
        if args.check:
            return check(args.install_dir, args.bin_dir)
        install(args.install_dir, args.bin_dir)
        return check(args.install_dir, args.bin_dir)
    except (Exception, KeyboardInterrupt) as error:
        print("mdtable の導入に失敗しました: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
