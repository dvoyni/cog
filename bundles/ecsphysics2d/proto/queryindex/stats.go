package queryindex

// statTests counts shape tests when built with -tags protostats, and is
// compiled away otherwise.
var statTests uint64

func countTest() {
	if statsOn {
		statTests++
	}
}
