# Test clip attribution

`pianoroll.ogg` is the first 48704 sample frames — 1.104399 seconds, stereo
44.1 kHz — of
[Pleasant Moments Piano Roll](https://commons.wikimedia.org/wiki/File:Pleasant_Moments_Piano_Roll.ogg)
from Wikimedia Commons, which is **public domain**. It was cut at an Ogg page
boundary and that page was marked end-of-stream with its CRC recomputed; nothing
was re-encoded, so the stream is Ogg Vorbis exactly as published.

It is the same file `extensions/nosound/internal/testdata` holds, copied rather
than shared: a `_test.go` file may reach into another plugin's `internal/`, but
a fixture read by path cannot, and a test that reached across the tree for its
bytes would break the moment either package moved. Holding both copies is what
lets a test assert that the two Adapters report one Clip identically — same
duration, same channels, same rate — which is the property that keeps a game
tested under `nosound` behaving the same under `otosound`.

Here it is decoded rather than merely parsed: `otosound.Prepare` decodes the
whole Clip, converts it to the device rate and hands the Mixer real samples, so
a test can render a block and assert it is neither silent nor clipped. At
44.1 kHz against a 48 kHz device it also exercises the rate conversion, which a
Clip already at the device's rate would not.

The public-domain clip was chosen over the rest of the
[#304](https://github.com/dvoyni/cog/issues/304) prototype kit, which is
CC BY-SA, so that nothing in the engine's own tree carries a share-alike
obligation.
