#!/usr/bin/env bash
# Assemble the print HTML of a GitHub Markdown guide (gfm adapter, cc-guide).
#   usage: pandoc/print.sh shareable|internal <out.html>
# Pages come in the order of docs/SUMMARY.md (its ## headings are the navigation
# sections); the internal view adds the pages listed in internal/README.md. Each page
# is converted with pandoc and pandoc/print.lua and starts with its h1; make pdf hands
# the result to tools/guide/print-pdf.mjs. Run it from the guide project root.
set -euo pipefail
[[ $# -eq 2 && ( $1 == shareable || $1 == internal ) ]] || { echo "usage: print.sh shareable|internal <out.html>" >&2; exit 2; }
view=$1
out=$2
here=$(cd "$(dirname "$0")" && pwd)
root=$(pwd -P)
[[ -f docs/SUMMARY.md ]] || { echo "docs/SUMMARY.md not found (run from the guide project root)" >&2; exit 1; }

esc() { sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'; }

# Navigation list: "S|<section title>" and "P|<page path relative to the project root>".
nav() { # nav <list file> <directory of its links>
  sed -n -e 's/^###* \(.*\)$/S|\1/p' \
         -e 's/^[-*] \[[^]]*\](\(\.\/\)\{0,1\}\([^)#]*\.md\)).*$/P|\2/p' "$1" |
    while IFS='|' read -r kind value; do
      if [[ $kind == P ]]; then echo "P|$2/$value"; else echo "S|$value"; fi
    done
}

mkdir -p "$(dirname "$out")"
body=$(mktemp "${TMPDIR:-/tmp}/guide-print.XXXXXX")
trap 'rm -f "$body" "$body.nav"' EXIT
{
  nav docs/SUMMARY.md docs
  if [[ $view == internal ]]; then nav internal/README.md internal; fi
} > "$body.nav"

pending="" open=0 count=0
while IFS='|' read -r kind value; do
  if [[ $kind == S ]]; then
    pending=$value
    continue
  fi
  [[ -f "$value" ]] || { echo "print.sh: page $value (listed in the navigation) not found" >&2; rm -f "$body.nav"; exit 1; }
  if [[ -n "$pending" ]]; then
    [[ $open -eq 1 ]] && echo '</section>' >> "$body"
    printf '<section class="guide-nav">\n<h1 class="guide-section">%s</h1>\n' "$(printf '%s' "$pending" | esc)" >> "$body"
    pending="" open=1
  fi
  echo '<article class="guide-source-page">' >> "$body"
  pandoc --from gfm --to html5 --lua-filter "$here/print.lua" \
    --metadata guide_root="$root" --metadata page_path="$value" "$value" >> "$body"
  echo '</article>' >> "$body"
  count=$((count + 1))
done < "$body.nav"
rm -f "$body.nav"
[[ $open -eq 1 ]] && echo '</section>' >> "$body"
[[ $count -gt 0 ]] || { echo "print.sh: no pages listed in docs/SUMMARY.md" >&2; exit 1; }

title=$(sed -n 's/^title: *//p' guide.yaml | head -1 | sed -e 's/^"//' -e 's/"$//' | esc)
tmp="$out.tmp"
{
  printf '<!DOCTYPE html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n<title>%s</title>\n<style>\n' "$title"
  cat "$here/page.css"
  cat <<'CSS'
body { max-width: none; margin: 0; }
section.guide-nav { break-before: page; }
section.guide-nav > article:first-of-type { break-before: avoid !important; page-break-before: avoid !important; }
h1.guide-section { font-size: 20pt; border-bottom: 2px solid #495057; padding-bottom: 4px; }
CSS
  printf '</style>\n</head>\n<body>\n'
  cat "$body"
  printf '</body>\n</html>\n'
} > "$tmp"
mv "$tmp" "$out"
echo "print HTML $out: $count pages"
