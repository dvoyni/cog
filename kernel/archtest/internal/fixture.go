// Package internal holds the fixture types kernel.TypeName's table test renders:
// named types declared in a package whose import path ends in internal, which is
// the shape every Bundle's and Port's internal/ package has. It declares nothing
// else and nothing outside kernel/archtest can import it.
package internal

// Font is a plain named type declared in an internal package.
type Font struct{}

// Frame is a second one, so a composite can hold two different internal types.
type Frame struct{}

// Box is a generic declared in an internal package, so a type argument and the
// type it instantiates can both need the rule.
type Box[T any] struct{ value T }
