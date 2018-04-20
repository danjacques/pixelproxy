package web

import (
	"html/template"
	"strconv"
	"strings"
	"time"
	"unicode"

	"code.cloudfoundry.org/bytefmt"
)

var defaultTemplateFuncs = template.FuncMap{
	"timestr": func(t time.Time) string {
		return t.Format("02 Jan 06 15:04:05.000 MST")
	},
	"timenonce": func(t time.Time) string {
		return strconv.FormatInt(t.UnixNano(), 10)
	},
	"durationstr": func(d time.Duration) string {
		return d.Truncate(10 * time.Millisecond).String()
	},
	"bytefmt": func(v int64) string {
		return bytefmt.ByteSize(uint64(v))
	},
	"boolstr": func(v bool) string {
		if v {
			return "Yes"
		}
		return "No"
	},
	"maybeplural": func(v int64) string {
		if v == 1 {
			return ""
		}
		return "s"
	},
	"makeid": func(v string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				return r
			}
			return '-'
		}, v)
	},
}
