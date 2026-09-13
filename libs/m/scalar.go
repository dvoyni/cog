package m

import "math"

const (
	DegToRad = math.Pi / 180
	RadToDeg = 180 / math.Pi
)

func Clamp(value, minimum, maximum float32) float32 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func Clamp01(value float32) float32 {
	return Clamp(value, 0, 1)
}

func Lerp(from, to, amount float32) float32 {
	return from + (to-from)*amount
}

// NormalizeAngle maps a radian angle to [-Pi, Pi).
func NormalizeAngle(angle float32) float32 {
	const turn = 2 * math.Pi
	normalized := float32(math.Mod(float64(angle+math.Pi), turn))
	if normalized < 0 {
		normalized += turn
	}
	return normalized - math.Pi
}

// LerpAngle interpolates along the shortest radian arc without normalizing the result.
func LerpAngle(from, to, amount float32) float32 {
	return from + NormalizeAngle(to-from)*amount
}

func (v Vec2) Rotate(angle float32) Vec2 {
	sine, cosine := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	return Vec2{v.X*cosine - v.Y*sine, v.X*sine + v.Y*cosine}
}

func (v Vec2) MoveTowards(target Vec2, maximumDistance float32) Vec2 {
	delta := target.Sub(v)
	distance := delta.Length()
	if distance == 0 || (maximumDistance >= 0 && distance <= maximumDistance) {
		return target
	}
	return v.Add(delta.MulS(maximumDistance / distance))
}
