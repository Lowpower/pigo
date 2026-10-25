//go:build windows

package codemode

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func applyHardMemoryLimit() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
	// Commit charge, not virtual address space. Idle commit is far below this,
	// and a runaway allocation dies inside the child.
	info.ProcessMemoryLimit = 1 << 30
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		cerr := windows.CloseHandle(job)
		if cerr != nil {
			return fmt.Errorf("%w (also closing job: %v)", err, cerr)
		}
		return err
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		cerr := windows.CloseHandle(job)
		if cerr != nil {
			return fmt.Errorf("%w (also closing job: %v)", err, cerr)
		}
		return err
	}
	// The handle stays open so the limit remains in force for this process.
	return nil
}
