package team

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"hobstack/internal/config"
	"hobstack/internal/mcp"
	"hobstack/internal/prompt"
	"hobstack/internal/runner"
	"hobstack/internal/store"
)

// maxQuestions: cate intrebari poate pune un agent intr-o runda.
const maxQuestions = 2

// brainstormRun porneste run-ul de brainstorming pentru card: construieste tura din subiect,
// pune cardul running si porneste run-ul (doar citire, sesiune separata). Se cheama cu t.work tinut.
func (t *Team) brainstormRun(p store.Project, a store.Agent, agents []store.Agent, card store.BrainstormCard) error {
	turn, err := t.brainstormTurn(p, a, agents, card)
	if err == nil {
		err = t.startRunOpts(p, a, agents, runOpts{Turn: turn, Card: &card})
	}
	if err != nil {
		// run-ul n-a pornit: cardul ramane failed (cu Retry), nu se reia la nesfarsit
		if serr := t.st.SetCardState(card.ID, store.CardFailed, "", err.Error()); serr != nil {
			log.Printf("brainstorm card %d: %v", card.ID, serr)
		}
		t.emit(p.ID, "brainstorm", map[string]int64{"topicId": card.TopicID})
	}
	return err
}

// brainstormTurn scrie tura agentului pentru card, cu ce are voie sa vada din subiect.
func (t *Team) brainstormTurn(p store.Project, a store.Agent, agents []store.Agent, card store.BrainstormCard) (string, error) {
	topic, err := t.st.GetTopic(card.TopicID)
	if err != nil {
		return "", err
	}
	rounds, err := t.st.TopicRounds(topic.ID)
	if err != nil {
		return "", err
	}
	cards, err := t.st.TopicCards(topic.ID)
	if err != nil {
		return "", err
	}
	files, err := t.st.ListTopicFiles(topic.ID)
	if err != nil {
		return "", err
	}
	asked, err := t.st.QuestionsAsked(card.RoundID, a.ID)
	if err != nil {
		return "", err
	}

	names := map[int64]string{}
	for _, ag := range agents {
		names[ag.ID] = ag.Name
	}
	c := prompt.BrainstormContext{Topic: topic.Name, Description: topic.Description, QuestionsLeft: maxQuestions - asked}
	for _, r := range rounds {
		if r.ID == card.RoundID {
			c.Message = r.Text
		}
	}
	// atasamentele: <ProjectBrainstormDir>/<topic>/<stored>
	dir := ""
	if t.filesRoot != "" {
		dir = filepath.Join(config.ProjectBrainstormDir(t.filesRoot, p.ID), strconv.FormatInt(topic.ID, 10))
	}
	for _, f := range files {
		c.Files = append(c.Files, prompt.FileRef{Name: f.Name, Path: filepath.Join(dir, f.Stored)})
	}

	for _, x := range cards {
		if x.Status != store.CardDone {
			continue
		}
		mine := x.AgentID == a.ID
		earlier := x.RoundID < card.RoundID
		switch x.Kind {
		case store.CardIdea, store.CardWild:
			idea := prompt.BrainstormIdea{Author: names[x.AgentID], Kind: x.Kind, Title: x.Title, Text: x.Text, Basis: x.Basis}
			switch {
			case x.Picked: // ideile alese le vede toata lumea, complet
				c.Picked = append(c.Picked, idea)
			case earlier && !mine: // din rundele anterioare doar titlul, ca sa nu le repete (ale lui sunt in Own)
				c.Unpicked = append(c.Unpicked, x.Title)
			}
			if mine {
				c.Own = append(c.Own, idea)
			}
			// un frate idea/wild al aceluiasi agent in aceeasi runda: cardul cere alta idee
			if mine && x.RoundID == card.RoundID && x.ID != card.ID {
				c.Another = true
			}
		case store.CardQuestion:
			// intrebarile cu raspuns: din rundele anterioare ale tuturor, din runda curenta doar ale lui
			if x.Answer == "" || !(earlier || mine && x.RoundID == card.RoundID) {
				continue
			}
			qa := prompt.BrainstormQA{Question: x.Title, Answer: x.Answer}
			if !mine {
				qa.Author = names[x.AgentID]
			}
			if x.Text != "" && x.Text != x.Title {
				qa.Question += " " + x.Text
			}
			c.QA = append(c.QA, qa)
		}
	}
	return prompt.BrainstormTurn(c), nil
}

