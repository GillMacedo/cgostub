// Package checksum computes FNV-1a checksums, in C when cgo is available
// and in Go otherwise. Both give the same result.
package checksum

// Sum returns the 32-bit FNV-1a checksum of data.
func Sum(data []byte) uint32 {
	return sum(data)
}
