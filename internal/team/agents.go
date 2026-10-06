package team

import (
	"errors"

	"foreman/internal/mcp"
)

// AddAgent: tool-ul add_agent (implementarea completa vine in pasul urmator al planului).
func (t *Team) AddAgent(c mcp.Caller, in mcp.AddAgentInput) (string, error) {
	return "", errors.New("add_agent is not available yet")
}

// RemoveAgent: tool-ul remove_agent.
func (t *Team) RemoveAgent(c mcp.Caller, name string) (string, error) {
	return "", errors.New("remove_agent is not available yet")
}
