// svgworker serves SVG parse and rasterize requests from ntsvg.WorkerRendererFactory.
// It is launched by ntmediabox, not run interactively.
package main

import (
	"fmt"
	"os"

	"nimbleterminal.dev/ntmediabox"
	"nimbleterminal.dev/ntsvg"
)

func main() {
	conn := ntmediabox.WorkerConn()
	if err := ntsvg.ServeWorker(conn, ntsvg.Limits{}); err != nil {
		_ = conn.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = conn.Close()
}
