//go:build !darwin

package selfupdate

// afterSwap: doar pe macOS binarul poate sta intr-un .app care trebuie semnat din nou.
func afterSwap(exe, version string) {}
