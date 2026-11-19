package canvas

import (
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

func TestASpritesSquareIsItsSizeAnAxisOfZeroKeepingItsImagesAspectAboutItsPivotInTexels(t *testing.T) {
	// An image of 8×4 texels, its pivot at its bottom right, 2 texels left.
	naturalSize := m.Vec2{X: 8, Y: 4}
	pivot := Pivot{Fraction: m.Vec2{X: 0.5, Y: 0.5}, Texels: m.Vec2{X: -2}}
	for _, sized := range []struct {
		name   string
		sprite Sprite
		want   m.Mat2x3
	}{
		// The pivot lies 6 texels from the left and 4 from the top.
		{"of no size", Sprite{}, m.Mat2x3{8, 0, 0, 4, -6, -4}},
		// 10 tall, so 20 wide, 2.5 units a texel.
		{"10 tall", Sprite{Size: m.Vec2{Y: 10}}, m.Mat2x3{20, 0, 0, 10, -15, -10}},
		{"16 wide", Sprite{Size: m.Vec2{X: 16}}, m.Mat2x3{16, 0, 0, 8, -12, -8}},
		{"stretched 4 by 4", Sprite{Size: m.Vec2{X: 4, Y: 4}}, m.Mat2x3{4, 0, 0, 4, -3, -4}},
		{"10 tall, moved", Sprite{Size: m.Vec2{Y: 10}, Transform: m.Translation2x3(100, 0)}, m.Mat2x3{20, 0, 0, 10, 85, -10}},
	} {
		square, size := sized.sprite.findSquare(naturalSize, pivot)
		if square != sized.want || size != (m.Vec2{X: sized.want[0], Y: sized.want[3]}) {
			t.Errorf("a sprite %s maps the unit square by %v, %v in its own space, want %v", sized.name, square, size, sized.want)
		}
	}
}

func TestASpritesBoundsAreThePartOfItsSquareItsCropLeaves(t *testing.T) {
	sprite := Sprite{Size: m.Vec2{X: 20, Y: 10}, Crop: Crop{Left: 0.25, Bottom: 0.5}, Transform: m.Translation2x3(100, 0)}

	got := sprite.findBounds(m.Vec2{X: 8, Y: 4}, Pivot{})

	if want := (m.Rect{X: 95, Y: -5, Width: 15, Height: 5}); got != want {
		t.Errorf("the cropped sprite's bounds are %v, want %v", got, want)
	}
}

func TestACropOfASidePastTheOneFacingItLeavesNothing(t *testing.T) {
	for _, cropped := range []struct {
		crop Crop
		want [4]float32
	}{
		{Crop{}, [4]float32{0, 0, 1, 1}},
		{Crop{Left: 0.25, Top: 0.5, Right: 0.25}, [4]float32{0.25, 0.5, 0.75, 1}},
		{Crop{Left: 0.75, Right: 0.5}, [4]float32{0.75, 0, 0.75, 1}},
		{Crop{Top: 0.5, Bottom: 0.75}, [4]float32{0, 0.5, 1, 0.5}},
	} {
		if got := cropped.crop.findRect(); got != cropped.want {
			t.Errorf("%+v leaves %v, want %v", cropped.crop, got, cropped.want)
		}
	}
}

func TestImageFromFileInternsEachPathAndOptionsIntoOneImage(t *testing.T) {
	water := ImageFromFile("interning/water.png")
	topLeft := ImageOptions{Pivot: PivotTopLeft}

	if again := ImageFromFile("interning/water.png"); again != water {
		t.Errorf("water.png interned as %v and then %v, want one image", water, again)
	}
	if zeroOptions := ImageFromFile("interning/water.png", ImageOptions{}); zeroOptions != water {
		t.Errorf("water.png of zero options is %v, want %v, the image of none", zeroOptions, water)
	}
	if pivoted := ImageFromFile("interning/water.png", topLeft); pivoted == water || pivoted != ImageFromFile("interning/water.png", topLeft) {
		t.Errorf("water.png pivoted at its top left is %v, want one image of its own, not %v", pivoted, water)
	}
	if other := ImageFromFile("interning/lava.png"); other == water || other == (Image{}) {
		t.Errorf("lava.png is %v, want an image of its own, not %v nor the zero one", other, water)
	}
	if key := imageNames.findKey(ImageFromFile("interning/water.png", topLeft)); key != (imageKey{path: "interning/water.png", options: topLeft}) {
		t.Errorf("water.png pivoted at its top left was interned from %+v", key)
	}
}
