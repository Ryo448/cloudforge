//go:build windows

package processes

import "unsafe"

func unsafePointer[T any](v *T) unsafe.Pointer { return unsafe.Pointer(v) }
func unsafeSizeof[T any](v T) uintptr          { return unsafe.Sizeof(v) }
