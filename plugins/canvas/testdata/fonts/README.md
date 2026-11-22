# Test fonts

Subsets of two Noto fonts, under the SIL Open Font License beside each, holding only the characters canvas's layout tests lay out:

- `notosansarabic.ttf`: Noto Sans Arabic from [google/fonts](https://github.com/google/fonts/tree/main/ofl/notosansarabic), at a weight of 400 and a width of 100, with Basic Latin, the no-break space and the Arabic block.
- `notosansjp.ttf`: Noto Sans JP from [google/fonts](https://github.com/google/fonts/tree/main/ofl/notosansjp), at a weight of 400, with Basic Latin, `、。〃「」`, a few hiragana and the ideographs `日本語文章次今天気良東京書読`, and its `kern`, `palt`, `liga`, `ccmp` and `locl` features.

Each was instanced with fontTools' `varLib.instancer`, then cut with `fontTools.subset`.
