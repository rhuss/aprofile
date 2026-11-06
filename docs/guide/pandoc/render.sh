#!/usr/bin/env bash
# Render the guide's Markdown pages to HTML with pandoc (gfm adapter, cc-guide).
#   usage: pandoc/render.sh <out dir> <src dir>:<subdir>...
# Each <src dir> is rendered into <out dir>/<subdir> (use "." for the root), one HTML
# page per Markdown page, links rewritten from .md to .html, other files copied as is.
set -euo pipefail
[[ $# -ge 2 ]] || { echo "usage: render.sh <out dir> <src dir>:<subdir>..." >&2; exit 2; }
out=$1; shift
here=$(cd "$(dirname "$0")" && pwd)
rm -rf "$out"
mkdir -p "$out/_assets"
cp "$here/page.css" "$out/_assets/page.css"
for spec in "$@"; do
  src=${spec%%:*}
  sub=${spec#*:}
  dest=$out/$sub
  mkdir -p "$dest"
  rsync -a --exclude '*.md' "$src/" "$dest/"
  (cd "$src" && find . -name '*.md' -type f) | sort | while read -r rel; do
    rel=${rel#./}
    html="$dest/${rel%.md}.html"
    mkdir -p "$(dirname "$html")"
    title=$(sed -n 's/^# //p' "$src/$rel" | head -1)
    prefix=""
    IFS=/ read -ra parts <<< "$(dirname "$sub/$rel")"
    for part in "${parts[@]}"; do
      if [[ -n "$part" && "$part" != "." ]]; then prefix="../$prefix"; fi
    done
    css="${prefix}_assets/page.css"
    pandoc --from gfm --to html5 --standalone --lua-filter "$here/links.lua" \
      --css "$css" --metadata pagetitle="${title:-$rel}" -o "$html" "$src/$rel"
  done
done
# A start page at the root of the view.
if [[ ! -f "$out/index.html" ]]; then
  for candidate in index.html README.html docs/index.html docs/README.html; do
    if [[ -f "$out/$candidate" ]]; then
      printf '<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0; url=%s"><a href="%s">Start</a>\n' \
        "$candidate" "$candidate" > "$out/index.html"
      break
    fi
  done
fi
echo "rendered $out"
