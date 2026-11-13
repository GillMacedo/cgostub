#!/bin/sh
# Runs cgostub on small programs that import real modules, and on real
# projects, and prints the output. It needs network to download them, so it
# isn't part of make test. Run it from the repository root.
set -eu
root=$(pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
go build -o "$work/cgostub" ./plugin/scripts

program() {
	name=$1
	shift
	mkdir -p "$work/$name"
	cd "$work/$name"
	go mod init "example.com/$name" > /dev/null 2>&1
	cat > main.go
	for mod in "$@"; do go get "$mod" > /dev/null 2>&1; done
	go mod tidy > /dev/null 2>&1
	cd "$root"
}

program sqlite3 github.com/mattn/go-sqlite3@v1.14.24 <<'GO'
package main

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, _ := sql.Open("sqlite3", ":memory:")
	fmt.Println(db.Ping())
}
GO

program sysinfo github.com/shirou/gopsutil/v3@v3.23.12 github.com/google/gopacket@v1.1.19 <<'GO'
package main

import (
	"fmt"

	"github.com/google/gopacket/pcap"
	"github.com/shirou/gopsutil/v3/cpu"
)

func main() {
	fmt.Println(cpu.Info())
	fmt.Println(pcap.Version())
}
GO

program purego modernc.org/sqlite@v1.34.4 <<'GO'
package main

import (
	"database/sql"
	"fmt"
	"net/http"

	_ "modernc.org/sqlite"
)

func main() {
	db, _ := sql.Open("sqlite", ":memory:")
	fmt.Println(db.Ping())
	http.ListenAndServe("127.0.0.1:0", nil)
}
GO

targets=linux/amd64,darwin/arm64,windows/amd64
for name in sqlite3 sysinfo purego; do
	echo "== $name"
	"$work/cgostub" --dir "$work/$name" --target "$targets" --fail-on never .
done

for repo in prometheus/node_exporter@v1.8.2 junegunn/fzf@v0.56.3 mikefarah/yq@v4.44.6; do
	path=${repo%@*}
	version=${repo#*@}
	git clone -q --depth 1 --branch "$version" "https://github.com/$path" "$work/$path"
	echo "== $repo"
	"$work/cgostub" --dir "$work/$path" --target "$targets" --fail-on never .
done
