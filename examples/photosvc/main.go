// Command photosvc is a made-up service for the cgostub examples. Three of
// its packages behave differently when it is built with CGO_ENABLED=0.
package main

import (
	"database/sql"
	"fmt"
	"os"

	"example.com/photosvc/checksum"
	"example.com/photosvc/heic"
	_ "example.com/photosvc/thumbdb"
)

func main() {
	img, err := heic.Decode([]byte("demo image"))
	fmt.Printf("heic: %d pixels, err=%v\n", img.Pixels, err)
	fmt.Printf("checksum: %08x\n", checksum.Sum([]byte("demo image")))
	fmt.Printf("drivers: %v\n", sql.Drivers())
	if err != nil {
		os.Exit(1)
	}
}
