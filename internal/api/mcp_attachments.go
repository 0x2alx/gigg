package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0x2alx/gigg/internal/access"
)

// MCP tools for attachments: list a gig's, attach a link, a commit or a short report, remove one.
// Files go through the REST upload (a runner's outbox); a report here is Markdown text kept as a .md
// file. Contents are never returned: an agent sees names, kinds and sizes.

const mcpReportMax = 256 << 10

type (
	attachArgs struct {
		Gig      string `json:"gig" jsonschema:"Gig id or key"`
		Kind     string `json:"kind" jsonschema:"link (an http(s) URL in value), commit (a commit id in value) or report (Markdown in text, kept as a .md file)"`
		Value    string `json:"value,omitempty" jsonschema:"link: the URL; commit: the commit id (7 to 64 hex digits)"`
		Text     string `json:"text,omitempty" jsonschema:"report: the Markdown, up to 256 KB"`
		Filename string `json:"filename,omitempty" jsonschema:"report: the file name (default report.md; .md is added when missing)"`
		Label    string `json:"label,omitempty" jsonschema:"A short caption (up to 200 characters)"`
		RunID    int64  `json:"run_id,omitempty" jsonschema:"The run it comes from, when you are a runner's agent"`
	}
	detachArgs struct {
		ID int64 `json:"id" jsonschema:"Attachment id (see list_attachments)"`
	}
)

func addAttachmentTools(s *mcp.Server, p *access.Principal, call apiCall) {
	t := url.PathEscape
	if !p.HasScope("gigs:read") {
		return
	}
	mcp.AddTool(s, &mcp.Tool{Name: "list_attachments", Description: "A gig's attachments (files, links, commits: ids, names, kinds, sizes; never the contents) and the workspace's limits. A file's bytes are at GET /attachments/{id}/file (Download attachments, attachments:read): save it to disk and search it there rather than reading it whole.", Annotations: ro("List attachments")},
		func(ctx context.Context, _ *mcp.CallToolRequest, in gigArg) (*mcp.CallToolResult, any, error) {
			return toolResult(call("GET", "/gigs/"+t(in.Gig)+"/attachments", nil, nil))
		})
	if !p.HasScope("attachments:write") || !p.AllowedAnywhere("add_attachments") {
		return
	}
	mcp.AddTool(s, &mcp.Tool{Name: "attach", Description: "Attach a link, a commit or a short Markdown report to a gig. Files (screenshots, logs, longer reports) go through the REST upload instead (POST /gigs/{id}/attachments).", Annotations: rw("Attach", false)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in attachArgs) (*mcp.CallToolResult, any, error) {
			switch in.Kind {
			case "link", "commit":
				b := map[string]any{"kind": in.Kind, "value": in.Value}
				setIf(b, "label", in.Label)
				if in.RunID != 0 {
					b["run_id"] = in.RunID
				}
				return toolResult(call("POST", "/gigs/"+t(in.Gig)+"/attachments/link", nil, b))
			case "report":
				if strings.TrimSpace(in.Text) == "" || len(in.Text) > mcpReportMax {
					return toolResult(422, []byte(`{"status":422,"code":"invalid","detail":"report: 1 character to 256 KB of Markdown in text"}`))
				}
				name := strings.TrimSpace(in.Filename)
				if name == "" {
					name = "report.md"
				}
				if l := strings.ToLower(name); !strings.HasSuffix(l, ".md") && !strings.HasSuffix(l, ".markdown") {
					name += ".md"
				}
				q := url.Values{"filename": {name}}
				if in.Label != "" {
					q.Set("label", in.Label)
				}
				if in.RunID != 0 {
					q.Set("run_id", strconv.FormatInt(in.RunID, 10))
				}
				return toolResult(call("POST", "/gigs/"+t(in.Gig)+"/attachments", q, rawBody{data: []byte(in.Text), ctype: "text/markdown"}))
			}
			return toolResult(422, []byte(`{"status":422,"code":"invalid","detail":"kind is link, commit or report"}`))
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_attachment", Description: "Remove an attachment: your own, or anyone's with Delete attachments.", Annotations: rw("Delete attachment", true)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in detachArgs) (*mcp.CallToolResult, any, error) {
			return toolResult(call("DELETE", fmt.Sprintf("/attachments/%d", in.ID), nil, nil))
		})
}
