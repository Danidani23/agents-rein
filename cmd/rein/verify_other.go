//go:build app && !darwin

package main

// verifyOwner is nil off macOS: a Confirm in the window counts by itself
// there, until Windows Hello stands behind it (P6.8 b).
var verifyOwner func(reason string) error
