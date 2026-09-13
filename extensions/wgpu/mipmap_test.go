package wgpu

import "testing"

func TestMipLevelCount(t *testing.T) {
	cases := []struct{ w, h, want int }{
		{1, 1, 1}, {2, 2, 2}, {4, 4, 3}, {8, 8, 4},
		{6, 4, 3}, {5, 1, 3}, {1024, 1, 11},
	}
	for _, c := range cases {
		if got := mipLevelCount(c.w, c.h); got != c.want {
			t.Errorf("mipLevelCount(%d,%d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}
}

func TestDownsampleRGBAAveragesBox(t *testing.T) {
	src := []byte{
		0, 0, 0, 0, 40, 40, 40, 40,
		80, 80, 80, 80, 120, 120, 120, 120,
	}
	dst, dw, dh := downsampleRGBA(src, 2, 2)
	if dw != 1 || dh != 1 {
		t.Fatalf("downsample size = %dx%d, want 1x1", dw, dh)
	}
	if dst[0] != 60 {
		t.Fatalf("averaged texel = %d, want 60 = (0+40+80+120)/4", dst[0])
	}
}

func TestDownsampleRGBAOddClampsEdges(t *testing.T) {
	src := []byte{10, 10, 10, 10, 30, 30, 30, 30, 200, 200, 200, 200}
	dst, dw, dh := downsampleRGBA(src, 3, 1)
	if dw != 1 || dh != 1 {
		t.Fatalf("downsample size = %dx%d, want 1x1", dw, dh)
	}
	if dst[0] != 20 {
		t.Fatalf("odd downsample texel = %d, want 20 = (10+30)/2 with edge clamp", dst[0])
	}
}
