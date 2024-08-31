package utils

import (
	log "github.com/sirupsen/logrus"
	"io"
	"os/exec"
)

func RunCmd(cmd string, env []string, args ...string) error {
	command := exec.Command(cmd, args...)
	if env != nil {
		command.Env = env
	}
	err := runCmd(command)
	if err != nil {
		return err
	}
	return nil
}

func RunCmdInPath(cmdPath string, cmd string, env []string, args ...string) error {
	command := exec.Command(cmd, args...)
	if env != nil {
		command.Env = env
	}
	command.Dir = cmdPath
	err := runCmd(command)
	if err != nil {
		return err
	}
	return nil
}

func runCmd(command *exec.Cmd) error {
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	err = command.Start()
	if err != nil {
		return err
	}
	s := command.String()
	stderr, _ := io.ReadAll(stderrPipe)
	if len(stderr) > 0 {
		stderrStr := string(stderr)
		log.Info("Running: \"", s, "\" err: ", stderrStr)
	}
	stdout, _ := io.ReadAll(stdoutPipe)
	if len(stdout) > 0 {
		stdoutStr := string(stdout)
		log.Debug("Running: \"", s, "\" output: ", stdoutStr)
	}
	if err = command.Wait(); err != nil {
		log.Fatal(err)
	}
	return nil
}
