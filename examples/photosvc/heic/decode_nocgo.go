//go:build !cgo

package heic

import "errors"

var errNoCgo = errors.New("heic: decoding needs cgo, and this binary was built with CGO_ENABLED=0")

func decode(data []byte) (Image, error) {
	return Image{}, errNoCgo
}
