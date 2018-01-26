// +build linux

package process

import "syscall"

func getSysProc() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGABRT}
}
