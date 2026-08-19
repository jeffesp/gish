package main

import (
	"os"
	"strconv"
)

// lastExitCode holds the exit code of the most recently executed command.
var lastExitCode int

// recordExitCode stores the exit code of a finished command and exposes it
// to child processes as $GISH_LASTEXIT.
func recordExitCode(code int) {
	lastExitCode = code
	os.Setenv("GISH_LASTEXIT", strconv.Itoa(code))
}
