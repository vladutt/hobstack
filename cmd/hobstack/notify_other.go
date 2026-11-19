//go:build !darwin && !windows && !linux

package main

// Pe alte sisteme nu exista notificari de sistem.
func sendNotification(title, body string, onClick func()) {}
