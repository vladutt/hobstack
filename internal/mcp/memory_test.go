package mcp

import "testing"

func TestToolsForMemory(t *testing.T) {
	with := []Caller{
		{HasParent: true, TaskID: 3},        // worker pe task
		{IsLead: true, CanAddAgents: true},  // radacina in chat
		{HasParent: true, IsReviewer: true}, // reviewer
	}
	without := []Caller{{BrainstormCardID: 1}, {DraftID: "d"}, {SetupID: "s"}}
	for _, c := range with {
		for _, name := range []string{"remember", "recall", "search_memory", "forget"} {
			if !allowed(c, name) {
				t.Errorf("%+v: no %s", c, name)
			}
		}
	}
	for _, c := range without {
		if allowed(c, "remember") || allowed(c, "recall") {
			t.Errorf("%+v: has memory tools", c)
		}
	}
}
