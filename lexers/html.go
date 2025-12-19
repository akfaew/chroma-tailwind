package lexers

import (
	"github.com/akfaew/chroma-tailwind/v2"
)

// HTML lexer.
var HTML = chroma.MustNewXMLLexer(embedded, "embedded/html.xml")
