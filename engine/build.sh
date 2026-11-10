#!/usr/bin/env bash
# glfm_markdown.wasm を作り直す。開発のときだけ使う (使うホストには Rust もコンテナも要らない)。
#
#   engine/build.sh               Cargo.lock のとおりにビルドし、できた .wasm を engine.lock.json の sha256 と比べる
#   engine/build.sh --update      Cargo.lock の更新を許し、.wasm・licenses/・engine.lock.json の wasm の値を書き直す
#   engine/build.sh --check-gem   rubygems.org のソース gem を取得し、写し (src/glfm.rs・src/formatter.rs・ライセンス) と、
#                                 Cargo.lock の重なる crate の版と checksum が gem と同じことを確かめる (ビルドはしない)
#
# 要るもの: podman (無ければ docker) と node。イメージ・gem・.wasm の固定値は engine.lock.json にある。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
mode=${1:-build}
case "$mode" in
build | --update | --check-gem) ;;
*)
  echo "使い方: engine/build.sh [--update|--check-gem]" >&2
  exit 2
  ;;
esac

lock() { node -e 'const l=require(process.argv[1]);let v=l;for(const k of process.argv[2].split("."))v=v[k];process.stdout.write(String(v))' "$here/engine.lock.json" "$1"; }
sha256() { node -e 'process.stdout.write(require("crypto").createHash("sha256").update(require("fs").readFileSync(process.argv[1])).digest("hex"))' "$1"; }

# 写しが固定値のとおりか (どのモードでも確かめる)
node - "$here" <<'EOF'
const { createHash } = require('crypto');
const { readFileSync } = require('fs');
const { join } = require('path');
const here = process.argv[2];
const lock = require(join(here, 'engine.lock.json'));
for (const [path, record] of Object.entries(lock.gem.files)) {
  const actual = createHash('sha256').update(readFileSync(join(here, path))).digest('hex');
  if (actual !== record.sha256) {
    console.error(`engine/${path} が engine.lock.json の sha256 と一致しません。`);
    process.exit(1);
  }
}
EOF

if [ "$mode" = "--check-gem" ]; then
  work=$(mktemp -d)
  trap 'rm -rf "${work:?}"' EXIT
  curl -fsSL -o "$work/source.gem" "$(lock gem.url)"
  if [ "$(sha256 "$work/source.gem")" != "$(lock gem.sha256)" ]; then
    echo "取得した gem が engine.lock.json の sha256 と一致しません。" >&2
    exit 1
  fi
  tar -xf "$work/source.gem" -C "$work" data.tar.gz
  mkdir "$work/data"
  tar -xzf "$work/data.tar.gz" -C "$work/data"
  node - "$here" "$work/data" <<'EOF'
const { readFileSync } = require('fs');
const { join } = require('path');
const [here, data] = process.argv.slice(2);
const lock = require(join(here, 'engine.lock.json'));
let failed = false;
for (const [path, record] of Object.entries(lock.gem.files)) {
  if (!readFileSync(join(here, path)).equals(readFileSync(join(data, record.gem)))) {
    console.error(`engine/${path} が gem の ${record.gem} と違います。`);
    failed = true;
  }
}
// Cargo.lock の [[package]] の名前と「版 checksum」。同じ名前が複数の版で入ることがあるので、集まりで比べる。
// 比べるのは両方にある crate だけ。gem にしか無いもの (Ruby の橋渡しの magnus など) と、
// こちらにしか無いもの (serde_json など) は、入口が違うので揃わない。
const packages = (text) => {
  const map = new Map();
  for (const block of text.split('[[package]]').slice(1)) {
    const name = /^name = "(.+)"$/m.exec(block)?.[1];
    const version = /^version = "(.+)"$/m.exec(block)?.[1];
    const checksum = /^checksum = "(.+)"$/m.exec(block)?.[1] || '';
    if (name && version) map.set(name, (map.get(name) || new Set()).add(`${version} ${checksum}`));
  }
  return map;
};
const ours = packages(readFileSync(join(here, 'Cargo.lock'), 'utf8'));
const theirs = packages(readFileSync(join(data, lock.gem.cargo_lock), 'utf8'));
let compared = 0;
for (const [name, set] of ours) {
  const other = theirs.get(name);
  if (!other) continue;
  for (const entry of set) {
    compared += 1;
    if (!other.has(entry)) {
      console.error(`${name} ${entry.split(' ')[0]} は gem の Cargo.lock (${[...other].map((e) => e.split(' ')[0]).join(', ')}) と版か checksum が違います。`);
      failed = true;
    }
  }
}
if (!ours.get('comrak') || !theirs.get('comrak') || compared === 0) {
  console.error('Cargo.lock を読めません。');
  failed = true;
}
if (failed) process.exit(1);
console.log(`写しと、Cargo.lock の重なる crate (${compared} 個) の版と checksum は、gem ${lock.gem.version} と一致しています。`);
EOF
  exit 0
