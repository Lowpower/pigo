package codemode

import "runtime/debug"

// softHeapLimit matches pi v1.3.0's QuickJS heap cap. It makes the garbage
// collector aggressive. It does not by itself abort the process.
const softHeapLimit int64 = 256 << 20

// memoryHeadroom is how far a script may grow the address space past the
// mappings already in place when the worker starts. A normal script adds a
// few hundred kilobytes. An allocation loop hits this cap and the process ends.
const memoryHeadroom uint64 = 256 << 20

func applyMemoryLimits() error {
	debug.SetMemoryLimit(softHeapLimit)
	return applyHardMemoryLimit()
}
