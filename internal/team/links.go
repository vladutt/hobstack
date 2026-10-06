package team

import (
	"errors"

	"foreman/internal/mcp"
)

// Tool-urile pentru proiecte legate (implementarea completa vine in pasul urmator al planului).

func (t *Team) ListLinkedProjects(c mcp.Caller) (string, error) {
	return "", errors.New("not available yet")
}

func (t *Team) AskProject(c mcp.Caller, project, question string) (string, error) {
	return "", errors.New("not available yet")
}

func (t *Team) ReplyToProject(c mcp.Caller, request, answer string) (string, error) {
	return "", errors.New("not available yet")
}

func (t *Team) RequestProjectWork(c mcp.Caller, project, title, description string) (string, error) {
	return "", errors.New("not available yet")
}
