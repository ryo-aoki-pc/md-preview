#!/usr/bin/env bash
# 配布と CI で同じ設定を使い、対象ごとの実行ファイルを作る。
set -eu

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

for target in windows/amd64 linux/amd64 linux/arm64; do
  target_os="${target%/*}"
  target_arch="${target#*/}"
  filename="mdpreview-$target_os-$target_arch"
  if [ "$target_os" = windows ]; then
    filename="$filename.exe"
  fi
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build \
    -trimpath -buildvcs=false \
    -ldflags "-s -w -X github.com/ryo-aoki-pc/md-tools/internal/version.Version=$version" \
    -o "$output_dir/$filename" ./cmd/mdpreview
done
