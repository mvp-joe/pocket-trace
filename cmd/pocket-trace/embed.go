//go:build !dev

package main

import "embed"

//go:embed all:ui/dist
var uiFS embed.FS
