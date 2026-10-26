//go:build linux

package codemode

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func applyHardMemoryLimit() error {
	current, err := currentVirtualBytes()
	if err != nil {
		return err
	}
	want := current + memoryHeadroom
	var old unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_AS, &old); err != nil {
		return err
	}
	if old.Max != ^uint64(0) && want > old.Max {
		want = old.Max
	}
	lim := unix.Rlimit{Cur: want, Max: want}
	if old.Max != ^uint64(0) && old.Max < lim.Max {
		lim.Max = old.Max
	}
	if err := unix.Setrlimit(unix.RLIMIT_AS, &lim); err != nil {
		return fmt.Errorf("set address space limit: %w", err)
	}
	return nil
}

func currentVirtualBytes() (n uint64, err error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "VmSize:" {
			continue
		}
		kb, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			return 0, perr
		}
		return kb * 1024, nil
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("VmSize missing from /proc/self/status")
}