// BrainstormReply e tool-ul brainstorm_reply: raspunsul agentului devine cardul run-ului.
func (t *Team) BrainstormReply(c mcp.Caller, in mcp.BrainstormReplyInput) (string, error) {
	if c.BrainstormCardID == 0 {
		return "", errors.New("brainstorm_reply is only available in a brainstorming run")
	}
	t.work.Lock()
	defer t.work.Unlock()

	card, err := t.st.GetCard(c.BrainstormCardID)
	if err != nil {
		return "", err
	}
	if card.Status != store.CardRunning || card.RunID != c.RunID {
		return "", errors.New("You already answered this card.")
	}
	kind := strings.TrimSpace(in.Kind)
	title, text, basis := strings.TrimSpace(in.Title), strings.TrimSpace(in.Text), strings.TrimSpace(in.Basis)
	switch {
	case kind != store.CardIdea && kind != store.CardWild && kind != store.CardQuestion:
		return "", errors.New("kind must be idea, wild or question")
	case title == "" || utf8.RuneCountInString(title) > 120:
		return "", errors.New("title is required: 1-120 characters")
	case text == "" || utf8.RuneCountInString(text) > 4000:
		return "", errors.New("text is required: 1-4000 characters")
	case utf8.RuneCountInString(basis) > 500:
		return "", errors.New("basis is too long: at most 500 characters")
	}
	var options []string
	if kind == store.CardQuestion {
		for _, o := range in.Options {
			o = strings.TrimSpace(o)
			if o == "" || utf8.RuneCountInString(o) > 80 {
				return "", errors.New("each option must have 1-80 characters")
			}
			options = append(options, o)
		}
		if len(options) < 2 || len(options) > 5 {
			return "", errors.New("a question needs 2-5 options")
		}
		asked, err := t.st.QuestionsAsked(card.RoundID, card.AgentID)
		if err != nil {
			return "", err
		}
		if asked >= maxQuestions {
			return "", fmt.Errorf("You already asked %d questions in this round; propose an idea with what you know.", maxQuestions)
		}
	}
	if err := t.st.FinishCard(card.ID, kind, title, text, basis, options); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return "", errors.New("You already answered this card.")
		}
		return "", err
	}
	t.emit(c.ProjectID, "brainstorm", map[string]int64{"topicId": card.TopicID})
	if kind == store.CardQuestion {
		return "Your question is on the topic; the user will answer it. Stop here.", nil
	}
	return "Your idea is on the topic. Stop here.", nil
}

