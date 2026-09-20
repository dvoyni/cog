# Clip attribution

Stock Ogg Vorbis from Wikimedia Commons, downloaded 2026-09-20 for the #304
prototype. Every file is Ogg Vorbis as published; none has been re-encoded.

They were chosen for what they expose rather than for how they sound. Panning
holes and fold-back artefacts show up on broadband content with transients and
hide on synthetic tones, so the kit leads with a bell and footsteps rather than
a sine sweep.

| File | Source | Channels / rate | Why it is here |
|---|---|---|---|
| `bellsmall.ogg` | [Klang der Glocke des Schlosses Winsen](https://commons.wikimedia.org/wiki/File:Klang_der_Glocke_des_Schlosses_Winsen.ogg) | mono 44.1 kHz, 6.5 s | Sharp transient with a long decay. The panning-hole detector, and the one-shot. |
| `stirling.ogg` | [Stirling marble engine 000](https://commons.wikimedia.org/wiki/File:Stirling_marble_engine_000.ogg) | mono 44.1 kHz, 28.2 s | Sustained broadband machinery that loops. The distance and cone case. |
| `engine5cyl.ogg` | [5 cylinder engine sound](https://commons.wikimedia.org/wiki/File:5_cylinder_engine_sound.ogg) | mono **96 kHz**, 32.6 s | Harmonically rich, and its 96 kHz rate makes Prepare's resampler do real work. |
| `footsteps.ogg` | [Pasos en escalera de caracol](https://commons.wikimedia.org/wiki/File:Pasos_en_escalera_de_caracol.ogg) | **stereo** 48 kHz, 18.0 s | Transient-rich stereo: the "does stereo pan as W3C pans it, or should we mix down" check. |
| `pianoroll.ogg` | [Pleasant Moments Piano Roll](https://commons.wikimedia.org/wiki/File:Pleasant_Moments_Piano_Roll.ogg) | **stereo** 44.1 kHz, **176 s** | Music, and long enough to be #298's streaming tier rather than its resident one. |

Licences are each file's own on Commons (CC BY-SA / CC BY / public domain).
Check the linked page before reusing any of them outside this throwaway.
