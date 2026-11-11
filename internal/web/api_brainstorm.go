package web

import (
	"errors"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"hobstack/internal/config"
	"hobstack/internal/store"
	"hobstack/internal/taskfiles"
	"hobstack/internal/team"
)

const maxTopicFiles = 50

// topicDir: folderul cu fisierele unui subiect.
func (s *Server) topicDir(projectID, topicID int64) string {
	return filepath.Join(config.ProjectBrainstormDir(s.dirs.Projects, projectID), strconv.FormatInt(topicID, 10))
}

func (s *Server) publishTopic(projectID, topicID int64) {
	s.hub.Publish(projectID, "brainstorm", map[string]int64{"topicId": topicID})
}

// topicOf citeste subiectul din id-ul din cale (404 daca nu exista).
func (s *Server) topicOf(w http.ResponseWriter, r *http.Request) (store.BrainstormTopic, bool) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return store.BrainstormTopic{}, false
	}
	t, err := s.st.GetTopic(id)
	if err != nil {
		fail(w, err)
		return store.BrainstormTopic{}, false
	}
	return t, true
}

// checkParticipants: cel putin unul, toti agenti existenti ai proiectului.
func (s *Server) checkParticipants(projectID int64, ids []int64) ([]int64, error) {
	agents, err := s.st.ListAgents(projectID) // fara agentii stersi
	if err != nil {
		return nil, err
	}
	out := []int64{}
	for _, id := range ids {
		if !slices.ContainsFunc(agents, func(a store.Agent) bool { return a.ID == id }) {
			return nil, badRequest("a participant is not an agent of this project")
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, badRequest("choose at least one participant")
	}
	return out, nil
}

func checkTopicText(name, desc string) (string, string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 60 {
		return "", "", badRequest("the name must have 1-60 characters")
	}
	if err := maxLen("description", desc, 4000); err != nil {
		return "", "", err
	}
	return name, desc, nil
}

type topicView struct {
	store.BrainstormTopic
	OpenQuestions int `json:"openQuestions"`
	Waiting       int `json:"waiting"`
}

func (s *Server) listBrainstorm(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	topics, err := s.st.ListTopics(pid)
	if err != nil {
		fail(w, err)
		return
	}
	open, err := s.st.OpenQuestions(pid)
	if err != nil {
		fail(w, err)
		return
	}
	out := struct {
		Topics        []topicView `json:"topics"`
		OpenQuestions int         `json:"openQuestions"`
	}{Topics: []topicView{}}
	for _, t := range topics {
		cards, err := s.st.TopicCards(t.ID)
		if err != nil {
			fail(w, err)
			return
		}
		v := topicView{BrainstormTopic: t, OpenQuestions: open[t.ID]}
		for _, c := range cards {
			if c.Status == store.CardWaiting {
				v.Waiting++
			}
		}
		out.Topics = append(out.Topics, v)
		out.OpenQuestions += v.OpenQuestions
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createTopic(w http.ResponseWriter, r *http.Request) {
	pid, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.st.GetProject(pid); err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Name         string  `json:"name"`
		Description  string  `json:"description"`
		Participants []int64 `json:"participants"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	name, desc, err := checkTopicText(in.Name, in.Description)
	if err != nil {
		fail(w, err)
		return
	}
	parts, err := s.checkParticipants(pid, in.Participants)
	if err != nil {
		fail(w, err)
		return
	}
	t := store.BrainstormTopic{ProjectID: pid, Name: name, Description: desc, Participants: parts}
	if err := s.st.CreateTopic(&t); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "a topic with this name already exists")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.publishTopic(pid, t.ID)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) patchTopic(w http.ResponseWriter, r *http.Request) {
	t, ok := s.topicOf(w, r)
	if !ok {
		return
	}
	var in struct {
		Name         *string  `json:"name"`
		Description  *string  `json:"description"`
		Participants *[]int64 `json:"participants"`
		Archived     *bool    `json:"archived"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Name != nil {
		t.Name = *in.Name
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	var err error
	if t.Name, t.Description, err = checkTopicText(t.Name, t.Description); err != nil {
		fail(w, err)
		return
	}
	if in.Participants != nil {
		if t.Participants, err = s.checkParticipants(t.ProjectID, *in.Participants); err != nil {
			fail(w, err)
			return
		}
	}
	if in.Archived != nil {
		t.Archived = *in.Archived
	}
	if err := s.st.UpdateTopic(t); errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, "a topic with this name already exists")
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	s.publishTopic(t.ProjectID, t.ID)
	writeJSON(w, http.StatusOK, t)
}

type cardView struct {
	store.BrainstormCard
	AgentName string `json:"agentName"`
	Busy      string `json:"busy"` // T-N cand cardul asteapta si agentul lucreaza la un task
}

type roundView struct {
	store.BrainstormRound
	Cards []cardView `json:"cards"`
}

func (s *Server) getTopic(w http.ResponseWriter, r *http.Request) {
	t, ok := s.topicOf(w, r)
	if !ok {
		return
	}
	files, err := s.st.ListTopicFiles(t.ID)
	if err != nil {
		fail(w, err)
		return
	}
	rounds, err := s.st.TopicRounds(t.ID)
	if err != nil {
		fail(w, err)
		return
	}
	cards, err := s.st.TopicCards(t.ID)
	if err != nil {
		fail(w, err)
		return
	}
	agents, err := s.st.ListAgents(t.ProjectID)
	if err != nil {
		fail(w, err)
		return
	}
	tasks, err := s.st.ListTasks(t.ProjectID)
	if err != nil {
		fail(w, err)
		return
	}
	byID := map[int64]store.Agent{}
	for _, a := range agents {
		byID[a.ID] = a
	}
	views := make([]roundView, len(rounds))
	at := map[int64]int{}
	for i, rd := range rounds {
		views[i] = roundView{BrainstormRound: rd, Cards: []cardView{}}
		at[rd.ID] = i
	}
	for _, c := range cards {
		i, ok := at[c.RoundID]
		if !ok {
			continue
		}
		v := cardView{BrainstormCard: c}
		if a, ok := byID[c.AgentID]; ok {
			v.AgentName = a.Name
			if c.Status == store.CardWaiting {
				if status, cur := s.team.AgentStatus(a, tasks); status == team.StatusWorking && cur != nil {
					v.Busy = cur.Ref()
				}
			}
		} else if ag, err := s.st.GetAgent(c.AgentID); err == nil {
			v.AgentName = ag.Name // agent sters: numele ramane pe card
		}
		views[i].Cards = append(views[i].Cards, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"topic": t, "files": files, "rounds": views})
}

// addTopicFiles pune fisierele in subiect: intai intr-un staging din folderul subiectului, apoi
// rename; la orice eroare nu ramane nimic pe disc si in baza de date.
func (s *Server) addTopicFiles(t store.BrainstormTopic, files []*multipart.FileHeader) ([]store.BrainstormFile, error) {
	if len(files) == 0 {
		return []store.BrainstormFile{}, nil
	}
	have, err := s.st.ListTopicFiles(t.ID)
	if err != nil {
		return nil, err
	}
	if len(have)+len(files) > maxTopicFiles {
		return nil, badRequest("a topic can have at most 50 files")
	}
	dir := s.topicDir(t.ProjectID, t.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(dir, ".staging-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	var saved []store.BrainstormFile
	undo := func() {
		for _, f := range saved {
			os.Remove(filepath.Join(dir, f.Stored))
			if f.ID != 0 {
				s.st.DeleteTopicFile(f.ID)
			}
		}
	}
	for _, fh := range files {
		src, err := fh.Open()
		if err != nil {
			undo()
			return nil, err
		}
		c, err := taskfiles.Save(src, store.CleanFileName(fh.Filename), stage)
		src.Close()
		if err != nil {
			undo()
			return nil, err
		}
		if err := os.Rename(filepath.Join(stage, c.Stored), filepath.Join(dir, c.Stored)); err != nil {
			undo()
			return nil, err
		}
		f := store.BrainstormFile{TopicID: t.ID, Name: c.Name, Stored: c.Stored, Mime: normalizeMime(c.Mime), Size: c.Size}
		saved = append(saved, f)
		if err := s.st.AddTopicFile(&saved[len(saved)-1]); err != nil {
			saved[len(saved)-1].ID = 0
			undo()
			return nil, err
		}
	}
	return saved, nil
}

func (s *Server) uploadTopicFiles(w http.ResponseWriter, r *http.Request) {
	t, ok := s.topicOf(w, r)
	if !ok {
		return
	}
	files, err := s.readUploads(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	if len(files) == 0 {
		fail(w, badRequest("choose at least one file"))
		return
	}
	saved, err := s.addTopicFiles(t, files)
	if err != nil {
		fail(w, err)
		return
	}
	s.publishTopic(t.ProjectID, t.ID)
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) newRound(w http.ResponseWriter, r *http.Request) {
	t, ok := s.topicOf(w, r)
	if !ok {
		return
	}
	files, err := s.readUploads(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	saved, err := s.addTopicFiles(t, files)
	if err != nil {
		fail(w, err)
		return
	}
	round, cards, err := s.team.NewRound(t.ID, r.FormValue("text"))
	if err != nil {
		// runda nu s-a facut: fisierele adaugate acum se scot
		for _, f := range saved {
			os.Remove(filepath.Join(s.topicDir(t.ProjectID, t.ID), f.Stored))
			if derr := s.st.DeleteTopicFile(f.ID); derr != nil {
				log.Printf("brainstorm: cleaning up file %d: %v", f.ID, derr)
			}
		}
		fail(w, err)
		return
	}
	if len(saved) > 0 {
		s.publishTopic(t.ProjectID, t.ID)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"round": round, "cards": cards})
}

// topicFileOf: fisierul din cale, doar daca apartine subiectului din cale.
func (s *Server) topicFileOf(w http.ResponseWriter, r *http.Request) (store.BrainstormTopic, store.BrainstormFile, bool) {
	t, ok := s.topicOf(w, r)
	if !ok {
		return t, store.BrainstormFile{}, false
	}
	fid, err := strconv.ParseInt(r.PathValue("fid"), 10, 64)
	if err != nil {
		fail(w, badRequest("invalid id"))
		return t, store.BrainstormFile{}, false
	}
	f, err := s.st.GetTopicFile(fid)
	if err == nil && f.TopicID != t.ID {
		err = store.ErrNotFound
	}
	if err != nil {
		fail(w, err)
		return t, f, false
	}
	return t, f, true
}

func (s *Server) serveTopicFile(w http.ResponseWriter, r *http.Request) {
	t, bf, ok := s.topicFileOf(w, r)
	if !ok {
		return
	}
	f, err := os.Open(filepath.Join(s.topicDir(t.ProjectID, t.ID), bf.Stored))
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "the file is missing from disk")
			return
		}
		fail(w, err)
		return
	}
	defer f.Close()
	serveStored(w, r, f, bf.Name, bf.Mime, bf.CreatedAt, r.URL.Query().Get("download") == "1")
}

func (s *Server) deleteTopicFile(w http.ResponseWriter, r *http.Request) {
	t, bf, ok := s.topicFileOf(w, r)
	if !ok {
		return
	}
	path := filepath.Join(s.topicDir(t.ProjectID, t.ID), bf.Stored)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("cannot delete %s: %v", path, err)
	}
	if err := s.st.DeleteTopicFile(bf.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		fail(w, err)
		return
	}
	s.publishTopic(t.ProjectID, t.ID)
	w.WriteHeader(http.StatusNoContent)
}

// cardAction: pick / unpick / answer / another / run-now / stop / retry pe un card.
func (s *Server) cardAction(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, err)
		return
	}
	switch r.PathValue("action") {
	case "pick":
		err = s.team.PickCard(id, true)
	case "unpick":
		err = s.team.PickCard(id, false)
	case "answer":
		var in struct {
			Answer string `json:"answer"`
		}
		if err = decode(r, &in); err == nil {
			err = s.team.AnswerCardQuestion(id, in.Answer)
		}
	case "another":
		err = s.team.AnotherIdea(id)
	case "run-now":
		err = s.team.RunNow(id)
	case "stop":
		err = s.team.StopCard(id)
	case "retry":
		err = s.team.RetryCard(id)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// saveBrainstorm pune rezumatul subiectului in Files; o salvare noua suprascrie fisierul cu acelasi nume.
func (s *Server) saveBrainstorm(w http.ResponseWriter, r *http.Request) {
	t, ok := s.topicOf(w, r)
	if !ok {
		return
	}
	name, data, err := s.team.BrainstormMarkdown(t.ID)
	if err != nil {
		fail(w, err)
		return
	}
	dir := config.ProjectFilesDir(s.dirs.Projects, t.ProjectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(w, err)
		return
	}
	a, err := s.st.FindProjectFile(t.ProjectID, name)
	switch {
	case err == nil:
		if err := os.WriteFile(filepath.Join(dir, a.Stored), data, 0o644); err != nil {
			fail(w, err)
			return
		}
		if err := s.st.SetAttachmentSize(a.ID, int64(len(data))); err != nil {
			fail(w, err)
			return
		}
	case errors.Is(err, store.ErrNotFound):
		a = store.Attachment{ProjectID: t.ProjectID, Kind: store.KindProjectFile, Name: name, Stored: store.StoredName(name),
			Mime: "text/markdown; charset=utf-8", Size: int64(len(data))}
		path := filepath.Join(dir, a.Stored)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			fail(w, err)
			return
		}
		if err := s.st.AddAttachment(&a); err != nil {
			os.Remove(path)
			fail(w, err)
			return
		}
	default:
		fail(w, err)
		return
	}
	s.hub.Publish(t.ProjectID, "files", map[string]int64{"id": t.ProjectID})
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (s *Server) sendBrainstorm(w http.ResponseWriter, r *http.Request) {
	t, ok := s.topicOf(w, r)
	if !ok {
		return
	}
	var in struct {
		LeadID int64 `json:"leadId"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	m, err := s.team.SendToLead(t.ID, in.LeadID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"messageId": m.ID})
}
