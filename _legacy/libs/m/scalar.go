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

func sqrt(value float32) float32  { return float32(math.Sqrt(float64(value))) }
func round(value float32) float32 { return float32(math.Round(float64(value))) }
func floor(value float32) float32 { return float32(math.Floor(float64(value))) }
func ceil(value float32) float32  { return float32(math.Ceil(float64(value))) }

func abs32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
