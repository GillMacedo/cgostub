// Package heic decodes images. Decoding needs a C library, and the build
// without cgo gets a stub that returns an error.
package heic

// Image is a decoded image.
type Image struct {
	Pixels int
}

// Decode decodes data.
func Decode(data []byte) (Image, error) {
	return decode(data)
}
