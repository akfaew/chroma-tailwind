package tailwind

import (
	"fmt"
	"html"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
)

// Option sets an option of the Tailwind formatter.
type Option func(f *Formatter)

// Standalone configures the formatter for generating a standalone HTML document.
func Standalone(b bool) Option { return func(f *Formatter) { f.standalone = b } }

// ClassPrefix sets the Tailwind class prefix (eg. "tw-").
func ClassPrefix(prefix string) Option { return func(f *Formatter) { f.prefix = prefix } }

// WithDarkStyle sets the dark theme style used for dark mode variants.
func WithDarkStyle(style *chroma.Style) Option { return func(f *Formatter) { f.darkStyle = style } }

// TabWidth sets the number of characters for a tab. Defaults to 8.
func TabWidth(width int) Option { return func(f *Formatter) { f.tabWidth = width } }

// PreventSurroundingPre prevents the surrounding pre tags around the generated code.
func PreventSurroundingPre(b bool) Option {
	return func(f *Formatter) {
		f.preventSurroundingPre = b

		if b {
			f.preWrapper = nopPreWrapper
		} else {
			f.preWrapper = defaultPreWrapper
		}
	}
}

// InlineCode creates inline code wrapped in a code tag.
func InlineCode(b bool) Option {
	return func(f *Formatter) {
		f.inlineCode = b
		f.preWrapper = preWrapper{
			start: func(code bool, classAttr string) string {
				if code {
					return fmt.Sprintf(`<code%s>`, classAttr)
				}

				return ``
			},
			end: func(code bool) string {
				if code {
					return `</code>`
				}

				return ``
			},
		}
	}
}

// WithPreWrapper allows control of the surrounding pre tags.
func WithPreWrapper(wrapper PreWrapper) Option {
	return func(f *Formatter) {
		f.preWrapper = wrapper
	}
}

// WrapLongLines wraps long lines.
func WrapLongLines(b bool) Option {
	return func(f *Formatter) {
		f.wrapLongLines = b
	}
}

// WithLineNumbers formats output with line numbers.
func WithLineNumbers(b bool) Option {
	return func(f *Formatter) {
		f.lineNumbers = b
	}
}

// LineNumbersInTable will, when combined with WithLineNumbers, separate the line numbers
// and code in table td's, which make them copy-and-paste friendly.
func LineNumbersInTable(b bool) Option {
	return func(f *Formatter) {
		f.lineNumbersInTable = b
	}
}

// WithLinkableLineNumbers decorates the line numbers HTML elements with an "id"
// attribute so they can be linked.
func WithLinkableLineNumbers(b bool, prefix string) Option {
	return func(f *Formatter) {
		f.linkableLineNumbers = b
		f.lineNumbersIDPrefix = prefix
	}
}

// HighlightLines higlights the given line ranges with the Highlight style.
//
// A range is the beginning and ending of a range as 1-based line numbers, inclusive.
func HighlightLines(ranges [][2]int) Option {
	return func(f *Formatter) {
		f.highlightRanges = ranges
		sort.Sort(f.highlightRanges)
	}
}

// BaseLineNumber sets the initial number to start line numbering at. Defaults to 1.
func BaseLineNumber(n int) Option {
	return func(f *Formatter) {
		f.baseLineNumber = n
	}
}

// New Tailwind formatter.
func New(options ...Option) *Formatter {
	f := &Formatter{
		baseLineNumber: 1,
		preWrapper:     defaultPreWrapper,
	}
	f.classCache = newClassCache(f)
	for _, option := range options {
		option(f)
	}
	return f
}

// PreWrapper defines the operations supported in WithPreWrapper.
type PreWrapper interface {
	// Start is called to write a start <pre> element.
	// The code flag tells whether this block surrounds
	// highlighted code. This will be false when surrounding
	// line numbers.
	Start(code bool, classAttr string) string

	// End is called to write the end </pre> element.
	End(code bool) string
}

type preWrapper struct {
	start func(code bool, classAttr string) string
	end   func(code bool) string
}

func (p preWrapper) Start(code bool, classAttr string) string {
	return p.start(code, classAttr)
}

func (p preWrapper) End(code bool) string {
	return p.end(code)
}

var (
	nopPreWrapper = preWrapper{
		start: func(code bool, classAttr string) string { return "" },
		end:   func(code bool) string { return "" },
	}
	defaultPreWrapper = preWrapper{
		start: func(code bool, classAttr string) string {
			if code {
				return fmt.Sprintf(`<pre%s><code>`, classAttr)
			}

			return fmt.Sprintf(`<pre%s>`, classAttr)
		},
		end: func(code bool) string {
			if code {
				return `</code></pre>`
			}

			return `</pre>`
		},
	}
)

