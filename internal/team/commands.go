package team

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"hobstack/internal/prompt"
	"hobstack/internal/store"
)

var ErrNotCommandRequest = errors.New("this request is not about a command")
var ErrRequestClosed = errors.New("this request is already closed")

// ErrNoProjectRule: cererea e pe o comanda a unui agent fara Shell; o regula de proiect nu i-ar folosi.
var ErrNoProjectRule = errors.New("this agent has no Shell; allow the command on the task or enable Shell")

// ErrRuleChanged: regula prefixului nu mai e Ask, deci cererea e depasita.
var ErrRuleChanged = errors.New("the rule changed")

// ruleChangedError poarta mesajul pentru utilizator si se potriveste cu ErrRuleChanged.
type ruleChangedError struct{ prefix string }

func (e ruleChangedError) Error() string {
	return fmt.Sprintf("The rule for %s changed; this request is out of date.", e.prefix)
}
func (e ruleChangedError) Unwrap() error { return ErrRuleChanged }

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

// AllowCommandForTask permite prefixul agentului pe task-ul cererii (fara task: in chat, pana la New chat).
func (t *Team) AllowCommandForTask(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		how := "allowed in this chat"
		if r.TaskID != nil {
			task, err := t.st.GetTask(*r.TaskID)
			if err != nil {
				return "", err
			}
			how = "allowed on " + task.Ref()
		}
		return how, t.st.AddCommandGrant(r.ProjectID, r.AgentID, r.TaskID, r.Prefix)
	}, true, false)
}

func (t *Team) AllowCommandInProject(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		return "allowed in this project", t.setProjectRule(r.ProjectID, r.Prefix, store.CmdAllow)
	}, true, true)
}

