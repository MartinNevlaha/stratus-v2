# Vendored: mdview

- Upstream: https://github.com/xuanji86/claude-mdview (MIT, see `LICENSE`)
- Commit: `da646556714507acf5d41610605b9911eb9aa42a` (2026-10-03)
- Local changes (keep them when updating):
  - `hooks/register.tsx`: off macOS (`uname -s` is not `Darwin`) pictures are sized and converted with
    ImageMagick (`magick`, looked for once, or version 6's `identify`/`convert`) instead of `sips`, the
    BMP keeping transparency (`-type TrueColorAlpha bmp:`; ImageMagick 6 has no `bmp4:` coder), and files
    open with `xdg-open` instead of `open`, through `sh -c` with no output pipes, since the app it starts
    would otherwise hold them open until it quits
  - `hooks/mdview.test.ts`: the process mock answers `uname`, ImageMagick 6 or 7 and its output paths; four
    Linux tests
  - `.claude-plugin/plugin.json`: the `clickOpens*` descriptions no longer say macOS only

Usage and options are documented in the upstream README. To update, copy
`.claude-plugin/plugin.json`, `hooks/*` and `types/index.d.ts` from a newer
upstream commit, reapply the local changes above, and bump the commit.

On Linux, pictures need ImageMagick (`sudo apt install imagemagick`); without it
they show as links.