// Formatter that generates Tailwind HTML.
type Formatter struct {
	classCache            *classCache
	standalone            bool
	prefix                string
	darkStyle             *chroma.Style
	preWrapper            PreWrapper
	inlineCode            bool
	preventSurroundingPre bool
	tabWidth              int
	wrapLongLines         bool
	lineNumbers           bool
	lineNumbersInTable    bool
	linkableLineNumbers   bool
	lineNumbersIDPrefix   string
	highlightRanges       highlightRanges
	baseLineNumber        int
}

type highlightRanges [][2]int

func (h highlightRanges) Len() int           { return len(h) }
func (h highlightRanges) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h highlightRanges) Less(i, j int) bool { return h[i][0] < h[j][0] }

func (f *Formatter) Format(w io.Writer, style *chroma.Style, iterator chroma.Iterator) (err error) {
	return f.writeHTML(w, style, iterator.Tokens())
}

// We deliberately don't use html/template here because it is two orders of magnitude slower (benchmarked).
//
// OTOH we need to be super careful about correct escaping...
func (f *Formatter) writeHTML(w io.Writer, style *chroma.Style, tokens []chroma.Token) (err error) { // nolint: gocyclo
	classes := f.classCache.get(style, f.darkStyle)
	if f.standalone {
		fmt.Fprint(w, "<html>\n")
		fmt.Fprintf(w, "<body%s>\n", f.classAttr(classes, chroma.Background))
	}

	wrapInTable := f.lineNumbers && f.lineNumbersInTable

	lines := chroma.SplitTokensIntoLines(tokens)
	lineDigits := len(strconv.Itoa(f.baseLineNumber + len(lines) - 1))
	highlightIndex := 0

	if wrapInTable {
		// List line numbers in its own <td>
		fmt.Fprintf(w, "<div%s>\n", f.classAttr(classes, chroma.PreWrapper))
		fmt.Fprintf(w, "<table%s><tr>", f.classAttr(classes, chroma.LineTable))
		fmt.Fprintf(w, "<td%s>\n", f.classAttr(classes, chroma.LineTableTD))
		fmt.Fprintf(w, "%s", f.preWrapper.Start(false, f.classAttr(classes, chroma.PreWrapper)))
		for index := range lines {
			line := f.baseLineNumber + index
			highlight, next := f.shouldHighlight(highlightIndex, line)
			if next {
				highlightIndex++
			}
			if highlight {
				fmt.Fprintf(w, "<span%s>", f.classAttr(classes, chroma.LineHighlight))
			}

			fmt.Fprintf(w, "<span%s%s>%s\n</span>", f.classAttr(classes, chroma.LineNumbersTable), f.lineIDAttribute(line), f.lineTitleWithLinkIfNeeded(classes, lineDigits, line))

			if highlight {
				fmt.Fprintf(w, "</span>")
			}
		}
		fmt.Fprint(w, f.preWrapper.End(false))
		fmt.Fprint(w, "</td>\n")
		fmt.Fprintf(w, "<td%s>\n", f.classAttr(classes, chroma.LineTableTD, "w-full"))
	}

	fmt.Fprintf(w, "%s", f.preWrapper.Start(true, f.classAttr(classes, chroma.PreWrapper)))

	highlightIndex = 0
	for index, tokens := range lines {
		// 1-based line number.
		line := f.baseLineNumber + index
		highlight, next := f.shouldHighlight(highlightIndex, line)
		if next {
			highlightIndex++
		}

		if !(f.preventSurroundingPre || f.inlineCode) {
			// Start of Line
			fmt.Fprint(w, `<span`)

			if highlight {
				// Line + LineHighlight
				lineClasses := strings.TrimSpace(strings.Join([]string{
					classes[chroma.Line],
					classes[chroma.LineHighlight],
				}, " "))
				if lineClasses != "" {
					fmt.Fprintf(w, ` class="%s"`, lineClasses)
				}
				fmt.Fprint(w, `>`)
			} else {
				fmt.Fprintf(w, "%s>", f.classAttr(classes, chroma.Line))
			}

			// Line number
			if f.lineNumbers && !wrapInTable {
				fmt.Fprintf(w, "<span%s%s>%s</span>", f.classAttr(classes, chroma.LineNumbers), f.lineIDAttribute(line), f.lineTitleWithLinkIfNeeded(classes, lineDigits, line))
			}

			fmt.Fprintf(w, `<span%s>`, f.classAttr(classes, chroma.CodeLine))
		}

		for _, token := range tokens {
			html := html.EscapeString(token.String())
			attr := f.classAttr(classes, token.Type)
			if attr != "" {
				html = fmt.Sprintf("<span%s>%s</span>", attr, html)
			}
			fmt.Fprint(w, html)
		}

		if !(f.preventSurroundingPre || f.inlineCode) {
			fmt.Fprint(w, `</span>`) // End of CodeLine

			fmt.Fprint(w, `</span>`) // End of Line
		}
	}
	fmt.Fprintf(w, "%s", f.preWrapper.End(true))

	if wrapInTable {
		fmt.Fprint(w, "</td></tr></table>\n")
		fmt.Fprint(w, "</div>\n")
	}

	if f.standalone {
		fmt.Fprint(w, "\n</body>\n")
		fmt.Fprint(w, "</html>\n")
	}

	return nil
}

