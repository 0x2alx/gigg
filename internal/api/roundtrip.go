package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
)

// A PUT replaces a thing with the body it is sent, and the natural body to send is the one GET
// returned, edited. That body also carries what the server keeps for itself (id, version,
// created_at, use counts, names joined from other tables), which the PUT schema doesn't take, so
// huma would refuse it field by field. readOnlyEcho drops exactly those names: the ones the
// operation's 200 response has and its request body doesn't. Any other unknown field is still
// refused, so a misspelt one doesn't pass unnoticed.

type echoCache struct {
	mu    sync.Mutex
	names map[*huma.Operation]map[string]bool
}

// readOnlyEcho is the middleware; it runs for PUT only.
func (a *API) readOnlyEcho() func(huma.Context, func(huma.Context)) {
	c := &echoCache{names: map[*huma.Operation]map[string]bool{}}
	return func(ctx huma.Context, next func(huma.Context)) {
		op := ctx.Operation()
		if op == nil || op.Method != http.MethodPut || op.RequestBody == nil {
			next(ctx)
			return
		}
		drop := c.forOp(a.h.OpenAPI().Components.Schemas, op)
		if len(drop) == 0 {
			next(ctx)
			return
		}
		next(&echoContext{humaContext: ctx, drop: drop, limit: op.MaxBodyBytes})
	}
}

func (c *echoCache) forOp(reg huma.Registry, op *huma.Operation) map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d, ok := c.names[op]; ok {
		return d
	}
	in := props(reg, jsonSchema(op.RequestBody.Content))
	var out map[string]*huma.Schema
	if r := op.Responses["200"]; r != nil {
		out = props(reg, jsonSchema(r.Content))
	}
	drop := map[string]bool{}
	if in != nil {
		for name := range out {
			if _, ok := in[name]; !ok {
				drop[name] = true
			}
		}
	}
	c.names[op] = drop
	return drop
}

func jsonSchema(content map[string]*huma.MediaType) *huma.Schema {
	for ct, mt := range content {
		if mt != nil && strings.Contains(ct, "json") {
			return mt.Schema
		}
	}
	return nil
}

func props(reg huma.Registry, s *huma.Schema) map[string]*huma.Schema {
	if s != nil && s.Ref != "" {
		s = reg.SchemaFromRef(s.Ref)
	}
	if s == nil || s.Type != "object" {
		return nil
	}
	return s.Properties
}

// echoContext hands huma the body with the read-only names taken out. A body that isn't a JSON
// object, or is over the size limit, goes through untouched (huma then rejects it as before).
// (embedded under an alias: a field named Context would hide the interface's Context method)
type humaContext = huma.Context

type echoContext struct {
	humaContext
	drop  map[string]bool
	limit int64
}

func (e *echoContext) BodyReader() io.Reader {
	r := e.humaContext.BodyReader()
	if r == nil {
		return nil
	}
	// the server closes the request body; r stays open so an oversized one can still be streamed on
	limit := e.limit
	if limit <= 0 {
		limit = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return io.MultiReader(bytes.NewReader(raw), r)
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return bytes.NewReader(raw)
	}
	n := len(m)
	for k := range m {
		if e.drop[k] {
			delete(m, k)
		}
	}
	if len(m) == n {
		return bytes.NewReader(raw)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return bytes.NewReader(raw)
	}
	return bytes.NewReader(b)
}
