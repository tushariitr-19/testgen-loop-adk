// Package classify is a demo target used by testgen-loop's verification
// workflow. The single exported function exposes several distinct branches so
// coverage progress is easy to observe iteration over iteration.
package classify

// Classify maps n to a coarse size bucket. It exists purely as a fixture for
// coverage-driven test generation; the buckets are arbitrary.
func Classify(n int) string {
	switch {
	case n < 0:
		return "negative"
	case n == 0:
		return "zero"
	case n < 10:
		return "small"
	case n < 100:
		return "medium"
	default:
		return "large"
	}
}
