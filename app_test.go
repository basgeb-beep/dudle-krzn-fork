package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ---------------------------------------------------------------- logic

func TestParseDates(t *testing.T) {
	got, err := parseDates(
		[]string{"2026-12-02", "2026-12-01", "2026-12-01", "", "2026-12-01"},
		[]string{"", "18:00", "18:00", "", ""})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-12-01", "2026-12-01T18:00", "2026-12-02"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	for _, bad := range [][]string{{"2026-13-01"}, {"nope"}, {"2026-02-30"}} {
		if _, err := parseDates(bad, nil); !errors.Is(err, errBadDate) {
			t.Errorf("%v: expected errBadDate, got %v", bad, err)
		}
	}
	if _, err := parseDates([]string{"2026-12-01"}, []string{"25:99"}); !errors.Is(err, errBadDate) {
		t.Error("bad time accepted")
	}
}

func TestParseChoices(t *testing.T) {
	got, err := parseChoices("Pizza\r\n  Sushi \n\nPizza\n\x00Burger")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "Pizza|Sushi|Burger" {
		t.Fatalf("got %v", got)
	}
	if _, err := parseChoices(strings.Repeat("x", 101)); !errors.Is(err, errTooLong) {
		t.Error("long label accepted")
	}
}

func TestRandID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := randID(10)
		if len(id) != 10 || !regexp.MustCompile(`^[a-z2-7]+$`).MatchString(id) {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatal("duplicate id")
		}
		seen[id] = true
	}
}

func TestSummarizeBest(t *testing.T) {
	d := &PollData{
		Poll:    Poll{Kind: KindDate},
		Options: []Option{{ID: 1, Label: "2026-12-01"}, {ID: 2, Label: "2026-12-02T18:00"}, {ID: 3, Label: "2027-01-05"}},
		Participants: []Participant{
			{Votes: map[int64]int{1: VoteYes, 2: VoteYes, 3: VoteMaybe}},
			{Votes: map[int64]int{1: VoteYes, 2: VoteMaybe}},
			{Votes: map[int64]int{1: VoteNo, 2: VoteYes}},
		},
	}
	s := Summarize(d, "de")
	if !s.Options[1].Best || s.Options[0].Best || s.Options[2].Best {
		t.Fatalf("wrong best: %+v", s.Options)
	}
	if s.Options[1].Yes != 2 || s.Options[1].Maybe != 1 || s.Options[0].No != 1 {
		t.Fatalf("wrong tally: %+v", s.Options)
	}
	if len(s.Groups) != 2 || s.Groups[0].Span != 2 || s.Groups[0].Name != "Dez 2026" {
		t.Fatalf("wrong groups: %+v", s.Groups)
	}
	if got := s.Options[1].Full; got != "Mi, 2. Dez 2026, 18:00" {
		t.Fatalf("german format: %q", got)
	}
	if got := Summarize(d, "en").Options[1].Full; got != "Wed, Dec 2, 2026, 18:00" {
		t.Fatalf("english format: %q", got)
	}

	empty := Summarize(&PollData{Poll: Poll{Kind: KindText}, Options: []Option{{ID: 1, Label: "a"}}}, "en")
	if len(empty.Best) != 0 {
		t.Fatal("no votes must not produce a best option")
	}
}

func TestMessagesComplete(t *testing.T) {
	for key := range messages["en"] {
		for _, l := range supportedLangs {
			if messages[l][key] == "" {
				t.Errorf("lang %s missing key %q", l, key)
			}
		}
	}
	for l, m := range messages {
		for key := range m {
			if messages["en"][key] == "" {
				t.Errorf("lang %s has unknown key %q", l, key)
			}
		}
	}
}