func (t *Team) BlockCommandInProject(accessID int64) error {
	return t.decideCommand(accessID, func(r store.AccessRequest) (string, error) {
		return "", t.setProjectRule(r.ProjectID, r.Prefix, store.CmdBlock)
	}, false, true)
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

// decideCommand aplica decizia pe o cerere de comanda, o inchide si, la Allow (allow), trezeste agentul
// (mesaj nelivrat) si pune task-ul inapoi in coada. Allow cere ca prefixul sa fie inca Ask in regulile curente.
// Cand decizia e pe proiect (project), inchide si celelalte cereri deschise cu acelasi prefix din proiect
// (si la Allow trezeste si acei agenti).
func (t *Team) decideCommand(accessID int64, apply func(store.AccessRequest) (string, error), allow, project bool) error {
	t.work.Lock()
	r, err := t.st.GetAccessRequest(accessID)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}
	if err != nil {
		t.work.Unlock()
		return err
	}
	if r.Dismissed {
		t.work.Unlock()
		return ErrRequestClosed
	}
	if r.Prefix == "" {
		t.work.Unlock()
		return ErrNotCommandRequest
	}
	if project && r.Kind != store.AccessAsk {
		t.work.Unlock()
		return ErrNoProjectRule
	}
	if project && allow {
		// o regula Allow nu da nimic unui agent fara Shell
		if shell, err := t.agentHasShell(r.AgentID); err != nil || !shell {
			t.work.Unlock()
			if err != nil {
				return err
			}
			return ErrNoProjectRule
		}
	}
	if allow {
		// regula s-a putut schimba dupa cerere (de ex. in Block): un Allow nu are voie sa o ocoleasca
		rules, err := t.EffectiveCommandRules(r.ProjectID)
		if err != nil {
			t.work.Unlock()
			return err
		}
		ok := slices.Contains(rules, store.CommandRule{Prefix: r.Prefix, Mode: store.CmdAsk})
		if r.Kind == store.AccessCommand {
			// prefix calculat: permis cat timp nu e pe Block si nu e un refuz fix pentru tool-urile agentului
			// (o regula Ask pusa intre timp nu conteaza: grant-ul o ridica)
			a, err := t.st.GetAgent(r.AgentID)
			if err != nil {
				t.work.Unlock()
				return err
			}
			mode, _ := prompt.DenyingRule(r.Prefix, rules)
			ok = mode != store.CmdBlock && !prompt.AlwaysDeniedBash(r.Prefix, a.Tools, nil)
		}
		if !ok {
			t.work.Unlock()
			return ruleChangedError{r.Prefix}
		}
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
	if allow {
		if err := t.wakeForCommand(r, how); err != nil {
			t.work.Unlock()
			return err
		}
	}
	if project {
		open, err := t.st.ListOpenAccessRequests(r.ProjectID)
		if err != nil {
			t.work.Unlock()
			return err
		}
		for _, o := range open {
			if o.ID == r.ID || o.Prefix != r.Prefix || o.Kind != store.AccessAsk {
				continue
			}
			if allow {
				// agentii fara Shell nu primesc nimic din regula: cererea lor ramane deschisa
				if shell, err := t.agentHasShell(o.AgentID); err != nil || !shell {
					continue
				}
			}
			if err := t.st.DismissAccessRequest(o.ID); err != nil {
				t.work.Unlock()
				return err
			}
			if allow {
				if err := t.wakeForCommand(o, how); err != nil {
					t.work.Unlock()
					return err
				}
			}
			t.emit(o.ProjectID, "access", map[string]int64{"agentId": o.AgentID})
		}
	}
	t.emit(r.ProjectID, "access", map[string]int64{"agentId": r.AgentID})
	t.work.Unlock()
	t.Kick(r.ProjectID)
	return nil
}

// ErrNotToolRequest: tool-ul nu se poate porni din aceasta cerere.
var ErrNotToolRequest = errors.New("only shell access can be enabled from here")

// EnableTool adauga tool-ul ("shell" sau "packages") agentului cererii, inchide cererea si il trezeste.
// Shell: pentru o cerere de tool Bash sau o comanda calculata; packages: doar pentru o cerere de tool Bash.
func (t *Team) EnableTool(accessID int64, tool string) error {
	t.work.Lock()
	r, err := t.st.GetAccessRequest(accessID)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}
	if err != nil {
		t.work.Unlock()
		return err
	}
	if r.Dismissed {
		t.work.Unlock()
		return ErrRequestClosed
	}
	if r.Tool != "Bash" || r.Kind == store.AccessAsk || (tool == "packages" && r.Kind != store.AccessTool) {
		t.work.Unlock()
		return ErrNotToolRequest
	}
	a, err := t.st.GetAgent(r.AgentID)
	if err != nil {
		t.work.Unlock()
		return err
	}
	if !slices.Contains(a.Tools, tool) {
		a.Tools = append(a.Tools, tool)
		if err := t.st.UpdateAgent(a); err != nil {
			t.work.Unlock()
			return err
		}
		t.emit(a.ProjectID, "agent", map[string]int64{"id": a.ID})
	}
	dismiss := t.st.DismissAccessRequest(r.ID)
	if tool == "shell" {
		// cu Shell, celelalte cereri Bash de tool/comanda ale agentului nu mai au rost
		dismiss = t.st.DismissShellRequests(r.AgentID)
	}
	if err := dismiss; err != nil {
		t.work.Unlock()
		return err
	}
	msg := "You now have the Shell tool (enabled by the user). Run your command again if you still need it."
	if tool == "packages" {
		msg = "You may now install packages (enabled by the user). Run your command again if you still need it."
	}
	if err := t.wakeAgent(r, msg); err != nil {
		t.work.Unlock()
		return err
	}
	t.emit(r.ProjectID, "access", map[string]int64{"agentId": r.AgentID})
	t.work.Unlock()
	t.Kick(r.ProjectID)
	return nil
}

// agentHasShell spune daca agentul are tool-ul "shell".
func (t *Team) agentHasShell(agentID int64) (bool, error) {
	a, err := t.st.GetAgent(agentID)
	if err != nil {
		return false, err
	}
	return slices.Contains(a.Tools, "shell"), nil
}

// wakeForCommand: wakeAgent cu mesajul pentru un prefix permis. Se apeleaza cu t.work blocat.
// Daca agentul mai are cereri deschise pe acelasi task (sau in chat), asteapta ultima aprobare:
// altfel run-ul ar fi refuzat iar pe comanda inca neaprobata.
func (t *Team) wakeForCommand(r store.AccessRequest, how string) error {
	var tid int64
	if r.TaskID != nil {
		tid = *r.TaskID
	}
	if waiting, err := t.st.HasOpenAccessRequest(r.AgentID, tid); err != nil || waiting {
		return err
	}
	return t.wakeAgent(r, fmt.Sprintf("You may now run commands starting with %q (%s by the user). Run your command again if you still need it.", r.Prefix, how))
}

// wakeAgent pune task-ul cererii inapoi in coada si lasa agentului un mesaj nelivrat. Se apeleaza cu t.work blocat.
func (t *Team) wakeAgent(r store.AccessRequest, msg string) error {
	a, err := t.st.GetAgent(r.AgentID)
	if err != nil {
		return err
	}
	if r.TaskID != nil {
		if err := t.requeueTaskFor(*r.TaskID, a); err != nil {
			return err
		}
	}
	_, err = t.addUserMessage(a, clip(msg), nil, true, true)
	return err
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
