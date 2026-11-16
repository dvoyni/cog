package m

import "iter"

func If[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func Any[T any](slice iter.Seq[T], pred func(T) bool) bool {
	for v := range slice {
		if pred(v) {
			return true
		}
	}
	return false
}

func Any2[K, V any](slice iter.Seq2[K, V], pred func(K, V) bool) bool {
	for k, v := range slice {
		if pred(k, v) {
			return true
		}
	}
	return false
}