fi

engine=$(command -v podman || command -v docker || true)
if [ -z "$engine" ]; then
  echo "podman か docker が要ります。" >&2
  exit 1
fi
# Windows (Git Bash) では、コンテナに渡すパスを C:/… の形にする
host() { (cd "$1" && (pwd -W 2>/dev/null || pwd)); }
out=$(mktemp -d)
trap 'rm -rf "${out:?}"' EXIT
locked=--locked
[ "$mode" = "--update" ] && locked=

# ソースは /src に置く (パスが .wasm に入っても、どのホストでも同じになる)。成果物は /out に出す。
# MSYS_NO_PATHCONV: Git Bash が /src などの引数を Windows のパスに変えないようにする。
# shellcheck disable=SC2016 # 引用符の中の $ は、コンテナの中の sh が展開する
MSYS_NO_PATHCONV=1 "$engine" run --rm --security-opt label=disable \
  -v "$(host "$here"):/src" -v "$(host "$out"):/out" -w /src \
  -e CARGO_TARGET_DIR=/tmp/target -e LOCKED="$locked" \
  "$(lock image)" sh -euc '
    rustup target add wasm32-unknown-unknown >/dev/null 2>&1
    cargo build --release $LOCKED --target wasm32-unknown-unknown
    cp /tmp/target/wasm32-unknown-unknown/release/glfm_markdown_wasm.wasm /out/glfm_markdown.wasm
    # .wasm に入る crate (proc-macro とビルド用の依存は入らない) と、そのライセンスの原文
    # sh には pipefail が無いので、cargo tree の結果はファイルに受けてから読む (失敗を見逃さない)
    mkdir /out/licenses
    cargo tree --target wasm32-unknown-unknown -e normal,no-proc-macro --prefix none --format "{p}" >/tmp/crates.txt
    sed "s/ (\*)$//" /tmp/crates.txt | sort -u >/tmp/crates.sorted
    grep -q "^comrak " /tmp/crates.sorted
    while read -r name version rest; do
        [ "$name" = "glfm-markdown-wasm" ] && continue
        dir=$(ls -d "${CARGO_HOME:-/usr/local/cargo}"/registry/src/*/"$name-${version#v}")
        found=0
        for file in "$dir"/LICENSE* "$dir"/COPYING* "$dir"/COPYRIGHT* "$dir"/license* "$dir"/UNLICENSE*; do
          [ -f "$file" ] || continue
          mkdir -p "/out/licenses/$name-${version#v}"
          cp "$file" "/out/licenses/$name-${version#v}/"
          found=1
        done
        if [ "$found" = 0 ]; then
          echo "ライセンスの原文が見つかりません: $name ${version#v}" >&2
          exit 1
        fi
    done </tmp/crates.sorted
    # root で動くコンテナ (docker) が作ったファイルも、ホストの利用者が片付けられるようにする
    chmod -R a+rwX /out
  '

actual=$(sha256 "$out/glfm_markdown.wasm")
if [ "$mode" = "--update" ]; then
  cp "$out/glfm_markdown.wasm" "$here/glfm_markdown.wasm"
  # gem のライセンス (licenses/ の直下のファイル) は残し、crate ごとのフォルダーだけを入れ替える
  mkdir -p "$here/licenses"
  find "$here/licenses" -mindepth 1 -maxdepth 1 -type d -exec rm -rf {} +
  cp -r "$out/licenses/." "$here/licenses/"
  node - "$here" "$actual" <<'EOF'
const { readFileSync, writeFileSync, statSync } = require('fs');
const { join } = require('path');
const [here, sha256] = process.argv.slice(2);
const path = join(here, 'engine.lock.json');
const lock = JSON.parse(readFileSync(path, 'utf8'));
lock.wasm.sha256 = sha256;
lock.wasm.size = statSync(join(here, lock.wasm.path)).size;
writeFileSync(path, JSON.stringify(lock, null, 2) + '\n');
EOF
  echo "glfm_markdown.wasm を作り直しました (sha256 $actual)。"
elif [ "$actual" = "$(lock wasm.sha256)" ]; then
  echo "glfm_markdown.wasm は engine.lock.json と同じです (sha256 $actual)。"
else
  echo "作り直した .wasm (sha256 $actual) が engine.lock.json と違います。" >&2
  exit 1
fi