// finishCard e finalul unui run de brainstorming (din recordOutcome, dupa FinishRun si tokeni):
// sesiunea subiectului, apoi starea cardului. Nimic in chat-ul agentului.
func (t *Team) finishCard(ar activeRun, agentID int64, hash string, o runner.Outcome) error {
	defer t.emit(ar.projectID, "brainstorm", map[string]int64{"topicId": ar.topicID})
	// o sesiune nesalvata doar porneste una noua data viitoare; cardul trebuie totusi inchis,
	// altfel ramane running fara niciun run in spate
	if o.SessionID != "" {
		if err := t.st.SetBrainstormSession(ar.topicID, agentID, o.SessionID, hash); err != nil {
			log.Printf("brainstorm card %d: saving session: %v", ar.cardID, err)
		}
	}
	card, err := t.st.GetCard(ar.cardID)
	if err != nil {
		log.Printf("brainstorm card %d: %v", ar.cardID, err)
		return nil // emit-ul de mai sus tot pleaca: UI-ul reciteste subiectul
	}
	if card.Status == store.CardDone {
		return nil // a raspuns cu brainstorm_reply
	}
	switch {
	case o.Status == store.RunStopped:
		return t.st.SetCardState(card.ID, store.CardStopped, o.RunID, "")
	case o.Status == store.RunFailed && runner.IsAuthError(o.Error):
		// contul a cazut: cardul e Failed (cu Retry), proiectul intra pe pauza ca la orice run.
		// Nu waiting: pe pauza brainstorming-ul merge, cardul ar reporni la nesfarsit cu contul picat
		if err := t.st.SetCardState(card.ID, store.CardFailed, o.RunID, o.Error); err != nil {
			return err
		}
		a, err := t.st.GetAgent(agentID)
		if err != nil {
			return err
		}
		return t.authFailed(ar, a, o.Error)
	case o.Status == store.RunFailed:
		return t.st.SetCardState(card.ID, store.CardFailed, o.RunID, o.Error)
	}
	// s-a terminat fara brainstorm_reply: textul final devine ideea (primul rand = titlul)
	text := strings.TrimSpace(o.Result)
	if text == "" {
		return t.st.SetCardState(card.ID, store.CardFailed, o.RunID, "The agent gave no answer.")
	}
	title, _, _ := strings.Cut(text, "\n")
	title = strings.TrimSpace(title)
	if r := []rune(title); len(r) > 120 {
		title = string(r[:120])
	}
	return t.st.FinishCard(card.ID, store.CardIdea, title, text, "", nil)
}

// BrainstormError: o cerere de brainstorming refuzata (date gresite), cu motivul pentru utilizator.
type BrainstormError string

func (e BrainstormError) Error() string { return string(e) }

// CardStateError: actiunea nu se potriveste cu starea cardului (ex. Pick pe un card in asteptare).
type CardStateError string

func (e CardStateError) Error() string { return string(e) }

// NewRound salveaza mesajul utilizatorului in subiect si cate un card waiting pentru fiecare
// participant care mai exista; cardurile pornesc cand agentii sunt liberi (Reconcile).
func (t *Team) NewRound(topicID int64, text string) (store.BrainstormRound, []store.BrainstormCard, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > 4000 {
		return store.BrainstormRound{}, nil, BrainstormError("The message must have 1-4000 characters.")
	}
	t.work.Lock()
	topic, err := t.st.GetTopic(topicID)
	if err != nil {
		t.work.Unlock()
		return store.BrainstormRound{}, nil, err
	}
	if topic.Archived {
		t.work.Unlock()
		return store.BrainstormRound{}, nil, BrainstormError("The topic is archived.")
	}
	// ListAgents nu intoarce agentii stersi
	agents, err := t.st.ListAgents(topic.ProjectID)
	if err != nil {
		t.work.Unlock()
		return store.BrainstormRound{}, nil, err
	}
	var ids []int64
	for _, a := range agents {
		if slices.Contains(topic.Participants, a.ID) {
			ids = append(ids, a.ID)
		}
	}
	if len(ids) == 0 {
		t.work.Unlock()
		return store.BrainstormRound{}, nil, BrainstormError("The topic has no participants.")
	}
	r := store.BrainstormRound{TopicID: topic.ID, Text: text}
	cards, err := t.st.AddRound(&r, ids)
	t.work.Unlock()
	if err != nil {
		return store.BrainstormRound{}, nil, err
	}
	t.emit(topic.ProjectID, "brainstorm", map[string]int64{"topicId": topic.ID})
	t.Kick(topic.ProjectID)
	return r, cards, nil
}

// cardAction e drumul comun al actiunilor pe un card: sub t.work citeste cardul si aplica fn,
// apoi anunta UI-ul si trezeste proiectul (un card nou sau reluat poate porni).
// Cu active, actiunea e refuzata pe un subiect arhivat (pune sau reia carduri care n-ar porni).
func (t *Team) cardAction(cardID int64, active bool, fn func(card store.BrainstormCard) error) error {
	t.work.Lock()
	card, err := t.st.GetCard(cardID)
	var topic store.BrainstormTopic
	if err == nil {
		topic, err = t.st.GetTopic(card.TopicID)
	}
	if err == nil && active && topic.Archived {
		err = CardStateError("The topic is archived.")
	}
	if err == nil {
		err = fn(card)
	}
	projectID := topic.ProjectID
	t.work.Unlock()
	if err != nil {
		return err
	}
	t.emit(projectID, "brainstorm", map[string]int64{"topicId": card.TopicID})
	t.Kick(projectID)
	return nil
}

