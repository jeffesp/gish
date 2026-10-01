//go:build !linux && !darwin

package main

import "errors"

// childStopped is unsupported here, so stopped commands are not detected.
func childStopped(pid int) (bool, error) {
	return false, errors.New("stop detection unsupported")
}
