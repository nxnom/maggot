#!/bin/sh
# cut.sh <input> <start> <end> <output>
# Cut [start, end] out of a media file as audio, with short fades and
# loudness set to -14 LUFS. Times are seconds (65) or m:ss (1:05).
set -eu
if [ $# -ne 4 ]; then
  echo "usage: cut.sh <input> <start> <end> <output>" >&2
  exit 2
fi
in=$1 out=$4
secs() { echo "$1" | awk -F: '{ s = 0; for (i = 1; i <= NF; i++) s = s * 60 + $i; print s }'; }
start=$(secs "$2")
end=$(secs "$3")
len=$(awk -v a="$start" -v b="$end" 'BEGIN { print b - a }')
if awk -v l="$len" 'BEGIN { exit !(l <= 0) }'; then
  echo "end must be after start" >&2
  exit 2
fi
fade_out=$(awk -v l="$len" 'BEGIN { s = l - 0.3; print (s < 0 ? 0 : s) }')
ffmpeg -hide_banner -loglevel error -y -ss "$start" -t "$len" -i "$in" -vn \
  -af "afade=t=in:d=0.3,afade=t=out:st=$fade_out:d=0.3,loudnorm=I=-14:TP=-1.5:LRA=11" \
  "$out"
echo "$out"
