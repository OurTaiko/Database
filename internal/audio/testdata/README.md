# Synthetic audio fixtures

Copied from Fanmade's `internal/audio/testdata` for the `video` tests (audio that is not a movie). Generated locally with FFmpeg from a 0.5-second 440 Hz sine wave; no external song content.

- `cover.mp3`: a libmp3lame 44100 Hz stereo 128 kbit/s MP3, remuxed unchanged with a synthetic 16×16 red PNG as attached artwork.
- `vorbis.ogg`: native `vorbis` encoder, `-strict experimental`, stereo.

Base input: `-f lavfi -i sine=frequency=440:duration=0.5`.
