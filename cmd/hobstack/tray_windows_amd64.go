//go:build windows && amd64

package main

import "unsafe"

// Marimile structurilor din Windows SDK pe x64: daca layout-ul Go s-ar abate, build-ul pica aici
// (indexul constant iese din tablou) in loc sa strice memoria la rulare.
var (
	_ = [1]struct{}{}[unsafe.Sizeof(notifyIconData{})-976]
	_ = [1]struct{}{}[unsafe.Sizeof(wndClassEx{})-80]
	_ = [1]struct{}{}[unsafe.Sizeof(msg{})-48]
)
