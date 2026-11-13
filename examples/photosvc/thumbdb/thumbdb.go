// Package thumbdb registers a database/sql driver named "thumbs". The
// driver is registered from a cgo file, so a build without cgo has no
// driver and sql.Open("thumbs", ...) fails at run time.
package thumbdb

import (
	"database/sql/driver"
	"errors"
)

type thumbsDriver struct{}

func (thumbsDriver) Open(name string) (driver.Conn, error) {
	return nil, errors.New("thumbdb: this example driver has no connections")
}
