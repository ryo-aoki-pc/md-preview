#!/usr/bin/env bash
# 配布と CI で同じ設定を使い、Windows の実行ファイルと同梱物を作る。
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo '使い方: bash scripts/build-release.sh <版> <出力のフォルダー>' >&2
  exit 2
fi
version="$1"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo '版は数字3つを点で区切って指定してください。' >&2
  exit 2
fi

mkdir -p -- "$2"
output_dir=$(CDPATH='' cd -- "$2" && pwd)
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
cd -- "$script_dir/.."

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
  -trimpath -buildvcs=false \
  -ldflags "-s -w -X github.com/ryo-aoki-pc/md-table-excel/internal/version.Version=$version" \
  -o "$output_dir/mdtable-windows-amd64.exe" ./cmd/mdtable

# ライセンスの出力には、ビルドしたホストで動く一時的な実行ファイルを使う。
temporary_dir=$(mktemp -d)
license_binary="$temporary_dir/mdtable$(go env GOEXE)"
trap 'rm -f -- "$license_binary"; rmdir -- "$temporary_dir"' EXIT
CGO_ENABLED=0 GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" go build \
  -trimpath -buildvcs=false \
  -ldflags "-s -w -X github.com/ryo-aoki-pc/md-table-excel/internal/version.Version=$version" \
  -o "$license_binary" ./cmd/mdtable

actual_version=$("$license_binary" --version | tr -d '\r')
if [ "$actual_version" != "mdtable $version" ]; then
  echo '成果物の版が一致しません。' >&2
  exit 1
fi
"$license_binary" --licenses > "$output_dir/THIRD-PARTY-LICENSES.txt"
if [ ! -s "$output_dir/THIRD-PARTY-LICENSES.txt" ]; then
  echo '同梱ライセンスの出力が空です。' >&2
  exit 1
fi
(
  cd -- "$output_dir"
  sha256sum mdtable-windows-amd64.exe THIRD-PARTY-LICENSES.txt > SHA256SUMS
)
