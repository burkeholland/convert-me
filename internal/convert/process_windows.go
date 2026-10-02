//go:build windows

package convert

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	createNoWindow           = 0x08000000
	belowNormalPriorityClass = 0x00004000
)

// configureProcess hides the console window and lowers the priority a little, so a long
// video conversion does not make the rest of the computer feel slow.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | belowNormalPriorityClass,
	}
}

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
)

// processJob returns a job object that ends every process in it when this application
// exits, including when it crashes. The handle is kept open on purpose for the whole run.
func processJob() windows.Handle {
	jobOnce.Do(func() {
		handle, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(handle, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(handle)
			return
		}
		jobHandle = handle
	})
	return jobHandle
}

// adoptProcess ties a started engine process to the application's lifetime. Failure is
// not fatal: the process is still stopped on cancel and on a normal exit.
func adoptProcess(cmd *exec.Cmd) {
	job := processJob()
	if job == 0 || cmd.Process == nil {
		return
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	_ = windows.AssignProcessToJobObject(job, handle)
}
