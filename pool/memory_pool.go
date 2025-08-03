package pool

import (
	"bytes"
	"sync"
)

// BufferPool provides a pool of reusable byte buffers
var BufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// GetBuffer returns a buffer from the pool
func GetBuffer() *bytes.Buffer {
	return BufferPool.Get().(*bytes.Buffer)
}

// PutBuffer returns a buffer to the pool after resetting it
func PutBuffer(buf *bytes.Buffer) {
	buf.Reset()
	BufferPool.Put(buf)
}

// ByteSlicePool provides a pool of reusable byte slices
var ByteSlicePool = sync.Pool{
	New: func() interface{} {
		slice := make([]byte, 0, 1024) // Start with 1KB capacity
		return &slice
	},
}

// GetByteSlice returns a byte slice from the pool
func GetByteSlice() []byte {
	return *ByteSlicePool.Get().(*[]byte)
}

// PutByteSlice returns a byte slice to the pool
func PutByteSlice(b []byte) {
	b = b[:0] // Reset slice but keep capacity
	ByteSlicePool.Put(&b)
}
