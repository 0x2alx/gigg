package api

import (
	"sort"
	"strings"
	"sync/atomic"

	"github.com/danielgtaylor/huma/v2"
)

// A body field the operation doesn't take comes back from huma as a bare "unexpected property". With
// the operation's schema at hand the reply can say what was meant: each such item names the field
// it most likely stands for ("status_id": did you mean "status"?), and the detail lists the fields
// the body takes.

// schemaRegistry holds the API's schema registry for the error hook, which huma calls without the
// API; every instance registers the same schemas.
var schemaRegistry atomic.Pointer[huma.Registry]

func withHints(ctx huma.Context, p huma.StatusError) huma.StatusError {
	pr, ok := p.(*Problem)
	if !ok || ctx == nil || len(pr.Errors) == 0 {
		return p
	}
	op := ctx.Operation()
	reg := schemaRegistry.Load()
	if op == nil || op.RequestBody == nil || reg == nil {
		return p
	}
	fields := props(*reg, jsonSchema(op.RequestBody.Content))
	if len(fields) == 0 {
		return p
	}
	names := make([]string, 0, len(fields))
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)
	hinted := false
	for i, it := range pr.Errors {
		name, top := strings.CutPrefix(it.Location, "body.")
		if it.Message != "unexpected property" || !top || strings.ContainsAny(name, ".[") {
			continue
		}
		hinted = true
		if m := closest(name, names); m != "" {
			pr.Errors[i].Message = `unexpected property; did you mean "` + m + `"?`
		}
	}
	if hinted {
		pr.Detail += "; this body takes: " + strings.Join(names, ", ")
	}
	return pr
}

// closest is the accepted name a wrong one most likely meant: the same without or with an _id
// suffix, else one within two edits (the nearest; none when it is ambiguous).
func closest(name string, names []string) string {
	for _, n := range names {
		if n == strings.TrimSuffix(name, "_id") || n == strings.TrimSuffix(name, "s") || name == strings.TrimSuffix(n, "_id") {
			return n
		}
	}
	best, bestD, tie := "", 3, false
	for _, n := range names {
		d := editDistance(name, n)
		switch {
		case d < bestD:
			best, bestD, tie = n, d, false
		case d == bestD:
			tie = true
		}
	}
	if tie || len(name) < 4 {
		return ""
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := prev[j-1]
			if a[i-1] != b[j-1] {
				c++
			}
			cur[j] = min(c, prev[j]+1, cur[j-1]+1)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
