// Package assets contains the browser assets shipped with the application.
package assets

import "embed"

// Files is embedded so the server can run independently of its working directory.
//
//go:embed app.css app.js interests.js recommendations.js discussions.js live-activity.js live-pickers.js htmx.min.js google-g.png fonts js LICENSES.txt manifest.json icons/*.svg icons/*.png
var Files embed.FS
