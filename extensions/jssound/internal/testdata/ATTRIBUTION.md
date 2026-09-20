# Test clip attribution

`pianoroll.ogg` is the first 48704 sample frames — 1.104399 seconds, stereo
44.1 kHz — of
[Pleasant Moments Piano Roll](https://commons.wikimedia.org/wiki/File:Pleasant_Moments_Piano_Roll.ogg)
from Wikimedia Commons, which is **public domain**. It was cut at an Ogg page
boundary and that page was marked end-of-stream with its CRC recomputed; nothing
was re-encoded, so the stream is Ogg Vorbis exactly as published.

It is byte-identical to `extensions/nosound/internal/testdata/pianoroll.ogg` and
`extensions/otosound/internal/testdata/pianoroll.ogg`, and it is copied rather
than shared for the same reason the Loop Region parse is copied: an Extension's
`internal/` may not reach another Extension's, and what must not diverge is the
answer the three Adapters give about one Clip rather than the file they give it
about. The three loop-region suites assert the same durations against the same
bytes, which is exactly the parity that would be lost if each had its own clip.

The public-domain clip was chosen over the rest of the
[#304](https://github.com/dvoyni/cog/issues/304) prototype kit, which is
CC BY-SA, so that nothing in the engine's own tree carries a share-alike
obligation.
