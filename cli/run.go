package run

import (
	"log"
	"os"
	"os/exec"
)

func runCommand(verbose bool, name string, args ...string) error {
	if verbose {
		log.Printf("Running command: %+v", append([]string{name}, args...))
	}
	// Create the command with the given arguments
	cmd := exec.Command(name, args...)
	// Set the command's output to be piped to the standard output
	cmd.Stdout = os.Stdout
	// Set the command's error output to be piped to the standard error
	cmd.Stderr = os.Stderr
	// Run the command
	return cmd.Run()
}
