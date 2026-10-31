# Test clip attribution

`pianoroll.ogg` is the first 48704 sample frames — 1.104399 seconds, stereo
44.1 kHz — of
[Pleasant Moments Piano Roll](https://commons.wikimedia.org/wiki/File:Pleasant_Moments_Piano_Roll.ogg)
from Wikimedia Commons, which is **public domain**. It was cut at an Ogg page
boundary and that page was marked end-of-stream with its CRC recomputed; nothing
was re-encoded, so the stream is Ogg Vorbis exactly as published.

It is here because `nosound.Prepare` reads real Ogg headers and a real stream
length, and a test of "a one-shot ends on the tick its duration says it should"
has nothing to assert without a real duration. The cut is what keeps it to 17 KB
and one second rather than 2.8 MB and three minutes: the header cost is flat
whatever the clip's length, so a shorter one costs a CI suite nothing and buys
it everything.

The public-domain clip was chosen over the rest of the
[#304](https://github.com/dvoyni/cog/issues/304) prototype kit, which is
CC BY-SA, so that nothing in the engine's own tree carries a share-alike
obligation.
