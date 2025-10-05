// Package web embeds the MapLibre preview page.
package web

import "embed"

// FS holds index.html.
//
//go:embed index.html
var FS embed.FS
