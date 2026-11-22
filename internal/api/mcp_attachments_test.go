package api

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPAttachments: attach a report, a link and a commit; list them (names, never contents);
// remove one. Without attachments:write only the list is offered.
func TestMCPAttachments(t *testing.T) {
	e := newEnv(t)
	open := func(c cred) (func(string, map[string]any) *mcp.CallToolResult, map[string]bool) {
		srv := e.mcpServer("internal")
		t.Cleanup(srv.Close)
		s, err := e.mcpSession(t, srv, authTransport{token: c.token})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		tools, err := s.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, tl := range tools.Tools {
			names[tl.Name] = true
		}
		return func(name string, args map[string]any) *mcp.CallToolResult {
			r, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return r
		}, names
	}
	gig := fmt.Sprint(e.ids["scout.gig"])

	_, names := open(e.key("scout")) // no attachments:write
	if !names["list_attachments"] || names["attach"] || names["delete_attachment"] {
		t.Fatalf("tools without the scope: %v", names)
	}
	call, names := open(attachKey(e, "scout"))
	if !names["attach"] || !names["delete_attachment"] {
		t.Fatalf("tools with the scope: %v", names)
	}
	r := call("attach", map[string]any{"gig": gig, "kind": "report", "text": "# Findings\n\nThe secret sauce.", "filename": "findings", "label": "Run report"})
	if r.IsError || !strings.Contains(text(r), `"filename":"findings.md"`) || !strings.Contains(text(r), `"mime":"text/markdown"`) {
		t.Fatalf("report: %s", text(r))
	}
	if r := call("attach", map[string]any{"gig": gig, "kind": "link", "value": "https://example.com/pr/9"}); r.IsError {
		t.Fatalf("link: %s", text(r))
	}
	if r := call("attach", map[string]any{"gig": gig, "kind": "commit", "value": "0123abcd"}); r.IsError {
		t.Fatalf("commit: %s", text(r))
	}
	if r := call("attach", map[string]any{"gig": gig, "kind": "report", "text": strings.Repeat("x", mcpReportMax+1)}); !r.IsError {
		t.Fatal("oversized report accepted")
	}
	if r := call("attach", map[string]any{"gig": gig, "kind": "file"}); !r.IsError {
		t.Fatal("kind file accepted")
	}
	var list struct {
		Items []struct {
			ID       int64  `json:"id"`
			Kind     string `json:"kind"`
			Filename string `json:"filename"`
		} `json:"items"`
	}
	r = call("list_attachments", map[string]any{"gig": gig})
	decode(t, r, &list)
	// the env's own link plus the three
	if len(list.Items) != 4 || strings.Contains(text(r), "secret sauce") {
		t.Fatalf("list: %s", text(r))
	}
	if r := call("delete_attachment", map[string]any{"id": list.Items[1].ID}); r.IsError {
		t.Fatalf("delete: %s", text(r))
	}
	// another workspace's attachments stay out of reach
	if r := call("delete_attachment", map[string]any{"id": e.ids["atlas.attachment"]}); !r.IsError {
		t.Fatal("deleted atlas's attachment")
	}
}
