//go:build !race

package keepass

// raceDetector reports whether the tests run with the race detector, which
// makes Argon2 about fifteen times slower.
const raceDetector = false
