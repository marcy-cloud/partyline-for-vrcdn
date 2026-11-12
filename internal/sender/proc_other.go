//go:build !windows

package sender

import "os/exec"

func hideWindow(*exec.Cmd) {}
