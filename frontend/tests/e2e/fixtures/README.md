# Browser test media

`collection-preview.mp4` is a two-second synthetic color test pattern used to verify actual video playback and pause-on-close in collection details. It contains no collected user media or audio. The checked-in fixture keeps browser tests independent of local media tools.

Generated with FFmpeg:

```sh
ffmpeg -f lavfi -i testsrc=size=160x120:rate=10 -t 2 -c:v libx264 -pix_fmt yuv420p -movflags +faststart collection-preview.mp4
```
