package main

import (
	"io/fs"

	"github.com/benjamincottle/gymrouter/internal/webui"
)

// webFS returns the embedded frontend (nil if it wasn't built before compiling).
func webFS() fs.FS { return webui.FS() }
