# Embedded clip attribution

`probe.ogg` is the three Vorbis header pages and the first audio page — 14912
sample frames, 0.338 seconds, stereo 44.1 kHz, 8813 bytes — of
[Pleasant Moments Piano Roll](https://commons.wikimedia.org/wiki/File:Pleasant_Moments_Piano_Roll.ogg)
from Wikimedia Commons, which is **public domain**. The kept page was marked
end-of-stream with its CRC recomputed; nothing was re-encoded, so the stream is
Ogg Vorbis exactly as published.

It ships inside the package rather than sitting in `testdata`, because it is not
a fixture: `jssound` decodes it once at startup to learn whether the browser can
decode Ogg Vorbis at all, and falls back to the wasm decoder when it cannot.
That is a question about the machine a game is running on, so the clip has to be
in the build.

It is cut to one audio page because the whole of it is header — the Vorbis
codebooks are most of those 8813 bytes, and a shorter clip would not be smaller.
A browser that decodes this decodes anything the same encoder wrote.

The public-domain clip was chosen over the rest of the
[#304](https://github.com/dvoyni/cog/issues/304) prototype kit, which is
CC BY-SA, so that nothing the engine ships carries a share-alike obligation.
