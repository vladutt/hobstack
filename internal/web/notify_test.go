package web

import (
	"net/http"
	"strconv"
	"testing"
)

func TestProjectNotifySwitch(t *testing.T) {
	e := setup(t)
	p := e.createProject(t, t.TempDir())
	url := "/api/projects/" + strconv.FormatInt(p.ID, 10) + "/notify"
	if code, raw := e.do(t, "PUT", url, map[string]bool{"on": false}); code != http.StatusNoContent {
		t.Fatalf("%d %s", code, raw)
	}
	if got, _ := e.st.GetProject(p.ID); got.Notify {
		t.Fatal("notify a ramas pornit")
	}
	if code, _ := e.do(t, "PUT", "/api/projects/999/notify", map[string]bool{"on": true}); code != http.StatusNotFound {
		t.Fatalf("inexistent: %d", code)
	}
}

func TestAppNotifySettings(t *testing.T) {
	e, _ := appEnv(t)
	if st := appState(t, e); st["notifyDone"] != true || st["notifyNeeds"] != true {
		t.Fatalf("implicit: %v", st)
	}
	if code, raw := e.do(t, "PUT", "/api/app/notify", map[string]bool{"needs": false}); code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if st := appState(t, e); st["notifyDone"] != true || st["notifyNeeds"] != false {
		t.Fatalf("dupa needs=false: %v", st)
	}
	if set := e.srv.NotifySettings(); !set.Done || set.Needs {
		t.Fatalf("NotifySettings: %+v", set)
	}
}