// isIdea: cardul e o idee (obisnuita sau nebuna) terminata.
func isIdea(c store.BrainstormCard) bool {
	return c.Status == store.CardDone && (c.Kind == store.CardIdea || c.Kind == store.CardWild)
}

// AnswerCardQuestion salveaza raspunsul utilizatorului la intrebarea agentului si pune un card nou
// in asteptare pentru acelasi agent si runda: la urmatorul run agentul vine cu ideea.
// (Numele AnswerQuestion e luat de intrebarile ask_user.)
func (t *Team) AnswerCardQuestion(cardID int64, answer string) error {
	answer = strings.TrimSpace(answer)
	if answer == "" || utf8.RuneCountInString(answer) > 4000 {
		return BrainstormError("The answer must have 1-4000 characters.")
	}
	return t.cardAction(cardID, false, func(card store.BrainstormCard) error {
		// raspunsul si cardul nou intr-o tranzactie: fara raspuns salvat si agent care nu mai vine
		if _, err := t.st.AnswerCardWithFollowUp(card.ID, answer); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return CardStateError("This card is not an open question.")
			}
			return err
		}
		return nil
	})
}

// AnotherIdea cere agentului inca o idee pe acelasi mesaj (card nou in aceeasi runda).
func (t *Team) AnotherIdea(cardID int64) error {
	return t.cardAction(cardID, true, func(card store.BrainstormCard) error {
		if !isIdea(card) {
			return CardStateError("Another idea works only on an idea.")
		}
		next := store.BrainstormCard{TopicID: card.TopicID, RoundID: card.RoundID, AgentID: card.AgentID}
		return t.st.AddCard(&next)
	})
}

// RunNow: cardul porneste inaintea urmatorului task din coada al agentului (dupa run-ul in curs).
func (t *Team) RunNow(cardID int64) error {
	return t.cardAction(cardID, true, func(card store.BrainstormCard) error {
		if card.Status != store.CardWaiting {
			return CardStateError("Run now works only on a waiting card.")
		}
		return t.st.SetCardPriority(card.ID, true)
	})
}

// RetryCard pune inapoi in asteptare un card oprit sau esuat.
func (t *Team) RetryCard(cardID int64) error {
	return t.cardAction(cardID, true, func(card store.BrainstormCard) error {
		if card.Status != store.CardStopped && card.Status != store.CardFailed {
			return CardStateError("Retry works only on a stopped or failed card.")
		}
		return t.st.SetCardState(card.ID, store.CardWaiting, "", "")
	})
}

// PickCard fixeaza (sau scoate) ideea in zona Picked a subiectului.
func (t *Team) PickCard(cardID int64, picked bool) error {
	return t.cardAction(cardID, false, func(card store.BrainstormCard) error {
		if !isIdea(card) {
			return CardStateError("Only ideas can be picked.")
		}
		return t.st.SetCardPicked(card.ID, picked)
	})
}

// pickedIdeas intoarce ideile alese ale subiectului, in ordinea crearii.
func (t *Team) pickedIdeas(topicID int64) ([]store.BrainstormCard, error) {
	cards, err := t.st.TopicCards(topicID)
	if err != nil {
		return nil, err
	}
	var out []store.BrainstormCard
	for _, c := range cards {
		if isIdea(c) && c.Picked {
			out = append(out, c)
		}
	}
	return out, nil
}

// authorOf: numele agentului care a dat cardul, si pentru un agent sters.
func (t *Team) authorOf(agentID int64) string {
	if a, err := t.st.GetAgent(agentID); err == nil {
		return a.Name
	}
	return "Unknown agent"
}

