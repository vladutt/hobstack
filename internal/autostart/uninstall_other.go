//go:build !windows

package autostart

// SetInstalledVersion: doar installerul de Windows are o intrare in lista de programe.
func SetInstalledVersion(string) {}
