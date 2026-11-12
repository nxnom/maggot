# Media Download

Downloads from YouTube and 1000+ other sites that yt-dlp supports.

A manifest-only tool: it runs the `yt-dlp` you have installed, with good
defaults. All yt-dlp options still work.

Fixed args: `--newline --no-playlist -o "%(title)s.%(ext)s"`. Later args win,
so `--yes-playlist` or your own `-o` override them.

## Usage

    maggot run media-download -- 'https://youtu.be/ID'                       # best video, into the current folder
    maggot run media-download -- -x --audio-format mp3 -P ~/Music 'URL'      # MP3
    maggot run media-download -- -S res:720 'URL'                            # up to 720p
    maggot run media-download -- --cookies-from-browser chrome 'URL'         # private / age-restricted

Without maggot, run `yt-dlp` with the same flags.

## Notes

- Audio modes and embedding need `ffmpeg`.
- Keep yt-dlp updated (`brew upgrade yt-dlp`); sites change often.
- Tests use a local file (`--enable-file-urls`), so they run offline.
