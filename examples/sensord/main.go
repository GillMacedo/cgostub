// Command sensord is a made-up daemon for the cgostub examples. Its
// lmsensors package has only cgo files, so it can't build without cgo.
package main

import (
	"fmt"

	"example.com/sensord/lmsensors"
)

func main() {
	fmt.Printf("sensors: %d\n", lmsensors.Count())
}
