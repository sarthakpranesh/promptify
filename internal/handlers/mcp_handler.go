package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"promptify/internal/store"
)

// RegisterEmbeddedPromptifyMCPTools registers list_prompts and get_prompt backed by the database
// (same data as the REST API). Use with server.NewStreamableHTTPServer on the main HTTP server.
func RegisterEmbeddedPromptifyMCPTools(s *server.MCPServer, ph *PromptHandler) {
	s.AddTool(
		mcp.NewTool("list_prompts",
			mcp.WithDescription("JSON array of prompts: id (internal), name, description, version, variables. When you show this to a user, reformat—do not dump JSON. Use server instructions."),
		),
		embeddedListPrompts(ph),
	)

	s.AddTool(
		mcp.NewTool("get_prompt",
			mcp.WithDescription("Fetch one prompt by id: full active template, description, variable names, and full version history. Use when you need the template text or history."),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Prompt id from list_prompts (tool use; not the user-facing name)"),
			),
		),
		embeddedGetPrompt(ph),
	)
}

func embeddedListPrompts(ph *PromptHandler) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		userID, ok := UserIDFromContext(ctx)
		if !ok {
			return mcp.NewToolResultError("unauthorized"), nil
		}

		list, err := ph.LoadPromptListForUser(userID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to load prompts: %v", err)), nil
		}
		data, err := json.MarshalIndent(list.Prompts, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}
}

func embeddedGetPrompt(ph *PromptHandler) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		userID, ok := UserIDFromContext(ctx)
		if !ok {
			return mcp.NewToolResultError("unauthorized"), nil
		}

		rawID := strings.TrimSpace(req.GetString("id", ""))
		if rawID == "" {
			return mcp.NewToolResultError("parameter 'id' is required"), nil
		}

		promptID, err := parsePromptID(rawID)
		if err != nil {
			return mcp.NewToolResultError("parameter 'id' is required and must be a valid prompt id"), nil
		}

		detail, err := ph.GetPromptDetailForUserByID(promptID, userID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return mcp.NewToolResultError(fmt.Sprintf("prompt %s not found", promptID)), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("failed to load prompt: %v", err)), nil
		}
		data, err := json.MarshalIndent(detail, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}
}
