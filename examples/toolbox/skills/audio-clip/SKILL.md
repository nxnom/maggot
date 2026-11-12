---
name: audio-clip
description: Cut a short, clean audio clip (ringtone, sample, quote) out of an online video or a local media file. Use when the user wants part of a video or song as an audio file, with a start and end time.
license: MIT
metadata:
  version: 1.0.0
  maggot-uses: media-download
  maggot-requires: ffmpeg
---

# Audio clip

Turn "the bit from 1:05 to 1:20 of this video" into an audio file.

## Steps

1. **Ask for what is missing.** You need: the source (a link or a file), the
   start and end time, and the format (default `m4a`; `mp3` for old players,
   `wav` for editing). Ask once, with defaults, instead of guessing.
2. **Get the audio.** For a link, download audio only with the maggot tool:
   `maggot run media-download --json --inputs '{"urls": "<link>", "mode": "m4a"}'`
   and take the file path from `files` in the result. A local file is used as is.
3. **Cut and clean it** with the script in this skill:
   `sh scripts/cut.sh <input> <start> <end> <output>`
   (times as `1:05` or `65`). It cuts exactly, fades in and out over 0.3 s and
   sets the loudness to -14 LUFS so clips sound the same.
4. **Check it.** Run `ffprobe -v error -show_entries format=duration -of csv=p=0 <output>`
   and make sure the length is about end − start. Tell the user the path and length.

## Rules

- Cut from the original download, not from an earlier clip: every re-encode loses quality.
- Keep the clip under 40 s for ringtones (iPhone's limit); say so if the user asks for more.
- Do not add music or effects the user did not ask for.
