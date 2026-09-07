// Package safety holds umbra's safety rails, enforced in code rather than
// merely documented — the design DNA carried over from vm-mcp:
//   - name sanitization before any value reaches a shell / a path
//   - output caps so a runaway `docker logs` can't flood the agent
//   - a confirm-gate primitive for destructive operations
//
// umbra never stores secrets in the repo, so there is no secret registry here;
// the proxy's ACME account key and Caddy's data live under ~/.umbra, outside git.
package safety

import (
	"fmt"
	"regexp"
	"strings"
)

// OutputCapBytes is the default ceiling for any captured command output.
const OutputCapBytes = 10 * 1024

// CapOutput truncates text to cap bytes (UTF-8), appending a truncation notice.
func CapOutput(text string) string {
	return CapOutputTo(text, OutputCapBytes)
}

// CapOutputTo truncates text to cap bytes, appending a truncation notice.
func CapOutputTo(text string, cap int) string {
	b := []byte(text)
	if len(b) <= cap {
		return text
	}
	return string(b[:cap]) + fmt.Sprintf("\n… [output truncated at %d bytes]", cap)
}

// ClampLines clamps a requested log-line count into a sane bound. A value < 1
// (also the zero value, meaning "unset") yields the default of 200; the cap is 1000.
func ClampLines(lines int) int {
	const def, max = 200, 1000
	if lines < 1 {
		return def
	}
	if lines > max {
		return max
	}
	return lines
}

var illegalRun = regexp.MustCompile(`[^a-z0-9-]+`)
var edgeDashes = regexp.MustCompile(`^-+|-+$`)
var repeatedDashes = regexp.MustCompile(`-{2,}`)

// SanitizeName reduces an app name to the umbra character set: lowercase,
// digits and dashes only. This is the single choke point — every path, compose
// project name, network alias and Caddy route derives from the sanitized value,
// so a name can never break out of ~/.umbra/apps or inject into a shell.
//
// It returns an error (rather than silently mangling) when nothing usable
// remains, so the caller gets a clear message instead of an app called "".
func SanitizeName(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = illegalRun.ReplaceAllString(name, "-") // collapse runs of illegal chars to a dash
	name = edgeDashes.ReplaceAllString(name, "")  // trim leading/trailing dashes
	name = repeatedDashes.ReplaceAllString(name, "-")
	if name == "" {
		return "", fmt.Errorf("unusable app name %q: names must contain at least one letter or digit ([a-z0-9-])", raw)
	}
	if len(name) > 63 {
		return "", fmt.Errorf("app name %q too long: max 63 chars (DNS label limit)", name)
	}
	return name, nil
}

// MustSanitizeName is SanitizeName for paths where the name is already known
// good; it panics on an unusable name. Used only where a prior SanitizeName has
// validated the same input.
func MustSanitizeName(raw string) string {
	name, err := SanitizeName(raw)
	if err != nil {
		panic(err)
	}
	return name
}

// RE2 has no lookahead, so the length bound is checked separately from the shape.
var domainShape = regexp.MustCompile(`^([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

// ValidateDomain validates a hostname before it reaches the Caddyfile. It
// rejects anything that could inject a second directive or a shell metacharacter.
func ValidateDomain(domain string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(domain))
	if len(d) < 1 || len(d) > 253 || !domainShape.MatchString(d) {
		return "", fmt.Errorf("invalid domain %q: expected a fully-qualified hostname like app.example.com", domain)
	}
	return d, nil
}

// ValidatePort validates a TCP port.
func ValidatePort(port int) (int, error) {
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port %d: expected an integer 1–65535", port)
	}
	return port, nil
}

// ConfirmPreview is a tool/command result that asks the caller to retry with
// confirmation. Its JSON shape matches the TypeScript original exactly.
type ConfirmPreview struct {
	ConfirmRequired bool   `json:"confirmRequired"`
	Action          string `json:"action"`
	Preview         string `json:"preview"`
	Hint            string `json:"hint"`
}

// NewConfirmPreview builds a confirm-gate preview for a destructive action.
func NewConfirmPreview(action, preview string) ConfirmPreview {
	return ConfirmPreview{
		ConfirmRequired: true,
		Action:          action,
		Preview:         preview,
		Hint:            "Nothing was changed. Re-run with confirm: true to proceed.",
	}
}
