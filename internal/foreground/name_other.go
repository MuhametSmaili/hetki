//go:build !darwin

package foreground

import "context"

// Native foreground-leader argv inspection is not implemented on these platforms.
func processName(context.Context, int) string { return "" }
