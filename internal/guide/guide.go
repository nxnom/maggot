// Package guide holds the tool authoring guide printed by `maggot guide`.
package guide

import _ "embed"

//go:embed guide.md
var Text string
