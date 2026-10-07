// Package assets contains the browser assets shipped with the application.
package assets

import "embed"

// Files is embedded so the server can run independently of its working directory.
//
//go:embed app.css app.js htmx.min.js google-g.png fonts js LICENSES.txt
var Files embed.FS
