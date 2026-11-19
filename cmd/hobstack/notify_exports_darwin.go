//go:build darwin && cgo

package main

import "C"

// goNotifyClick: clic pe o notificare HobStack (Objective-C, firul principal).
//
//export goNotifyClick
func goNotifyClick(id *C.char) {
	notifyClicked(C.GoString(id))
}
