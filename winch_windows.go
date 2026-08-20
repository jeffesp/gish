//go:build windows

package main

// watchWinch is a no-op on Windows, which has no window-resize signal;
// the terminal size captured at startup is used for the session.
func watchWinch(update func()) {}
