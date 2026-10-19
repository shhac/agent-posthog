// Package secret says what counts as a PostHog secret, for every place that
// keeps secrets out of output: printed records and --debug request logs.
package secret

import (
	"regexp"
	"slices"
	"strings"
)

// prefixes start PostHog personal, project, share, app, and recording keys.
var prefixes = []string{"phx_", "phc_", "phs_", "pha_", "phr_"}

// fields are names whose values are secrets whatever they look like.
var fields = []string{"access_token", "api_key", "personal_api_key", "project_token", "secret", "token"}

// IsField reports a field name whose value is a secret.
func IsField(name string) bool {
	return slices.Contains(fields, name)
}

// IsKey reports a value that is a PostHog key.
func IsKey(value string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(value, prefix) })
}

// inURL matches PostHog keys anywhere in a URL, even URL-encoded inside
// another parameter, and the values of query parameters named like secrets.
var inURL = regexp.MustCompile(`(` + alternation(prefixes) + `)[A-Za-z0-9_-]+|((?:^|[?&])(?:` + alternation(fields) + `)=)[^&]*`)

// RedactURL keeps the prefix or parameter name and masks the secret after it.
func RedactURL(value string) string {
	return inURL.ReplaceAllStringFunc(value, func(match string) string {
		m := inURL.FindStringSubmatch(match)
		if m[1] != "" {
			return m[1] + "..."
		}
		return m[2] + "..."
	})
}

func alternation(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = regexp.QuoteMeta(w)
	}
	return strings.Join(quoted, "|")
}
