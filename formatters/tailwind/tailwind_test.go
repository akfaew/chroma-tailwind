package tailwind

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

func TestTailwindFormatterGitHubDark(t *testing.T) {
	light := styles.Get("github")
	dark := styles.Get("github-dark")
	if light == nil || dark == nil {
		t.Fatal("expected github and github-dark styles to exist")
	}

	formatter := New(WithDarkStyle(dark))
	it, err := lexers.Get("go").Tokenise(nil, "package main\nfunc main() {}\n")
	assert.NoError(t, err)

	var buf bytes.Buffer
	err = formatter.Format(&buf, light, it)
	assert.NoError(t, err)

	out := buf.String()

	lightKeyword := light.Get(chroma.Keyword).Colour
	darkKeyword := dark.Get(chroma.Keyword).Colour
	if !lightKeyword.IsSet() || !darkKeyword.IsSet() {
		t.Fatal("expected keyword colours to be set for both styles")
	}
	assert.Contains(t, out, fmt.Sprintf("text-[%s]", lightKeyword.String()))
	assert.Contains(t, out, fmt.Sprintf("dark:text-[%s]", darkKeyword.String()))

	lightBG := light.Get(chroma.Background).Background
	darkBG := dark.Get(chroma.Background).Background
	if !lightBG.IsSet() || !darkBG.IsSet() {
		t.Fatal("expected background colours to be set for both styles")
	}
	assert.Contains(t, out, fmt.Sprintf("bg-[%s]", lightBG.String()))
	assert.Contains(t, out, fmt.Sprintf("dark:bg-[%s]", darkBG.String()))
}
