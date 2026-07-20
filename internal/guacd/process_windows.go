//go:build windows

package guacd

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

func terminateProcess(process *os.Process) error {
	// os.Process.Signal(os.Interrupt) is unsupported on Windows. A dedicated
	// process group lets console launchers such as wsl.exe receive Ctrl+Break
	// without interrupting Jianmen itself.
	return windows.GenerateConsoleCtrlEvent(
		windows.CTRL_BREAK_EVENT,
		uint32(process.Pid),
	)
}

func forceKillProcess(process *os.Process) error {
	return process.Kill()
}
