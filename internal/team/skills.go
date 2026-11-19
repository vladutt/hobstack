package team

import (
	"fmt"
	"regexp"
	"strings"
)

// Refuzurile tool-ului Skill: claude permite singur skill-urile din plugin-uri (Library) fara allowed-tools,
// dar le refuza pe cele care vin cu Claude Code (ex. claude-api) si pe cele care isi dau tool-uri prin
// allowed-tools. Nu permitem "Skill" in --settings: un skill cu allowed-tools ar ocoli permisiunile
// (verificat cu claude 2.1.328: un agent fara Shell a rulat Bash prin skill). Cardul din Needs you propune
// in schimb instalarea skill-ului in Library si in proiect.

// skillField: campul "skill" din inputul JSON; regex, nu json, pentru ca detail-ul poate fi scurtat (120 de caractere)
var skillField = regexp.MustCompile(`"skill"\s*:\s*"([^"\\]+)"`)

// SkillOfDenial: numele skill-ului dintr-un refuz al tool-ului Skill (detail = inputul JSON,
// {"skill":"claude-api"}); "" pentru alte tool-uri sau un input fara nume.
func SkillOfDenial(tool, detail string) string {
	if tool != "Skill" {
		return ""
	}
	m := skillField.FindStringSubmatch(detail)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// SkillDetail: detail-ul salvat pentru un refuz Skill, fara argumente: cererile pentru acelasi skill se comaseaza.
func SkillDetail(name string) string { return fmt.Sprintf(`{"skill":%q}`, name) }

// SkillBase: numele fara prefixul plugin-ului ("anthropic-skills:pdf" -> "pdf").
func SkillBase(name string) string {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// SkillAvailable: skill-ul name a fost instalat in proiect din Library. Inchide cererile Skill deschise
// pentru el (orice prefix de plugin) si trezeste agentii, o data pe agent si task.
func (t *Team) SkillAvailable(projectID int64, name string) error {
	t.work.Lock()
	reqs, err := t.st.ListOpenAccessRequests(projectID)
	if err != nil {
		t.work.Unlock()
		return err
	}
	msg := fmt.Sprintf("The skill %q is now installed in this project (added by the user). Use it with the Skill tool as \"hobstack-library:%s\" if you still need it.", name, name)
	woken := map[string]bool{}
	closed := false
	for _, r := range reqs {
		if SkillBase(SkillOfDenial(r.Tool, r.Detail)) != name || name == "" {
			continue
		}
		if err := t.st.DismissAccessRequest(r.ID); err != nil {
			t.work.Unlock()
			return err
		}
		closed = true
		key := fmt.Sprint(r.AgentID, "/", ptrVal(r.TaskID))
		if woken[key] {
			continue
		}
		woken[key] = true
		if err := t.wakeAgent(r, msg); err != nil {
			t.work.Unlock()
			return err
		}
	}
	t.work.Unlock()
	if closed {
		t.emit(projectID, "access", map[string]int64{})
		t.Kick(projectID)
	}
	return nil
}

func ptrVal(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
