package configtx

import (
	"os/exec"
	"syscall"
)

// runCmd executes an absolute binary with args. No shell involved.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(syscall.Getuid()),
			Gid: uint32(syscall.Getgid()),
		},
	}
	return cmd.Run()
}
