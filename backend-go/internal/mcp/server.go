// Package mcp exposes easy-host's content-hosting operations over the Model
// Context Protocol.
//
// It mounts a single Streamable HTTP endpoint (/mcp) backed by the official Go
// MCP SDK. Every request is authenticated with an OAuth 2.1 bearer token minted
// by the MCP authorization server (see internal/service/mcpoauth.go); the owner
// username carried by that token is threaded into the content service, which
// enforces per-owner data isolation.
package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/oglimmer/easy-host/internal/service"
)

// Server builds and serves the MCP endpoint.
type Server struct {
	contentSvc *service.ContentService
	oauthSvc   *service.MCPOAuthService
}

func NewServer(contentSvc *service.ContentService, oauthSvc *service.MCPOAuthService) *Server {
	return &Server{contentSvc: contentSvc, oauthSvc: oauthSvc}
}

var errNoUser = errors.New("no authenticated user in request")

// Handler returns the bearer-protected Streamable HTTP handler to mount at /mcp.
func (s *Server) Handler() http.Handler {
	server := sdk.NewServer(&sdk.Implementation{Name: "easy-host", Version: "1.0.0"}, nil)
	s.registerTools(server)

	streamHandler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server {
		return server
	}, nil)

	requireToken := auth.RequireBearerToken(s.verifyToken, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: s.oauthSvc.ProtectedResourceMetadataURL(),
		Scopes:              []string{service.MCPScope},
	})
	return requireToken(streamHandler)
}

// verifyToken validates the MCP access token and surfaces the owner username to
// tool handlers via TokenInfo.Extra.
func (s *Server) verifyToken(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	vt, err := s.oauthSvc.VerifyAccessToken(token)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}
	return &auth.TokenInfo{
		UserID:     vt.Username,
		Scopes:     vt.Scopes,
		Expiration: vt.ExpiresAt,
		Extra:      map[string]any{"username": vt.Username},
	}, nil
}

// owner extracts the authenticated owner username placed on the request by
// verifyToken.
func owner(req *sdk.CallToolRequest) (string, error) {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return "", errNoUser
	}
	if name, ok := req.Extra.TokenInfo.Extra["username"].(string); ok && name != "" {
		return name, nil
	}
	return "", errNoUser
}

// ---- tool definitions ----------------------------------------------------

type getContentInput struct {
	Slug string `json:"slug" jsonschema:"The slug of the content to fetch."`
}

type deleteContentInput struct {
	Slug string `json:"slug" jsonschema:"The slug of the content to delete."`
}

type createContentInput struct {
	Slug                   string `json:"slug" jsonschema:"URL slug for the content. Must match ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ and be unique."`
	Title                  string `json:"title,omitempty" jsonschema:"Human-readable title. Defaults to the slug when omitted."`
	SourceURL              string `json:"source_url,omitempty" jsonschema:"Optional source/attribution URL."`
	Creator                string `json:"creator,omitempty" jsonschema:"Optional creator name. Defaults to the owner when omitted."`
	AllowExternalResources bool   `json:"allow_external_resources,omitempty" jsonschema:"Whether the page may load external (cross-origin) resources."`
	HTML                   string `json:"html,omitempty" jsonschema:"Raw HTML stored as index.html. Provide either html or zip_base64."`
	ZipBase64              string `json:"zip_base64,omitempty" jsonschema:"Base64-encoded ZIP archive of a multi-file site, extracted preserving structure. Provide either html or zip_base64."`
}

type updateContentInput struct {
	Slug                   string  `json:"slug" jsonschema:"The slug of the content to update."`
	Title                  *string `json:"title,omitempty" jsonschema:"New title. Omit to leave unchanged."`
	SourceURL              *string `json:"source_url,omitempty" jsonschema:"New source URL. Omit to leave unchanged."`
	Creator                *string `json:"creator,omitempty" jsonschema:"New creator. Omit to leave unchanged."`
	AllowExternalResources *bool   `json:"allow_external_resources,omitempty" jsonschema:"New external-resources flag. Omit to leave unchanged."`
	HTML                   string  `json:"html,omitempty" jsonschema:"Replacement HTML stored as index.html. Omit to leave files unchanged."`
	ZipBase64              string  `json:"zip_base64,omitempty" jsonschema:"Replacement site as a base64-encoded ZIP archive. Omit to leave files unchanged."`
}

