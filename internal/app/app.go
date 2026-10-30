// Package app holds the product identity shared by every command.
package app

// Product is the name shown to people and in the health body.
const Product = "formnet"

// Version is set at build time from the Makefile or build_app (-X ...app.Version=vX.Y.Z).
var Version = "dev"