func (f *Formatter) lineIDAttribute(line int) string {
	if !f.linkableLineNumbers {
		return ""
	}
	return fmt.Sprintf(" id=\"%s\"", f.lineID(line))
}

func (f *Formatter) lineTitleWithLinkIfNeeded(classes map[chroma.TokenType]string, lineDigits, line int) string {
	title := fmt.Sprintf("%*d", lineDigits, line)
	if !f.linkableLineNumbers {
		return title
	}
	return fmt.Sprintf("<a%s href=\"#%s\">%s</a>", f.classAttr(classes, chroma.LineLink), f.lineID(line), title)
}

func (f *Formatter) lineID(line int) string {
	return fmt.Sprintf("%s%d", f.lineNumbersIDPrefix, line)
}

func (f *Formatter) shouldHighlight(highlightIndex, line int) (bool, bool) {
	next := false
	for highlightIndex < len(f.highlightRanges) && line > f.highlightRanges[highlightIndex][1] {
		highlightIndex++
		next = true
	}
	if highlightIndex < len(f.highlightRanges) {
		hrange := f.highlightRanges[highlightIndex]
		if line >= hrange[0] && line <= hrange[1] {
			return true, next
		}
	}
	return false, next
}

func (f *Formatter) classAttr(classes map[chroma.TokenType]string, tt chroma.TokenType, extraClasses ...string) string {
	parts := []string{}
	if cls := strings.TrimSpace(classes[tt]); cls != "" {
		parts = append(parts, cls)
	}
	if len(extraClasses) > 0 {
		for _, extra := range extraClasses {
			extra = strings.TrimSpace(extra)
			if extra == "" {
				continue
			}
			prefixed := f.prefixedClasses(strings.Fields(extra))
			if len(prefixed) > 0 {
				parts = append(parts, strings.Join(prefixed, " "))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf(` class="%s"`, strings.Join(parts, " "))
}

func (f *Formatter) tabWidthClass() string {
	if f.tabWidth != 0 && f.tabWidth != 8 {
		return fmt.Sprintf("[tab-size:%d]", f.tabWidth)
	}
	return ""
}

func (f *Formatter) baseClasses(tt chroma.TokenType) []string {
	switch tt {
	case chroma.PreWrapper:
		classes := []string{}
		if len(f.highlightRanges) > 0 {
			classes = append(classes, "grid")
		}
		if f.wrapLongLines {
			classes = append(classes, "whitespace-pre-wrap", "break-words")
		}
		return classes
	case chroma.Line:
		return []string{"flex"}
	case chroma.LineNumbers, chroma.LineNumbersTable:
		return []string{"whitespace-pre", "select-none", "mr-[0.4em]", "px-[0.4em]"}
	case chroma.LineTable:
		return []string{"border-separate", "border-spacing-0", "p-0", "m-0", "border-0"}
	case chroma.LineTableTD:
		return []string{"align-top", "p-0", "m-0", "border-0"}
	case chroma.LineLink:
		return []string{"outline-none", "no-underline", "text-[inherit]"}
	}
	return nil
}

func (f *Formatter) classes(light, dark *chroma.Style) map[chroma.TokenType]string {
	if dark == nil {
		dark = light
	}
	classes := map[chroma.TokenType]string{}
	bgLight := light.Get(chroma.Background)
	bgDark := dark.Get(chroma.Background)
	for t := range chroma.StandardTypes {
		lightEntry := light.Get(t)
		darkEntry := dark.Get(t)
		if t != chroma.Background {
			lightEntry = lightEntry.Sub(bgLight)
			darkEntry = darkEntry.Sub(bgDark)
		}

		lightValues := entryValuesFrom(lightEntry)
		darkValues := entryValuesFrom(darkEntry)

		parts := []string{}
		parts = append(parts, f.prefixedClasses(f.baseClasses(t))...)
		parts = append(parts, lightValues.classes(f.prefix)...)
		parts = append(parts, f.darkVariantClasses(lightValues, darkValues)...)
		classes[t] = strings.Join(parts, " ")
	}
	if tabClass := f.tabWidthClass(); tabClass != "" {
		classes[chroma.Background] = joinClasses(classes[chroma.Background], prefixClass(f.prefix, tabClass))
	}
	classes[chroma.PreWrapper] = joinClasses(classes[chroma.PreWrapper], classes[chroma.Background])
	return classes
}

type entryValues struct {
	text      string
	bg        string
	bold      bool
	italic    bool
	underline bool
}

func entryValuesFrom(entry chroma.StyleEntry) entryValues {
	out := entryValues{}
	if entry.Colour.IsSet() {
		out.text = "text-[" + entry.Colour.String() + "]"
	}
	if entry.Background.IsSet() {
		out.bg = "bg-[" + entry.Background.String() + "]"
	}
	if entry.Bold == chroma.Yes {
		out.bold = true
	}
	if entry.Italic == chroma.Yes {
		out.italic = true
	}
	if entry.Underline == chroma.Yes {
		out.underline = true
	}
	return out
}

func (e entryValues) classes(prefix string) []string {
	out := []string{}
	if e.text != "" {
		out = append(out, prefixClass(prefix, e.text))
	}
	if e.bg != "" {
		out = append(out, prefixClass(prefix, e.bg))
	}
	if e.bold {
		out = append(out, prefixClass(prefix, "font-bold"))
	}
	if e.italic {
		out = append(out, prefixClass(prefix, "italic"))
	}
	if e.underline {
		out = append(out, prefixClass(prefix, "underline"))
	}
	return out
}

func (f *Formatter) darkVariantClasses(light, dark entryValues) []string {
	out := []string{}
	if dark.text != "" {
		out = append(out, f.darkClass(dark.text))
	} else if light.text != "" {
		out = append(out, f.darkClass("text-[inherit]"))
	}
	if dark.bg != "" {
		out = append(out, f.darkClass(dark.bg))
	} else if light.bg != "" {
		out = append(out, f.darkClass("bg-transparent"))
	}
	if dark.bold {
		out = append(out, f.darkClass("font-bold"))
	} else if light.bold {
		out = append(out, f.darkClass("font-normal"))
	}
	if dark.italic {
		out = append(out, f.darkClass("italic"))
	} else if light.italic {
		out = append(out, f.darkClass("not-italic"))
	}
	if dark.underline {
		out = append(out, f.darkClass("underline"))
	} else if light.underline {
		out = append(out, f.darkClass("no-underline"))
	}
	return out
}

func (f *Formatter) darkClass(class string) string {
	return "dark:" + prefixClass(f.prefix, class)
}

func (f *Formatter) prefixedClasses(classes []string) []string {
	out := make([]string, 0, len(classes))
	for _, class := range classes {
		if class == "" {
			continue
		}
		out = append(out, prefixClass(f.prefix, class))
	}
	return out
}

func prefixClass(prefix, class string) string {
	if prefix == "" {
		return class
	}
	return prefix + class
}

func joinClasses(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + " " + b
}

const classCacheLimit = 32

type classCacheEntry struct {
	light *chroma.Style
	dark  *chroma.Style
	cache map[chroma.TokenType]string
}

type classCache struct {
	mu sync.Mutex
	// LRU cache of compiled styles. This is a slice
	// because the cache size is small, and a slice is sufficiently fast for
	// small N.
	cache []classCacheEntry
	f     *Formatter
}

func newClassCache(f *Formatter) *classCache {
	return &classCache{f: f}
}

func (c *classCache) get(light, dark *chroma.Style) map[chroma.TokenType]string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if dark == nil {
		dark = light
	}

	// Look for an existing entry.
	for i := len(c.cache) - 1; i >= 0; i-- {
		entry := c.cache[i]
		if entry.light == light && entry.dark == dark {
			// Top of the cache, no need to adjust the order.
			if i == len(c.cache)-1 {
				return entry.cache
			}
			// Move this entry to the end of the LRU
			copy(c.cache[i:], c.cache[i+1:])
			c.cache[len(c.cache)-1] = entry
			return entry.cache
		}
	}

	// No entry, create one.
	cached := c.f.classes(light, dark)

	// Evict the oldest entry.
	if len(c.cache) >= classCacheLimit {
		c.cache = c.cache[0:copy(c.cache, c.cache[1:])]
	}
	c.cache = append(c.cache, classCacheEntry{light: light, dark: dark, cache: cached})
	return cached
}
