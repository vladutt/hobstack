package config

import (
	"os"
	"path/filepath"
)

// Paths tine toate locatiile de pe disc folosite de agentdash.
type Paths struct {
	Root   string // ~/.agentdash
	Agents string // ~/.agentdash/agents
	Logs   string // ~/.agentdash/logs/runs
}

func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	root := filepath.Join(home, ".agentdash")
	return Paths{
		Root:   root,
		Agents: filepath.Join(root, "agents"),
		Logs:   filepath.Join(root, "logs", "runs"),
	}, nil
}

// Ensure creeaza folderele daca nu exista.
func (p Paths) Ensure() error {
	for _, d := range []string{p.Root, p.Agents, p.Logs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
