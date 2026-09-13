package m

type Rect struct{ X, Y, Width, Height float32 }
type Recti struct{ X, Y, Width, Height int }

func (rect Rect) Normalize() Rect {
	if rect.Width < 0 {
		rect.X += rect.Width
		rect.Width = -rect.Width
	}
	if rect.Height < 0 {
		rect.Y += rect.Height
		rect.Height = -rect.Height
	}
	return rect
}

func (rect Rect) Min() Vec2 { rect = rect.Normalize(); return Vec2{rect.X, rect.Y} }
func (rect Rect) Max() Vec2 {
	rect = rect.Normalize()
	return Vec2{rect.X + rect.Width, rect.Y + rect.Height}
}
func (rect Rect) Size() Vec2 { rect = rect.Normalize(); return Vec2{rect.Width, rect.Height} }
func (rect Rect) Center() Vec2 {
	minimum, maximum := rect.Min(), rect.Max()
	return minimum.Lerp(maximum, 0.5)
}
func (rect Rect) Contains(point Vec2) bool {
	minimum, maximum := rect.Min(), rect.Max()
	return point.X >= minimum.X && point.X <= maximum.X && point.Y >= minimum.Y && point.Y <= maximum.Y
}
func (rect Rect) Intersects(other Rect) bool { _, ok := rect.Intersection(other); return ok }
func (rect Rect) Translate(offset Vec2) Rect {
	return Rect{rect.X + offset.X, rect.Y + offset.Y, rect.Width, rect.Height}
}
func (rect Rect) Inset(amount Vec2) Rect {
	return Rect{rect.X + amount.X, rect.Y + amount.Y, rect.Width - 2*amount.X, rect.Height - 2*amount.Y}
}
func (rect Rect) Int() Recti {
	return Recti{int(rect.X), int(rect.Y), int(rect.Width), int(rect.Height)}
}

func (rect Rect) Intersection(other Rect) (Rect, bool) {
	minimum := rect.Min()
	maximum := rect.Max()
	otherMinimum := other.Min()
	otherMaximum := other.Max()
	left := max(minimum.X, otherMinimum.X)
	top := max(minimum.Y, otherMinimum.Y)
	right := min(maximum.X, otherMaximum.X)
	bottom := min(maximum.Y, otherMaximum.Y)
	if right < left || bottom < top {
		return Rect{}, false
	}
	return Rect{left, top, right - left, bottom - top}, true
}

func (rect Recti) Normalize() Recti {
	if rect.Width < 0 {
		rect.X += rect.Width
		rect.Width = -rect.Width
	}
	if rect.Height < 0 {
		rect.Y += rect.Height
		rect.Height = -rect.Height
	}
	return rect
}

func (rect Recti) Min() Vec2i { rect = rect.Normalize(); return Vec2i{rect.X, rect.Y} }
func (rect Recti) Max() Vec2i {
	rect = rect.Normalize()
	return Vec2i{rect.X + rect.Width, rect.Y + rect.Height}
}
func (rect Recti) Size() Vec2i { rect = rect.Normalize(); return Vec2i{rect.Width, rect.Height} }
func (rect Recti) Center() Vec2 {
	minimum, maximum := rect.Min().Float(), rect.Max().Float()
	return minimum.Lerp(maximum, 0.5)
}
func (rect Recti) Contains(point Vec2i) bool {
	minimum, maximum := rect.Min(), rect.Max()
	return point.X >= minimum.X && point.X <= maximum.X && point.Y >= minimum.Y && point.Y <= maximum.Y
}
func (rect Recti) Intersects(other Recti) bool { _, ok := rect.Intersection(other); return ok }
func (rect Recti) Translate(offset Vec2i) Recti {
	return Recti{rect.X + offset.X, rect.Y + offset.Y, rect.Width, rect.Height}
}
func (rect Recti) Inset(amount Vec2i) Recti {
	return Recti{rect.X + amount.X, rect.Y + amount.Y, rect.Width - 2*amount.X, rect.Height - 2*amount.Y}
}
func (rect Recti) Float() Rect {
	return Rect{float32(rect.X), float32(rect.Y), float32(rect.Width), float32(rect.Height)}
}

func (rect Recti) Intersection(other Recti) (Recti, bool) {
	minimum := rect.Min()
	maximum := rect.Max()
	otherMinimum := other.Min()
	otherMaximum := other.Max()
	left := max(minimum.X, otherMinimum.X)
	top := max(minimum.Y, otherMinimum.Y)
	right := min(maximum.X, otherMaximum.X)
	bottom := min(maximum.Y, otherMaximum.Y)
	if right < left || bottom < top {
		return Recti{}, false
	}
	return Recti{left, top, right - left, bottom - top}, true
}

func FitInside(source, destination Rect, minimumScale, maximumScale float32) (Vec2, float32) {
	return fit(source, destination, false, minimumScale, maximumScale)
}

func FitCover(source, destination Rect, minimumScale, maximumScale float32) (Vec2, float32) {
	return fit(source, destination, true, minimumScale, maximumScale)
}

func fit(source, destination Rect, cover bool, minimumScale, maximumScale float32) (Vec2, float32) {
	scaleX := destination.Width / source.Width
	scaleY := destination.Height / source.Height
	scale := min(scaleX, scaleY)
	if cover {
		scale = max(scaleX, scaleY)
	}
	if minimumScale > 0 && scale < minimumScale {
		scale = minimumScale
	}
	if maximumScale > 0 && scale > maximumScale {
		scale = maximumScale
	}
	return Vec2{
		X: destination.X + (destination.Width-source.Width*scale)/2 - source.X*scale,
		Y: destination.Y + (destination.Height-source.Height*scale)/2 - source.Y*scale,
	}, scale
}

func PointInPolygon(point Vec2, polygon []Vec2) bool {
	inside := false
	previous := len(polygon) - 1
	for current := 0; current < len(polygon); current++ {
		if (polygon[current].Y > point.Y) != (polygon[previous].Y > point.Y) &&
			point.X < (polygon[previous].X-polygon[current].X)*(point.Y-polygon[current].Y)/(polygon[previous].Y-polygon[current].Y)+polygon[current].X {
			inside = !inside
		}
		previous = current
	}
	return inside
}
