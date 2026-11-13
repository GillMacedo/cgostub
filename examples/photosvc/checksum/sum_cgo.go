package checksum

/*
#include <stddef.h>
#include <stdint.h>

static uint32_t fnv1a(const unsigned char *p, size_t n) {
	uint32_t h = 2166136261u;
	for (size_t i = 0; i < n; i++) {
		h ^= p[i];
		h *= 16777619u;
	}
	return h;
}
*/
import "C"

import "unsafe"

func sum(data []byte) uint32 {
	if len(data) == 0 {
		return uint32(C.fnv1a(nil, 0))
	}
	return uint32(C.fnv1a((*C.uchar)(unsafe.Pointer(&data[0])), C.size_t(len(data))))
}
