package team

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
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
			case earlier: // din rundele anterioare doar titlul, ca sa nu le repete
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
	if o.SessionID != "" {
		if err := t.st.SetBrainstormSession(ar.topicID, agentID, o.SessionID, hash); err != nil {
			return err
		}
	}
	defer t.emit(ar.projectID, "brainstorm", map[string]int64{"topicId": ar.topicID})
	card, err := t.st.GetCard(ar.cardID)
	if err != nil {
		return err
	}
	if card.Status == store.CardDone {
		return nil // a raspuns cu brainstorm_reply
	}
	switch {
	case o.Status == store.RunStopped:
		return t.st.SetCardState(card.ID, store.CardStopped, o.RunID, "")
	case o.Status == store.RunFailed && runner.IsAuthError(o.Error):
		// contul a cazut: cardul asteapta, proiectul intra pe pauza ca la orice run
		if err := t.st.SetCardState(card.ID, store.CardWaiting, "", ""); err != nil {
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
