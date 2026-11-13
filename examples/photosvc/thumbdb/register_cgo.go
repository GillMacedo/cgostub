package thumbdb

/*
static int version(void) { return 3; }
*/
import "C"

import "database/sql"

func init() {
	if C.version() >= 3 {
		sql.Register("thumbs", thumbsDriver{})
	}
}
