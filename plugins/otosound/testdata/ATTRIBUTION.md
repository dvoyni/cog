# Test clip attribution

`pianoroll.ogg` is the first 48704 sample frames — 1.104399 seconds, stereo
44.1 kHz — of
[Pleasant Moments Piano Roll](https://commons.wikimedia.org/wiki/File:Pleasant_Moments_Piano_Roll.ogg)
from Wikimedia Commons, which is **public domain**. It was cut at an Ogg page
boundary and that page was marked end-of-stream with its CRC recomputed; nothing
was re-encoded, so the stream is Ogg Vorbis exactly as published.

It is the same file sfx's headless backend holds in
`plugins/sfx/headless/testdata`, copied rather than shared: a fixture read by
path from another module's tree would break the moment either package moved.
Holding both copies is what lets a test assert that the two backends report one
clip identically, the same duration, channels and rate, which keeps a game
tested under the headless backend behaving the same under otosound.

Here it is decoded rather than merely parsed: otosound's `Prepare` decodes the
whole clip and keeps its samples, so a test can assert on real frames. At
44.1 kHz against a 48 kHz device it also exercises the rate conversion, which a
clip already at the device's rate would not.

The public-domain clip was chosen over the rest of the
[#304](https://github.com/dvoyni/cog/issues/304) prototype kit, which is
CC BY-SA, so that nothing in the engine's own tree carries a share-alike
obligation. It is v1's otosound fixture, carried over unchanged.