func TestDetectLang(t *testing.T) {
	cases := []struct{ accept, query, want string }{
		{"de-DE,de;q=0.9,en;q=0.8", "", "de"},
		{"fr-FR,fr;q=0.9", "", "en"},
		{"", "", "en"},
		{"en-US", "?lang=de", "de"},
		{"de", "?lang=xx", "de"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/"+c.query, nil)
		r.Header.Set("Accept-Language", c.accept)
		if got := detectLang(r); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
}

// ---------------------------------------------------------------- store

func TestStoreLifecycle(t *testing.T) {
	st := testStore(t)
	p := &Poll{ID: "abc", Title: "T", Kind: KindText, AdminToken: "tok", AllowMaybe: true}
	if err := st.CreatePoll(p, []string{"A", "B", "C"}); err != nil {
		t.Fatal(err)
	}
	d, err := st.Load("abc")
	if err != nil || len(d.Options) != 3 {
		t.Fatalf("load: %v %+v", err, d)
	}
	a, b := d.Options[0].ID, d.Options[1].ID

	pid, err := st.SaveVote("abc", 0, "Anna", map[int64]int{a: VoteYes, b: VoteMaybe, 9999: VoteYes})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveVote("abc", 0, "anna", nil); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("expected ErrNameTaken, got %v", err)
	}
	if _, err := st.SaveVote("abc", pid, "Anna", map[int64]int{a: VoteNo, b: VoteYes}); err != nil {
		t.Fatalf("editing own vote with same name must work: %v", err)
	}
	d, _ = st.Load("abc")
	if got := d.Participants[0].Votes; got[a] != VoteNo || got[b] != VoteYes || len(got) != 2 {
		t.Fatalf("votes not replaced: %v", got)
	}

	if err := st.DeleteOption("abc", a); err != nil {
		t.Fatal(err)
	}
	d, _ = st.Load("abc")
	if len(d.Options) != 2 || len(d.Participants[0].Votes) != 1 {
		t.Fatalf("option delete must cascade to votes: %+v", d)
	}
	st.DeleteOption("abc", d.Options[0].ID)
	if err := st.DeleteOption("abc", d.Options[0].ID); !errors.Is(err, ErrLastOption) {
		t.Fatalf("expected ErrLastOption, got %v", err)
	}

	if err := st.AddOptions("abc", []string{"D", "D"}); err != nil {
		t.Fatal(err)
	}
	d, _ = st.Load("abc")
	if len(d.Options) != 2 {
		t.Fatalf("duplicate labels must be ignored, got %d options", len(d.Options))
	}

	if err := st.UpdateSettings("abc", "New", "desc", false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveVote("abc", 0, "Bob", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed poll must reject votes, got %v", err)
	}

	if err := st.DeletePoll("abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load("abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStoreVoteScopedToPoll(t *testing.T) {
	st := testStore(t)
	st.CreatePoll(&Poll{ID: "p1", Title: "1", Kind: KindText, AdminToken: "x"}, []string{"A"})
	st.CreatePoll(&Poll{ID: "p2", Title: "2", Kind: KindText, AdminToken: "y"}, []string{"B"})
	d1, _ := st.Load("p1")
	pid, _ := st.SaveVote("p1", 0, "Anna", map[int64]int{d1.Options[0].ID: VoteYes})
	// A participant of poll 1 must not be editable via poll 2.
	if _, err := st.SaveVote("p2", pid, "Eve", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-poll edit must fail, got %v", err)
	}
	// Deleting via the wrong poll is a no-op.
	st.DeleteParticipant("p2", pid)
	d1, _ = st.Load("p1")
	if len(d1.Participants) != 1 {
		t.Fatal("cross-poll delete succeeded")
	}
}

func TestDeleteInactive(t *testing.T) {
	st := testStore(t)
	st.CreatePoll(&Poll{ID: "old", Title: "o", Kind: KindText, AdminToken: "x"}, []string{"A"})
	st.db.Exec(`UPDATE polls SET updated_at=? WHERE id='old'`, time.Now().Add(-400*24*time.Hour).Unix())
	st.CreatePoll(&Poll{ID: "new", Title: "n", Kind: KindText, AdminToken: "y"}, []string{"A"})
	n, err := st.DeleteInactive(time.Now().Add(-365 * 24 * time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := st.GetPoll("new"); err != nil {
		t.Fatal("recent poll was removed")
	}
}

// ---------------------------------------------------------------- HTTP

type client struct {
	t *testing.T
	h http.Handler
}

func (c *client) do(method, path string, form url.Values, hdr ...string) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

func newClient(t *testing.T) *client {
	srv, err := NewServer(testStore(t), "")
	if err != nil {
		t.Fatal(err)
	}
	return &client{t: t, h: srv.Handler()}
}

func TestHTTPFullFlow(t *testing.T) {
	c := newClient(t)

	// Home renders (both languages).
	for _, lang := range []string{"en", "de"} {
		w := c.do("GET", "/?lang="+lang, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `<html lang="`+lang+`"`) {
			t.Fatalf("home %s: %d", lang, w.Code)
		}
		if w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("security headers missing")
		}
	}

	// Validation errors keep the form and return 422.
	w := c.do("POST", "/create", url.Values{"title": {"  "}, "kind": {"text"}, "choices": {"a"}})
	if w.Code != 422 {
		t.Fatalf("empty title: %d", w.Code)
	}
	w = c.do("POST", "/create", url.Values{"title": {"X"}, "kind": {"date"}})
	if w.Code != 422 || !strings.Contains(w.Body.String(), "at least one option") {
		t.Fatalf("no options: %d", w.Code)
	}
	w = c.do("POST", "/create", url.Values{"title": {"X"}, "kind": {"date"}, "date": {"31.12.2026"}})
	if w.Code != 422 {
		t.Fatalf("bad date: %d", w.Code)
	}

	// Create a date poll.
	w = c.do("POST", "/create", url.Values{
		"title": {"Team <b>dinner</b>"}, "description": {"Somewhere nice"}, "kind": {"date"},
		"date": {"2026-12-03", "2026-12-01"}, "time": {"", "19:00"}, "allow_maybe": {"1"},
	})
	if w.Code != 303 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	m := regexp.MustCompile(`^/p/([a-z2-7]{10})/admin/([a-z2-7]{26})$`).FindStringSubmatch(loc)
	if m == nil {
		t.Fatalf("unexpected redirect %q", loc)
	}
	id, token := m[1], m[2]

	// Admin page; wrong token is a 404.
	if w = c.do("GET", loc, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "/p/"+id) {
		t.Fatalf("admin: %d", w.Code)
	}
	if w = c.do("GET", "/p/"+id+"/admin/"+strings.Repeat("a", 26), nil); w.Code != 404 {
		t.Fatalf("wrong token: %d", w.Code)
	}
	if w = c.do("POST", "/p/"+id+"/admin/wrong/delete_poll", url.Values{}); w.Code != 404 {
		t.Fatalf("wrong token POST: %d", w.Code)
	}

	// Poll page escapes HTML in titles.
	w = c.do("GET", "/p/"+id, nil)
	body := w.Body.String()
	if w.Code != 200 || strings.Contains(body, "<b>dinner</b>") || !strings.Contains(body, "&lt;b&gt;dinner") {
		t.Fatalf("poll page / escaping: %d", w.Code)
	}
	if !strings.Contains(body, `class="day">1<`) || strings.Index(body, `class="day">1<`) > strings.Index(body, `class="day">3<`) {
		t.Fatal("dates not sorted chronologically")
	}
	optIDs := regexp.MustCompile(`name="o_(\d+)"`).FindAllStringSubmatch(body, -1)
	if len(optIDs) != 2 {
		t.Fatalf("expected 2 vote selects, got %d", len(optIDs))
	}
	o1, o2 := optIDs[0][1], optIDs[1][1]

	// Vote.
	vote := func(name string, v1, v2 string, extra ...string) *httptest.ResponseRecorder {
		f := url.Values{"name": {name}, "o_" + o1: {v1}, "o_" + o2: {v2}}
		for i := 0; i+1 < len(extra); i += 2 {
			f.Set(extra[i], extra[i+1])
		}
		return c.do("POST", "/p/"+id+"/vote", f)
	}
	if w = vote("Anna", "1", "2"); w.Code != 303 {
		t.Fatalf("vote: %d", w.Code)
	}
	if w = vote("anna", "1", "1"); w.Code != 422 || !strings.Contains(w.Body.String(), "already taken") {
		t.Fatalf("duplicate name: %d", w.Code)
	}
	if w = vote("", "1", "1"); w.Code != 422 {
		t.Fatalf("empty name: %d", w.Code)
	}
	if w = vote("Ben", "1", "1"); w.Code != 303 {
		t.Fatalf("vote 2: %d", w.Code)
	}

	body = c.do("GET", "/p/"+id, nil).Body.String()
	if !strings.Contains(body, "Anna") || !strings.Contains(body, "Ben") || !strings.Contains(body, "best-banner") {
		t.Fatal("votes or best banner missing")
	}
	pidRe := regexp.MustCompile(`\?edit=(\d+)#vote`)
	pid := pidRe.FindStringSubmatch(body)[1]

	// Edit mode renders a prefilled form.
	w = c.do("GET", "/p/"+id+"?edit="+pid, nil)
	if !strings.Contains(w.Body.String(), `name="pid" value="`+pid+`"`) || !strings.Contains(w.Body.String(), `value="Anna"`) {
		t.Fatal("edit form missing")
	}
	if w = vote("Anna", "0", "0", "pid", pid); w.Code != 303 {
		t.Fatalf("edit vote: %d", w.Code)
	}

	// Comments.
	if w = c.do("POST", "/p/"+id+"/comment", url.Values{"name": {"Ben"}, "text": {"Can't wait"}}); w.Code != 303 {
		t.Fatalf("comment: %d", w.Code)
	}
	if w = c.do("POST", "/p/"+id+"/comment", url.Values{"name": {"Ben"}, "text": {""}}); w.Code != 422 {
		t.Fatalf("empty comment: %d", w.Code)
	}
	if !strings.Contains(c.do("GET", "/p/"+id, nil).Body.String(), "Can&#39;t wait") {
		t.Fatal("comment missing")
	}

	// Export and feed.
	w = c.do("GET", "/p/"+id+"/export.csv", nil)
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || !strings.Contains(w.Body.String(), "Ben,yes,yes") {
		t.Fatalf("csv: %q", w.Body.String())
	}
	w = c.do("GET", "/p/"+id+"/feed.atom", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<feed") || !strings.Contains(w.Body.String(), "Ben") {
		t.Fatalf("feed: %d %s", w.Code, w.Body.String())
	}

	// Admin actions.
	base := "/p/" + id + "/admin/" + token + "/"
	if w = c.do("POST", base+"settings", url.Values{"title": {"Renamed"}, "closed": {"1"}}); w.Code != 303 {
		t.Fatalf("settings: %d", w.Code)
	}
	body = c.do("GET", "/p/"+id, nil).Body.String()
	if !strings.Contains(body, "Renamed") || !strings.Contains(body, "This poll is closed") {
		t.Fatal("settings not applied")
	}
	if w = vote("Cara", "1", "1"); w.Code != 422 || !strings.Contains(w.Body.String(), "closed") {
		t.Fatalf("vote on closed poll: %d", w.Code)
	}
	if w = c.do("POST", base+"settings", url.Values{"title": {"Renamed"}}); w.Code != 303 { // reopen
		t.Fatal("reopen failed")
	}
	if w = c.do("POST", base+"add_options", url.Values{"date": {"2026-12-05"}, "time": {"10:30"}}); w.Code != 303 {
		t.Fatalf("add option: %d", w.Code)
	}
	if w = c.do("POST", base+"add_options", url.Values{}); w.Code != 422 {
		t.Fatalf("empty add: %d", w.Code)
	}
	if w = c.do("POST", base+"delete_participant", url.Values{"participant": {pid}}); w.Code != 303 {
		t.Fatal("delete participant")
	}
	if strings.Contains(c.do("GET", "/p/"+id, nil).Body.String(), "Anna") {
		t.Fatal("participant not deleted")
	}
	if w = c.do("POST", base+"nonsense", url.Values{}); w.Code != 404 {
		t.Fatalf("unknown action: %d", w.Code)
	}
	if w = c.do("POST", base+"delete_poll", url.Values{}); w.Code != 303 {
		t.Fatal("delete poll")
	}
	if w = c.do("GET", "/p/"+id, nil); w.Code != 404 {
		t.Fatalf("deleted poll still served: %d", w.Code)
	}
}

func TestHTTPTextPollAndMisc(t *testing.T) {
	c := newClient(t)
	w := c.do("POST", "/create", url.Values{"title": {"Lunch"}, "kind": {"text"}, "choices": {"Pizza\nSushi"}})
	if w.Code != 303 {
		t.Fatalf("create: %d", w.Code)
	}
	id := strings.Split(w.Header().Get("Location"), "/")[2]
	body := c.do("GET", "/p/"+id, nil, "Accept-Language", "de").Body.String()
	for _, s := range []string{"Pizza", "Sushi", "Dein Name", "Teilnehmende"} {
		if !strings.Contains(body, s) {
			t.Errorf("poll page missing %q", s)
		}
	}
	if w = c.do("GET", "/nope", nil); w.Code != 404 || !strings.Contains(w.Body.String(), "Page not found") {
		t.Fatalf("404 page: %d", w.Code)
	}
	if w = c.do("GET", "/p/doesnotexist", nil); w.Code != 404 {
		t.Fatalf("unknown poll: %d", w.Code)
	}
	if w = c.do("GET", "/healthz", nil); w.Code != 200 {
		t.Fatal("healthz")
	}
	for _, f := range []string{"style.css", "app.js", "favicon.svg"} {
		if w = c.do("GET", "/static/"+f, nil); w.Code != 200 || w.Body.Len() == 0 {
			t.Errorf("static %s: %d", f, w.Code)
		}
	}
	// "if need be" is downgraded to "no" when the poll doesn't allow it.
	loc := c.do("POST", "/create", url.Values{"title": {"T"}, "kind": {"text"}, "choices": {"A"}}).Header().Get("Location")
	id = strings.Split(loc, "/")[2]
	oid := regexp.MustCompile(`name="o_(\d+)"`).FindStringSubmatch(c.do("GET", "/p/"+id, nil).Body.String())[1]
	c.do("POST", "/p/"+id+"/vote", url.Values{"name": {"Zed"}, "o_" + oid: {"2"}})
	if !strings.Contains(c.do("GET", "/p/"+id+"/export.csv", nil).Body.String(), "Zed,no") {
		t.Fatal("maybe vote accepted although disabled")
	}
}

func TestCSVInjection(t *testing.T) {
	for in, want := range map[string]string{"=1+1": "'=1+1", "+x": "'+x", "-x": "'-x", "@x": "'@x", "plain": "plain", "": ""} {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q)=%q want %q", in, got, want)
		}
	}
}

func TestRateLimit(t *testing.T) {
	srv, err := NewServer(testStore(t), "")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetRateLimit(6, false)
	h := srv.Handler()
	post := func(addr string) int {
		req := httptest.NewRequest("POST", "/create", strings.NewReader(""))
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	limited := 0
	for i := 0; i < 10; i++ {
		if post("10.0.0.1:1234") == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("expected some requests to be limited")
	}
	if post("10.0.0.2:1234") == http.StatusTooManyRequests {
		t.Fatal("other client must not be limited")
	}
	// GET requests are never limited
	for i := 0; i < 30; i++ {
		req := httptest.NewRequest("GET", "/healthz", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("GET limited: %d", rec.Code)
		}
	}
}

func TestLimiterRefill(t *testing.T) {
	l := newLimiter(60)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 20; i++ {
		if ok, _ := l.allow("a"); !ok {
			t.Fatalf("burst request %d denied", i)
		}
	}
	if ok, wait := l.allow("a"); ok || wait <= 0 {
		t.Fatal("expected denial with wait")
	}
	now = now.Add(2 * time.Second)
	if ok, _ := l.allow("a"); !ok {
		t.Fatal("expected refill after 2s")
	}
}

func TestAccessControlInviteDelete(t *testing.T) {
	c := newClient(t)
	loc := c.do("POST", "/create", url.Values{"title": {"T"}, "kind": {"text"}, "choices": {"A\nB"}}).Header().Get("Location")
	parts := strings.Split(loc, "/")
	id, token := parts[2], parts[4]
	admin := "/p/" + id + "/admin/" + token

	// Invite participants (duplicates and blanks skipped).
	if w := c.do("POST", admin+"/invite", url.Values{"names": {"Anna\n\nBen\nanna"}}); w.Code != 303 {
		t.Fatalf("invite: %d", w.Code)
	}
	body := c.do("GET", "/p/"+id, nil).Body.String()
	if !strings.Contains(body, "Anna") || !strings.Contains(body, "Ben") {
		t.Fatal("invited names missing")
	}

	// Delete confirmation card, then delete.
	w := c.do("GET", "/p/"+id+"?delete=1", nil)
	if !strings.Contains(w.Body.String(), "/delete_participant") {
		t.Fatal("no delete confirmation")
	}
	if w := c.do("POST", "/p/"+id+"/delete_participant", url.Values{"participant": {"1"}}); w.Code != 303 {
		t.Fatalf("delete participant: %d", w.Code)
	}
	if strings.Contains(c.do("GET", "/p/"+id, nil).Body.String(), "Anna") {
		t.Fatal("Anna should be deleted")
	}

	// Password validation.
	if w := c.do("POST", admin+"/set_password", url.Values{"role": {"participant"}, "password": {"short"}, "password2": {"short"}}); w.Code != 422 {
		t.Fatalf("short pw: %d", w.Code)
	}
	if w := c.do("POST", admin+"/set_password", url.Values{"role": {"participant"}, "password": {"longenough1"}, "password2": {"different11"}}); w.Code != 422 {
		t.Fatalf("mismatch: %d", w.Code)
	}

	// Participant password locks the poll for everyone without the cookie.
	w = c.do("POST", admin+"/set_password", url.Values{"role": {"participant"}, "password": {"secret-pw-1"}, "password2": {"secret-pw-1"}})
	if w.Code != 303 {
		t.Fatalf("set pw: %d", w.Code)
	}
	for _, p := range []string{"/p/" + id, "/p/" + id + "/export.csv", "/p/" + id + "/feed.atom"} {
		if w := c.do("GET", p, nil); w.Code != 401 {
			t.Fatalf("%s unlocked: %d", p, w.Code)
		}
	}
	if w := c.do("POST", "/p/"+id+"/vote", url.Values{"name": {"X"}}); w.Code != 401 {
		t.Fatalf("vote without pw: %d", w.Code)
	}
	if w := c.do("POST", "/p/"+id+"/unlock", url.Values{"password": {"nope"}}); w.Code != 401 {
		t.Fatalf("wrong pw: %d", w.Code)
	}
	w = c.do("POST", "/p/"+id+"/unlock", url.Values{"password": {"secret-pw-1"}})
	if w.Code != 303 || len(w.Result().Cookies()) == 0 {
		t.Fatalf("unlock: %d", w.Code)
	}
	ck := w.Result().Cookies()[0]
	cookie := ck.Name + "=" + ck.Value
	if w := c.do("GET", "/p/"+id, nil, "Cookie", cookie); w.Code != 200 {
		t.Fatalf("with cookie: %d", w.Code)
	}
	// Cookie from another poll / forged value does not work.
	if w := c.do("GET", "/p/"+id, nil, "Cookie", ck.Name+"=deadbeef"); w.Code != 401 {
		t.Fatalf("forged cookie: %d", w.Code)
	}
	// Admin page itself is not locked by the participant password.
	if w := c.do("GET", admin, nil); w.Code != 200 {
		t.Fatalf("admin: %d", w.Code)
	}

	// Admin password adds a second lock on the admin page.
	w = c.do("POST", admin+"/set_password", url.Values{"role": {"admin"}, "password": {"admin-pw-123"}, "password2": {"admin-pw-123"}})
	if w.Code != 303 {
		t.Fatalf("set admin pw: %d", w.Code)
	}
	if w := c.do("GET", admin, nil); w.Code != 401 {
		t.Fatalf("admin should be locked: %d", w.Code)
	}
	if w := c.do("POST", admin+"/delete_poll", url.Values{}); w.Code != 401 {
		t.Fatalf("admin action should be locked: %d", w.Code)
	}
	w = c.do("POST", admin+"/unlock", url.Values{"password": {"admin-pw-123"}})
	if w.Code != 303 {
		t.Fatalf("admin unlock: %d", w.Code)
	}
	var ac string
	for _, k := range w.Result().Cookies() {
		ac = k.Name + "=" + k.Value
	}
	if w := c.do("GET", admin, nil, "Cookie", ac); w.Code != 200 {
		t.Fatalf("admin with cookie: %d", w.Code)
	}
	// Admin cookie also opens the participant area.
	if w := c.do("GET", "/p/"+id, nil, "Cookie", ac); w.Code != 200 {
		t.Fatalf("admin cookie on poll: %d", w.Code)
	}
	// Removing the participant password re-opens the poll and invalidates nothing else.
	if w := c.do("POST", admin+"/remove_password", url.Values{"role": {"participant"}}, "Cookie", ac); w.Code != 303 {
		t.Fatalf("remove pw: %d", w.Code)
	}
	if w := c.do("GET", "/p/"+id, nil); w.Code != 200 {
		t.Fatalf("poll should be open again: %d", w.Code)
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword("correct horse", h) || checkPassword("wrong", h) || checkPassword("x", "garbage") {
		t.Fatal("password check broken")
	}
}
