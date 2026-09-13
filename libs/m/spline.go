package m

func BezierQuadratic(start, control, end Vec2, amount float32) Vec2 {
	inverse := 1 - amount
	return start.MulS(inverse * inverse).
		Add(control.MulS(2 * inverse * amount)).
		Add(end.MulS(amount * amount))
}

func CatmullRom(point0, point1, point2, point3 Vec2, amount float32) Vec2 {
	amount2 := amount * amount
	amount3 := amount2 * amount
	coefficient0 := -amount3 + 2*amount2 - amount
	coefficient1 := 3*amount3 - 5*amount2 + 2
	coefficient2 := -3*amount3 + 4*amount2 + amount
	coefficient3 := amount3 - amount2
	return point0.MulS(coefficient0).
		Add(point1.MulS(coefficient1)).
		Add(point2.MulS(coefficient2)).
		Add(point3.MulS(coefficient3)).
		MulS(0.5)
}
