#!/usr/bin/env bash
# 配布と CI で同じ設定を使い、両コマンドの実行ファイルと同梱物を作る。
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo '使い方: bash scripts/build-release.sh <版> <出力のフォルダー>' >&2
  exit 2
fi
version="$1"
case "$version" in
  ''|*[!a-zA-Z0-9.+-]*)
    echo '版には英数字・点・ハイフン・+ だけを指定してください。' >&2
    exit 2
    ;;
esac

mkdir -p -- "$2"
output_dir=$(CDPATH='' cd -- "$2" && pwd)
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
cd -- "$script_dir/.."

build_binary() {
  CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build \
    -trimpath -buildvcs=false \
    -ldflags "-s -w -X github.com/ryo-aoki-pc/md-tools/internal/version.Version=$version" \
    -o "$3" "$4"
}

for target in windows/amd64 linux/amd64 linux/arm64; do
  target_os="${target%/*}"
  target_arch="${target#*/}"
  filename="mdpreview-$target_os-$target_arch"
  if [ "$target_os" = windows ]; then
    filename="$filename.exe"
  fi
  build_binary "$target_os" "$target_arch" "$output_dir/$filename" ./cmd/mdpreview
done
build_binary windows amd64 "$output_dir/mdtable-windows-amd64.exe" ./mdtable/cmd/mdtable

# 版とライセンスは、ビルドしたホストで動く一時的な実行ファイルで確かめる。
host_os=$(go env GOHOSTOS)
host_arch=$(go env GOHOSTARCH)
host_exe=''
if [ "$host_os" = windows ]; then
  host_exe='.exe'
fi
temporary_dir=$(mktemp -d "$output_dir/.build-release.XXXXXX")
trap 'rm -f -- "$temporary_dir/mdpreview$host_exe" "$temporary_dir/mdtable$host_exe"; rmdir -- "$temporary_dir"' EXIT
for tool in mdpreview mdtable; do
  package="./cmd/$tool"
  expected_version="$version"
  if [ "$tool" = mdtable ]; then
    package='./mdtable/cmd/mdtable'
    expected_version="mdtable $version"
  fi
  host_binary="$temporary_dir/$tool$host_exe"
  build_binary "$host_os" "$host_arch" "$host_binary" "$package"
  actual_version=$("$host_binary" --version | tr -d '\r')
  if [ "$actual_version" != "$expected_version" ]; then
    echo "$tool の成果物の版が一致しません。" >&2
    exit 1
  fi
  license_file="$output_dir/$tool-THIRD-PARTY-LICENSES.txt"
  "$host_binary" --licenses > "$license_file"
  if [ ! -s "$license_file" ]; then
    echo "$tool の同梱ライセンスの出力が空です。" >&2
    exit 1
  fi
done
(
  cd -- "$output_dir"
  sha256sum mdpreview-windows-amd64.exe mdpreview-linux-amd64 mdpreview-linux-arm64 \
    mdtable-windows-amd64.exe mdpreview-THIRD-PARTY-LICENSES.txt mdtable-THIRD-PARTY-LICENSES.txt > SHA256SUMS
)
