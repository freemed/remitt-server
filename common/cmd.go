package common

import (
	"io"
	"log"
	"os/exec"
	"syscall"
	"time"
)

func RunWithTimeout(command []string, timeout int) (string, error) {
	// Set default
	if timeout == 0 {
		timeout = 30
	}

	var err error
	torun := command[0]
	args := command[1:]
	cmd := exec.Command(torun, args...)
	stdout, err := cmd.StdoutPipe()

	if err != nil {
		log.Print("RunWithTimeout(): " + err.Error())
		return "", err
	}
	err = cmd.Start()
	if err != nil {
		log.Print("RunWithTimeout(): " + err.Error())
		return "", err
	}
	timer := time.AfterFunc(time.Duration(timeout)*time.Second, func() {
		cmd.Process.Kill()
	})
	err = cmd.Wait()
	out, _ := io.ReadAll(stdout)
	log.Printf("RunWithTimeout(): Returned %d bytes", len(out))
	timer.Stop()

	// Only return error if it's an ExitError. I/O stuff we ignore for now.
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return string(out), err
		} else {
			log.Print("RunWithTimeout(): " + err.Error())
		}
	}

	return string(out), nil
}

func ExitCodeFromCommand(err error) (bool, int) {
	// If this isn't an ExitError, return false and no code
	if _, ok := err.(*exec.ExitError); !ok {
		return false, 0
	}
	if status, ok := err.(*exec.ExitError).Sys().(syscall.WaitStatus); ok {
		return true, status.ExitStatus()
	}
	return false, 0
}
