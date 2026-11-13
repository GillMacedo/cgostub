package heic

/*
static int count_pixels(int n) { return n * 4; }
*/
import "C"

func decode(data []byte) (Image, error) {
	return Image{Pixels: int(C.count_pixels(C.int(len(data))))}, nil
}
