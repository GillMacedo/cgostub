// Package lmsensors reads hardware sensors through a C library.
package lmsensors

/*
static int sensor_count(void) { return 2; }
*/
import "C"

// Count returns the number of sensors.
func Count() int {
	return int(C.sensor_count())
}
