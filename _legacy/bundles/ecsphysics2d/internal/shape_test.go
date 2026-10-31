package internal

import (
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
)

func TestAShapeIsTheHundredAndFourBytesTheSpecificationLaysOut(t *testing.T) {
	if got, want := unsafe.Sizeof(Shape{}), uintptr(104); got != want {
		t.Fatalf("Shape is %d bytes, want %d", got, want)
	}
}

func TestTheShapeConstructorsStartInEveryCollisionGroup(t *testing.T) {
	circle := NewCircleShape(2, m.Vec2d{X: 1, Y: -1})
	if circle.Kind != ShapeCircle || circle.Radius != 2 || circle.Offset() != (m.Vec2d{X: 1, Y: -1}) {
		t.Fatalf("NewCircleShape built %+v", circle)
	}

	segment := NewSegmentShape(m.Vec2d{X: -1}, m.Vec2d{X: 1}, 0.5)
	if segment.Kind != ShapeSegment || segment.Radius != 0.5 ||
		segment.A() != (m.Vec2d{X: -1}) || segment.B() != (m.Vec2d{X: 1}) {
		t.Fatalf("NewSegmentShape built %+v", segment)
	}

	for name, shape := range map[string]Shape{"circle": circle, "segment": segment} {
		if shape.CollisionBits != CollisionBitsAll || shape.CollidesWith != CollisionBitsAll {
			t.Errorf("a %s was built in %#x and looking for %#x, want every group in both",
				name, shape.CollisionBits, shape.CollidesWith)
		}
	}

	// The hazard the spec accepts: a Shape written as a bare literal is in no
	// group and looks for none, so it collides with nothing.
	bare := Shape{}
	if collides(bare.CollisionBits, bare.CollidesWith, CollisionBitsAll, CollisionBitsAll) {
		t.Error("a bare Shape literal collided with something")
	}
}

func TestAPairCollidesOnlyWhenBothSidesAgree(t *testing.T) {
	const (
		walls       uint32 = 1 << 0
		projectiles uint32 = 1 << 1
	)

	if !collides(projectiles, walls, walls, projectiles) {
		t.Error("two sides that each look for the other did not collide")
	}
	if collides(projectiles, walls, walls, CollisionBitsNone) {
		t.Error("a wall looking for nothing still collided")
	}
	if collides(projectiles, CollisionBitsNone, walls, projectiles) {
		t.Error("a projectile looking for nothing still collided")
	}
	if collides(CollisionBitsNone, walls, walls, projectiles) {
		t.Error("a projectile in no group at all was still seen")
	}
}