func (s *Server) registerTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{
		Name:        "list_content",
		Description: "List the authenticated owner's hosted content entries.",
	}, s.listContent)

	sdk.AddTool(server, &sdk.Tool{
		Name:        "get_content",
		Description: "Fetch a single one of the authenticated owner's content entries by slug, including its file list.",
	}, s.getContent)

	sdk.AddTool(server, &sdk.Tool{
		Name:        "create_content",
		Description: "Create a new content entry served at /s/{slug}. Provide the site body either as raw HTML (stored as index.html) or as a base64-encoded ZIP archive.",
	}, s.createContent)

	sdk.AddTool(server, &sdk.Tool{
		Name:        "update_content",
		Description: "Update one of the authenticated owner's content entries by slug. Only supplied metadata fields are changed; supply html or zip_base64 to replace the files.",
	}, s.updateContent)

	sdk.AddTool(server, &sdk.Tool{
		Name:        "delete_content",
		Description: "Delete one of the authenticated owner's content entries by slug.",
	}, s.deleteContent)
}

func (s *Server) listContent(_ context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	user, err := owner(req)
	if err != nil {
		return nil, nil, err
	}
	items, _, err := s.contentSvc.List(user, 10000, 0)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(items)
}

func (s *Server) getContent(_ context.Context, req *sdk.CallToolRequest, in getContentInput) (*sdk.CallToolResult, any, error) {
	user, err := owner(req)
	if err != nil {
		return nil, nil, err
	}
	if in.Slug == "" {
		return errorResult(errors.New("slug is required")), nil, nil
	}
	resp, err := s.contentSvc.Get(in.Slug, user)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(resp)
}

func (s *Server) createContent(_ context.Context, req *sdk.CallToolRequest, in createContentInput) (*sdk.CallToolResult, any, error) {
	user, err := owner(req)
	if err != nil {
		return nil, nil, err
	}
	if in.Slug == "" {
		return errorResult(errors.New("slug is required")), nil, nil
	}
	data, fileName, err := siteData(in.HTML, in.ZipBase64)
	if err != nil {
		return errorResult(err), nil, nil
	}
	resp, err := s.contentSvc.Create(in.Slug, data, fileName, user, in.Title, in.SourceURL, in.Creator, in.AllowExternalResources)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(resp)
}

func (s *Server) updateContent(_ context.Context, req *sdk.CallToolRequest, in updateContentInput) (*sdk.CallToolResult, any, error) {
	user, err := owner(req)
	if err != nil {
		return nil, nil, err
	}
	if in.Slug == "" {
		return errorResult(errors.New("slug is required")), nil, nil
	}

	var data []byte
	var fileName string
	if in.HTML != "" || in.ZipBase64 != "" {
		data, fileName, err = siteData(in.HTML, in.ZipBase64)
		if err != nil {
			return errorResult(err), nil, nil
		}
	}

	resp, err := s.contentSvc.Update(in.Slug, user, data, fileName, in.Title, in.SourceURL, in.Creator, in.AllowExternalResources)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(resp)
}

func (s *Server) deleteContent(_ context.Context, req *sdk.CallToolRequest, in deleteContentInput) (*sdk.CallToolResult, any, error) {
	user, err := owner(req)
	if err != nil {
		return nil, nil, err
	}
	if in.Slug == "" {
		return errorResult(errors.New("slug is required")), nil, nil
	}
	if err := s.contentSvc.Delete(in.Slug, user); err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(map[string]any{"deleted": in.Slug})
}

// ---- helpers -------------------------------------------------------------

// siteData turns the html / zip_base64 tool inputs into the (fileData, fileName)
// pair the content service expects. Exactly one of html / zipBase64 must be set.
func siteData(html, zipBase64 string) ([]byte, string, error) {
	switch {
	case html != "" && zipBase64 != "":
		return nil, "", errors.New("provide either html or zip_base64, not both")
	case html != "":
		return []byte(html), "index.html", nil
	case zipBase64 != "":
		data, err := base64.StdEncoding.DecodeString(zipBase64)
		if err != nil {
			return nil, "", fmt.Errorf("zip_base64 is not valid base64: %w", err)
		}
		return data, "upload.zip", nil
	default:
		return nil, "", errors.New("either html or zip_base64 is required")
	}
}

// jsonResult renders a value as a pretty-printed JSON text content block.
func jsonResult(v any) (*sdk.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(err), nil, nil
	}
	return &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: string(data)}},
	}, nil, nil
}

// errorResult returns a tool-level error result (isError=true) rather than a
// protocol error, so the model can read the message.
func errorResult(err error) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}},
	}
}
