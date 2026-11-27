package store

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// What the activity log records about a change: one rule for every object. An update records
// {field: {"old": …, "new": …}} for exactly the fields whose values differ between the object as read
// before the write and as read after it, in the same transaction; a create records every field of the
// new object, and a delete every field of the object as it was. Fields are the object's JSON fields, so a
// field added to a struct is audited without more code. Left out: bookkeeping (version, updated_at) and
// values looked up or counted from other rows (names, counts, next occurrences), listed per object below.

// auditSkipAll: fields no object's audit records.
var auditSkipAll = map[string]bool{"version": true, "updated_at": true}

// auditSkip: per object, fields that aren't its own settings.
var auditSkip = map[string][]string{
	"gig": {"key", "status_name", "status_category", "created_by_username", "created_by_admin", "cost_usd_total", "comment_count",
		"subgig_count", "subgig_done", "checklist_total", "checklist_done", "blocked_by", "blocked_by_subgigs", "parent_key", "schedule", "last_run",
		"question", "aliases"},
	"schedule":        {"uses", "errand_uses"},
	"template":        {"recurring"},
	"errand_template": {"command_set_by_username", "uses"},
	"recurrence": {"template_name", "template_created_by", "template_uses", "template_shared", "schedule_name", "schedule_uses",
		"schedule_shared", "next_at", "created_count", "skipped_count", "last_gig_id"},
	"errand": {"worker_username", "next_at", "run_now_at", "state", "fail_streak", "failing_since", "last_run", "template_uses",
		"template_shared", "schedule_name", "schedule_shared", "schedule_text"},
}

// auditMasked: per object, text fields that go through MaskSecrets before they are recorded (the log is
// append-only and kept for good, so a secret pasted into a command could never be taken out of it).
var auditMasked = map[string][]string{
	"errand":          {"command"},
	"errand_template": {"command"},
}

// auditFields returns v's audited fields as JSON values (raw, so numbers and nesting stay exact).
func auditFields(entity string, v any) map[string]json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	for k := range auditSkipAll {
		delete(m, k)
	}
	for _, k := range auditSkip[entity] {
		delete(m, k)
	}
	for _, k := range auditMasked[entity] {
		var s string
		if raw, ok := m[k]; ok && json.Unmarshal(raw, &s) == nil {
			m[k], _ = json.Marshal(MaskSecrets(s))
		}
	}
	return m
}

// AuditDiff returns {field: {"old", "new"}} for every audited field of an object that differs between
// before and after (both the same type, as read inside the write's transaction).
func AuditDiff(entity string, before, after any) map[string]any {
	ob, nb := auditFields(entity, before), auditFields(entity, after)
	out := map[string]any{}
	for k, nv := range nb {
		ov, ok := ob[k]
		if !ok {
			ov = json.RawMessage("null")
		}
		if !sameJSON(ov, nv) {
			out[k] = map[string]any{"old": ov, "new": nv}
		}
	}
	for k, ov := range ob {
		if _, ok := nb[k]; !ok && !sameJSON(ov, json.RawMessage("null")) {
			out[k] = map[string]any{"old": ov, "new": json.RawMessage("null")}
		}
	}
	return out
}

// AuditSnapshot returns every audited field of an object: what a create or a delete records. extra
// adds entries of its own (e.g. what a copy was made from); they win over a field of the same name.
func AuditSnapshot(entity string, v any, extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, raw := range auditFields(entity, v) {
		out[k] = raw
	}
	for k, x := range extra {
		out[k] = x
	}
	return out
}

// sameJSON: whether two JSON values are equal (object keys in any order; [] and null differ).
func sameJSON(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var x, y any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&x) != nil || db.Decode(&y) != nil {
		return false
	}
	xb, _ := json.Marshal(x) // maps marshal with sorted keys
	yb, _ := json.Marshal(y)
	return bytes.Equal(xb, yb)
}

const masked = "[masked]"

var (
	// NAME=value, NAME: value where the name says it holds a secret (FOO_TOKEN=…, "api_key": "…")
	secretAssign = regexp.MustCompile(`(?i)\b([a-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credentials?)["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s"';&|]+)`)
	// --password value, --token=value, -p'…' style flags
	secretFlag = regexp.MustCompile(`(?i)(--?(?:password|passwd|pass|secret|token|api-?key|access-?key)(?:=|\s+))("[^"]*"|'[^']*'|[^\s"';&|]+)`)
	// Authorization: Bearer …, Basic …, token …
	secretAuth = regexp.MustCompile(`(?i)(\b(?:bearer|basic)\s+|authorization:\s*(?:token\s+)?)([a-z0-9._~+/=-]{8,})`)
	// scheme://user:password@host
	secretURL = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://[^/\s:@]+:)([^@\s/]+)(@)`)
	// well-known token formats, wherever they appear
	secretKnown = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[abposr]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|glpat-[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{35})`)
)

// MaskSecrets replaces what looks like a secret in a command line (assignments and flags named like
// one, auth headers, passwords in URLs, well-known token formats) with [masked]. Variable references
// ($TOKEN, "${TOKEN}") are kept: they name a secret, they don't hold it.
func MaskSecrets(s string) string {
	keepRefs := func(re *regexp.Regexp, valueGroup int) {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			sub := re.FindStringSubmatch(m)
			if sub == nil {
				return masked // can't happen (m is a match); mask rather than keep
			}
			v := strings.Trim(sub[valueGroup], `"'`)
			if strings.HasPrefix(v, "$") || v == "" {
				return m
			}
			out := ""
			for i := 1; i < len(sub); i++ {
				if i == valueGroup {
					q := sub[i][:1] // a quoted value stays quoted
					if q != `"` && q != "'" {
						q = ""
					}
					out += q + masked + q
				} else {
					out += sub[i]
				}
			}
			return out // group 1 opens every pattern
		})
	}
	keepRefs(secretURL, 2)
	keepRefs(secretAssign, 2)
	keepRefs(secretFlag, 2)
	keepRefs(secretAuth, 2)
	return secretKnown.ReplaceAllString(s, masked)
}
