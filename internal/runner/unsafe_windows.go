package runner

import "unsafe"

// The job object API takes a pointer and a size as plain integers, so the two
// conversions are isolated here rather than spread through the code that reads
// as ordinary process handling.

func unsafePointer[T any](v *T) unsafe.Pointer { return unsafe.Pointer(v) }

func unsafeSizeof[T any](v T) uintptr { return unsafe.Sizeof(v) }
