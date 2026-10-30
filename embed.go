// Package formnet embeds the built web client into the binary.
package formnet

import (
	"embed"
	"io/fs"
)

//go:embed all:web/dist
var dist embed.FS

// Dist is the built web client, served at the root of the API server.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "web/dist")
	if err != nil {
		panic(err)
	}
	return sub
}
