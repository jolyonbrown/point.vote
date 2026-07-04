// Package web holds the embedded browser UI — two static pages, one
// stylesheet, one script, llms.txt — served straight from the binary with
// no build step (PLAN.md §6). The UI consumes the same public API as curl;
// if curl can't do something, these pages can't either.
package web

import "embed"

//go:embed index.html room.html app.js style.css llms.txt openapi.yaml skill.md
var Files embed.FS
