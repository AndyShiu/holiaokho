// Package web embeds the built Web UI (web/dist). The React project lives
// in web/ and writes its build output to web/dist.
package web

import "embed"

//go:embed dist
var Dist embed.FS
