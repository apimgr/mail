package emailtemplates

import "embed"

// FS contains the built-in email templates embedded in the server binary.
//
//go:embed *.tmpl
var FS embed.FS
