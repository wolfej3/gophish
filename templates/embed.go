// Package templates embeds the HTML templates for the admin interface.
package templates

import "embed"

// FS contains the admin interface's HTML templates.
//
//go:embed *.html
var FS embed.FS
