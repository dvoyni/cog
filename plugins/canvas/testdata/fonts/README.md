# Test fonts

Subsets of three Noto fonts, under the SIL Open Font License beside each, holding only the characters canvas's layout tests and benchmarks lay out:

- `notosansarabic.ttf`: Noto Sans Arabic from [google/fonts](https://github.com/google/fonts/tree/main/ofl/notosansarabic), at a weight of 400 and a width of 100, with Basic Latin, the no-break space and the Arabic block.
- `notosansjp.ttf`: Noto Sans JP from [google/fonts](https://github.com/google/fonts/tree/main/ofl/notosansjp), at a weight of 400, with Basic Latin, `、。〃「」`, a few hiragana and the ideographs `日本語文章次今天気良東京書読`, and its `kern`, `palt`, `liga`, `ccmp` and `locl` features.
- `notosanssc.ttf`: Noto Sans SC from [google/fonts](https://github.com/google/fonts/tree/main/ofl/notosanssc), at a weight of 400, with Basic Latin, `、。` and the ideographs `今日本语文章骨`, whose `今` is drawn otherwise than Noto Sans JP's, and its `kern`, `locl`, `ccmp` and `liga` features.
- `notosanssc-hanzi.ttf`: the same Noto Sans SC at a weight of 400, with Basic Latin and the first 256 ideographs, U+4E00 to U+4EFF, and no layout features, for the benchmarks of a Han glyph's distance field and of a frame of 200 new hanzi; its license is `notosanssc-OFL.txt`.

Each was instanced with fontTools' `varLib.instancer`, then cut with `fontTools.subset`.
