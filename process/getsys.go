// +build !linux

package process

import "syscall"

func getSysProc() *syscall.SysProcAttr {
	return nil
}
