package types

// Twin is one of two Component types kernel.TypeName renders alike, as
// ecs.Twin: this one is in package types, and its twin is in the external test
// package types_test, whose path also sits under bundles/ecs/internal. It is
// exported so the external test can register both. See readbyname_test.go.
type Twin struct{ Here int }
