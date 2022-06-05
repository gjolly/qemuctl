package commands

import (
	"fmt"
	"os"
	"os/exec"
)

// Run runs a command
func Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)

	env := os.Environ()
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Printf(">>> %v\n", cmd.String())
	err := cmd.Start()
	if err != nil {
		return err
	}

	err = cmd.Wait()
	return err
}