// SendToLead trimite liderului ales ideile alese, ca mesaj al utilizatorului: e o cerere din chat,
// deci liderul poate porni task-uri din ea (start_task / create_task).
func (t *Team) SendToLead(topicID, leadID int64) (store.Message, error) {
	t.work.Lock()
	m, projectID, err := t.sendToLeadLocked(topicID, leadID)
	t.work.Unlock()
	if err != nil {
		return store.Message{}, err
	}
	t.Kick(projectID)
	return m, nil
}

func (t *Team) sendToLeadLocked(topicID, leadID int64) (store.Message, int64, error) {
	topic, err := t.st.GetTopic(topicID)
	if err != nil {
		return store.Message{}, 0, err
	}
	agents, err := t.st.ListAgents(topic.ProjectID)
	if err != nil {
		return store.Message{}, 0, err
	}
	lead, ok := store.FindAgent(agents, leadID)
	if !ok {
		return store.Message{}, 0, BrainstormError("The lead is not in this project.")
	}
	if r := store.RolesOf(lead, agents); !r.IsLead && !r.IsRoot {
		return store.Message{}, 0, BrainstormError(lead.Name + " is not a lead.")
	}
	ideas, err := t.pickedIdeas(topic.ID)
	if err != nil {
		return store.Message{}, 0, err
	}
	if len(ideas) == 0 {
		return store.Message{}, 0, BrainstormError("Pick at least one idea first.")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From the brainstorm %q: turn these picked ideas into tasks.", topic.Name)
	// legatura spre subiect (ruta din UI), ca utilizatorul sa ajunga inapoi din chat
	fmt.Fprintf(&b, "\nTopic: #/p/%d/brainstorm/%d", topic.ProjectID, topic.ID)
	for i, c := range ideas {
		fmt.Fprintf(&b, "\n\n%d. %s (by %s)\n%s", i+1, c.Title, t.authorOf(c.AgentID), c.Text)
	}
	m, err := t.addUserMessage(lead, b.String(), nil, false, false)
	return m, topic.ProjectID, err
}

// BrainstormMarkdown scrie rezumatul subiectului pentru Files: descrierea, ideile alese si
// intrebarile cu raspuns. Numele e fix per subiect (cu id-ul, ca doua subiecte cu acelasi
// slug sa nu se suprascrie), ca o salvare noua sa-l inlocuiasca.
func (t *Team) BrainstormMarkdown(topicID int64) (string, []byte, error) {
	t.work.Lock()
	defer t.work.Unlock()
	topic, err := t.st.GetTopic(topicID)
	if err != nil {
		return "", nil, err
	}
	cards, err := t.st.TopicCards(topic.ID)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", topic.Name)
	if topic.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", topic.Description)
	}
	b.WriteString("\n## Picked ideas\n")
	n := 0
	for _, c := range cards {
		if !isIdea(c) || !c.Picked {
			continue
		}
		n++
		wild := ""
		if c.Kind == store.CardWild {
			wild = " (wild)"
		}
		fmt.Fprintf(&b, "\n### %s%s\n\n*By %s*\n\n%s\n", c.Title, wild, t.authorOf(c.AgentID), c.Text)
		if c.Basis != "" {
			fmt.Fprintf(&b, "\nBased on: %s\n", c.Basis)
		}
	}
	if n == 0 {
		b.WriteString("\nNo picked ideas yet.\n")
	}
	header := false
	for _, c := range cards {
		if c.Status != store.CardDone || c.Kind != store.CardQuestion || c.Answer == "" {
			continue
		}
		if !header {
			b.WriteString("\n## Questions\n")
			header = true
		}
		q := c.Title
		if c.Text != "" && c.Text != c.Title {
			q += " " + c.Text
		}
		fmt.Fprintf(&b, "\n**%s** (%s)\n\nAnswer: %s\n", q, t.authorOf(c.AgentID), c.Answer)
	}
	return fmt.Sprintf("brainstorm-%d-%s.md", topic.ID, topicSlug(topic.Name)), []byte(b.String()), nil
}

// topicSlug face din numele subiectului un nume de fisier: litere mici, cifre si "-".
func topicSlug(name string) string {
	var b strings.Builder
	dash := true // true la inceput: nu incepem cu "-"
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	if slug == "" {
		return "topic"
	}
	return slug
}
