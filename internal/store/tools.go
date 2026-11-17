package store

import (
	"regexp"
	"strings"
)

// MaxToolRules and MaxToolRule bound a gig's tool allowlist.
const (
	MaxToolRules = 64
	MaxToolRule  = 500
)

// toolRuleRe: a Claude Code permission rule: a tool name (Read, Bash, WebFetch, mcp__server__tool), optionally
// with a specifier in parentheses (Bash(git status *), Edit(//home/me/notes/**), WebFetch(domain:example.com)).
// No control characters anywhere; a rule can't start with "-", so it never reads as a flag. The runner
// joins the rules with commas into one argument (--allowedTools=a,b); claude splits
// that on commas and spaces outside parentheses, tracking one level. So commas and spaces are fine
// inside the specifier (Bash(curl "x?a=1,2")), but not outside it, and no parentheses inside it.
var toolRuleRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(\([^()\x00-\x1f\x7f]+\))?$`)

// NormTools cleans a tool allowlist: trims each rule, drops empty and repeated ones (order kept) and
// refuses anything that isn't one plain rule (see toolRuleRe).
func NormTools(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		if len(r) > MaxToolRule {
			return nil, Invalid("allowed_tools: a rule has at most %d characters", MaxToolRule)
		}
		if !toolRuleRe.MatchString(r) {
			return nil, Invalid("allowed_tools: %q isn't one permission rule (Tool or Tool(specifier), e.g. Read, Bash(git status *); one rule per entry, no parentheses inside the specifier)", r)
		}
		seen[r] = true
		out = append(out, r)
	}
	if len(out) > MaxToolRules {
		return nil, Invalid("allowed_tools: at most %d rules", MaxToolRules)
	}
	return out, nil
}

// SameTools reports equal allowlists (order matters: it is what the runner passes).
func SameTools(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
