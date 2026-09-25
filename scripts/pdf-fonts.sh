#!/bin/sh
# Regenerates internal/vuln/fonts: Noto Sans cut down to what a PDF report
# can contain — Latin text (package names, versions, advisory summaries) and
# the characters of the translated labels in internal/vuln/labels.go.
#
# Run after changing a label. TestPDFFontsCoverLabels says when it is needed.
# Runs fonttools in Docker; nothing is installed on the host.
set -eu
cd "$(dirname "$0")/.."
go run ./scripts/pdffonts > /tmp/pdffonts-labels.tsv
docker run --rm -v "$PWD/internal/vuln/fonts:/out" -v /tmp/pdffonts-labels.tsv:/labels.tsv:ro python:3.12-slim sh -c '
set -eu
pip install -q fonttools >/dev/null 2>&1
apt-get update -qq >/dev/null && apt-get install -y -qq curl >/dev/null
cd /tmp
# Latin text, punctuation, arrows, the euro and trade marks: what advisory
# summaries and package names contain.
LATIN="U+0020-007E,U+00A0-024F,U+2000-206F,U+20AC,U+2122,U+2190-21FF,U+2713,U+2717"
while IFS="$(printf "\t")" read -r lang chars; do
  case "$lang" in
    zh-TW) src=notosanstc/NotoSansTC ;;
    zh-CN) src=notosanssc/NotoSansSC ;;
    ja)    src=notosansjp/NotoSansJP ;;
    ko)    src=notosanskr/NotoSansKR ;;
  esac
  curl -fsSL -o var.ttf "https://github.com/google/fonts/raw/main/ofl/${src}%5Bwght%5D.ttf"
  for w in 400 700; do
    python -m fontTools.varLib.instancer -q var.ttf wght=$w -o inst.ttf
    name=regular; [ $w = 700 ] && name=bold
    python -m fontTools.subset inst.ttf --unicodes="$LATIN" --text="$chars" \
      --no-hinting --desubroutinize --layout-features="" --drop-tables+=GSUB,GPOS,GDEF \
      --name-IDs="*" --name-languages="*" \
      --output-file="/out/${lang}-${name}.ttf"
  done
done < /labels.tsv
curl -fsSL -o /out/OFL.txt https://github.com/google/fonts/raw/main/ofl/notosanstc/OFL.txt
'
ls -la internal/vuln/fonts
