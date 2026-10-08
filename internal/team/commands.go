package team

import (
	"errors"
	"fmt"
	"slices"

	"foreman/internal/prompt"
	"foreman/internal/store"
)

var ErrNotCommandRequest = errors.New("this request is not about a command")

// EffectiveCommandRules: regulile globale cu override-urile proiectului.
func (t *Team) EffectiveCommandRules(projectID int64) ([]store.CommandRule, error) {
	global, err := t.st.CommandRules(0)
	if err != nil {
		return nil, err
	}
	project, err := t.st.CommandRules(projectID)
	if err != nil {
		return nil, err
	}
	return prompt.EffectiveRules(global, project), nil
}

func (t *Team) AllowCommandOnce(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		return "allowed once", t.st.AddCommandGrant(r.ProjectID, r.AgentID, r.Prefix)
	}, true)
}

func (t *Team) AllowCommandInProject(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		return "allowed in this project", t.setProjectRule(r.ProjectID, r.Prefix, store.CmdAllow)
	}, true)
}

func (t *Team) BlockCommandInProject(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		return "", t.setProjectRule(r.ProjectID, r.Prefix, store.CmdBlock)
	}, false)
}

// setProjectRule pune (sau inlocuieste) o regula a proiectului, pastrand restul.
func (t *Team) setProjectRule(projectID int64, prefix, mode string) error {
	rules, err := t.st.CommandRules(projectID)
	if err != nil {
		return err
	}
	rules = slices.DeleteFunc(rules, func(r store.CommandRule) bool { return r.Prefix == prefix })
	rules = append(rules, store.CommandRule{Prefix: prefix, Mode: mode})
	return t.st.SetCommandRules(projectID, rules)
}

// decideCommand aplica decizia pe o cerere de comanda, o inchide si, la Allow, trezeste agentul
// (mesaj nelivrat) si pune task-ul inapoi in coada.
func (t *Team) decideCommand(accessID int64, apply func(store.AccessRequest) (string, error), wake bool) error {
	t.work.Lock()
	r, err := t.st.GetAccessRequest(accessID)
	if err != nil {
		t.work.Unlock()
		return err
	}
	if r.Prefix == "" {
		t.work.Unlock()
		return ErrNotCommandRequest
	}
	how, err := apply(r)
	if err != nil {
		t.work.Unlock()
		return err
	}
	if err := t.st.DismissAccessRequest(r.ID); err != nil {
		t.work.Unlock()
		return err
	}
	if wake {
		a, err := t.st.GetAgent(r.AgentID)
		if err != nil {
			t.work.Unlock()
			return err
		}
		if r.TaskID != nil {
			if err := t.requeueTaskFor(*r.TaskID, a); err != nil {
				t.work.Unlock()
				return err
			}
		}
		msg := fmt.Sprintf("You may now run %q (%s by the user). Run it again if you still need it.", r.Detail, how)
		if _, err := t.addUserMessage(a, clip(msg), nil, true, false); err != nil {
			t.work.Unlock()
			return err
		}
	}
	t.emit(r.ProjectID, "access", map[string]int64{"agentId": r.AgentID})
	t.work.Unlock()
	t.Kick(r.ProjectID)
	return nil
}

// requeueTaskFor pune task-ul in coada daca e In progress la agent, nu ruleaza chiar acum si nu asteapta
// o intrebare deschisa (aceleasi conditii ca requeueAnswered). Se apeleaza cu t.work blocat.
func (t *Team) requeueTaskFor(taskID int64, a store.Agent) error {
	if t.activeTaskID(a.ID) == taskID {
		return nil
	}
	task, err := t.st.GetTask(taskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if task.Status != store.StatusInProgress || task.AssigneeID != a.ID {
		return nil
	}
	if waiting, err := t.st.HasOpenQuestionForTask(task.ID); err != nil || waiting {
		return err
	}
	task.Status = store.StatusQueued
	if err := t.st.SaveTask(&task); err != nil {
		return err
	}
	t.emit(task.ProjectID, "task", task)
	return nil
}
