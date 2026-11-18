package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

type ctxKey int

const langKey ctxKey = 0

type Server struct {
	store   *Store
	pages   map[string]*template.Template
	baseURL string // optional fixed public base URL

	contact       string   // operator contact for accessibility feedback (email, URL or text)
	basePath      string   // optional URL path prefix, e.g. "/dudle"
	secret        []byte   // signs unlock cookies
	unlockLimiter *limiter // brute-force protection for password forms
	limiter       *limiter // nil = no rate limiting
	trustProxy    bool     // use X-Forwarded-For to identify clients
}

// SetRateLimit enables per-client limiting of write requests (per minute, 0 = off).
func (s *Server) SetRateLimit(perMinute int, trustProxy bool) {
	s.trustProxy = trustProxy
	if perMinute > 0 {
		s.limiter = newLimiter(perMinute)
	} else {
		s.limiter = nil
	}
}

// VoteSel is the data for one vote <select>.
type VoteSel struct {
	V     *View
	Cell  Cell
	Label string
}

// DPData is the data for the date picker partial.
type DPData struct {
	V    *View
	Init string
	Rows []int // fallback rows when JavaScript is unavailable
}

func (d DPData) T(key string, args ...any) string { return d.V.T(key, args...) }

// PwForm is the data for one password form on the admin page.
type PwForm struct {
	V      *View
	Role   string
	Active bool
}

var templateFuncs = template.FuncMap{
	"tag":    func(l string) string { return strings.ReplaceAll(l, "_", "-") },
	"pw":     func(v *View, role, hash string) PwForm { return PwForm{V: v, Role: role, Active: hash != ""} },
	"dict":   func(v *View, c Cell, label string) VoteSel { return VoteSel{V: v, Cell: c, Label: label} },
	"dpData": func(v *View, init string) DPData { return DPData{V: v, Init: init, Rows: []int{0, 1, 2, 3}} },
}

func NewServer(store *Store, baseURL string) (*Server, error) {
	secret, err := store.Secret()
	if err != nil {
		return nil, fmt.Errorf("secret: %w", err)
	}
	s := &Server{store: store, baseURL: strings.TrimRight(baseURL, "/"), pages: map[string]*template.Template{},
		secret: secret, unlockLimiter: newLimiter(10)}
	for _, name := range []string{"home", "poll", "admin", "error", "unlock", "a11y_de", "a11y_en"} {
		t, err := template.New("").Funcs(templateFuncs).ParseFS(webFS, "web/templates/layout.html", "web/templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", name, err)
		}
		s.pages[name] = t
	}
	return s, nil
}

// ---------------------------------------------------------------- view model

type Cell struct {
	OptID int64
	Value int
}

type Row struct {
	ID      int64
	Name    string
	Cells   []Cell
	Editing bool
}

type CommentView struct {
	ID   int64
	Name string
	Text string
	When string
}

type Draft struct {
	Title       string
	Description string
	Kind        string
	Choices     string
	DatesInit   string
	AllowMaybe  bool
}

type View struct {
	Lang      string
	Path      string
	Title     string
	Error     string
	Notice    string
	Status    int
	Poll      *Poll
	Opts      []OptionView
	Groups    []MonthGroup
	Best      []OptionView
	Rows      []Row
	NewRow    []Cell
	NewName   string
	EditID    int64
	Comments  []CommentView
	CmtName   string
	CmtText   string
	PollURL   string
	AdminURL  string
	Draft     Draft
	Langs     []string
	NumVoters int

	UnlockRole   string
	UnlockAction string
	Contact      string
	DeleteID     int64
	DeleteName   string
}

func (v *View) T(key string, args ...any) string { return tr(v.Lang, key, args...) }
func (v *View) LangName(l string) string         { return langNames[l] }

// LangTag is the BCP 47 tag for the html lang attribute (es_AR -> es-AR).
func (v *View) LangTag() string { return strings.ReplaceAll(v.Lang, "_", "-") }

// Dir is the text direction of the current language.
func (v *View) Dir() string {
	if rtlLangs[v.Lang] {
		return "rtl"
	}
	return "ltr"
}
func (v *View) Symbol(val int) string {
	switch val {
	case VoteYes:
		return "✓"
	case VoteMaybe:
		return "?"
	}
	return "✕"
}
func (v *View) VoteLabel(val int) string {
	switch val {
	case VoteYes:
		return v.T("v_yes")
	case VoteMaybe:
		return v.T("v_maybe")
	}
	return v.T("v_no")
}
func (v *View) HasBest() bool { return len(v.Best) > 0 }
func (v *View) BestTitle() string {
	if len(v.Best) > 1 {
		return v.T("best_options")
	}
	return v.T("best_option")
}
func (v *View) BestText() string {
	var parts []string
	for _, b := range v.Best {
		parts = append(parts, b.Full)
	}
	return strings.Join(parts, " · ")
}

func langOf(r *http.Request) string {
	if l, ok := r.Context().Value(langKey).(string); ok {
		return l
	}
	return "en"
}

func (s *Server) newView(r *http.Request) *View {
	return &View{Lang: langOf(r), Path: r.URL.Path, Langs: supportedLangs, Status: http.StatusOK}
}

func (s *Server) render(w http.ResponseWriter, page string, v *View) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "internal error", 500)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		log.Printf("render %s: %v", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(v.Status)
	w.Write(s.prefixHTML(buf.Bytes()))
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, titleKey, msgKey string) {
	v := s.newView(r)
	v.Status = status
	v.Title = v.T(titleKey)
	v.Error = v.T(msgKey)
	s.render(w, "error", v)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	v := s.newView(r)
	v.Status = http.StatusNotFound
	v.Title = v.T("not_found_h")
	s.render(w, "error", v)
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	s.fail(w, r, http.StatusInternalServerError, "err_generic", "err_generic")
}

func (s *Server) absURL(r *http.Request, path string) string {
	if s.baseURL != "" {
		return s.baseURL + path
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + s.basePath + path
}

// ---------------------------------------------------------------- routing

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(http.FileServerFS(static))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /accessibility", s.accessibility)
	mux.HandleFunc("POST /create", s.create)
	mux.HandleFunc("GET /p/{id}", s.poll)
	mux.HandleFunc("POST /p/{id}/unlock", s.unlock(roleParticip))
	mux.HandleFunc("POST /p/{id}/admin/{token}/unlock", s.unlock(roleAdmin))
	mux.HandleFunc("POST /p/{id}/vote", s.vote)
	mux.HandleFunc("POST /p/{id}/delete_participant", s.deleteParticipant)
	mux.HandleFunc("POST /p/{id}/comment", s.comment)
	mux.HandleFunc("GET /p/{id}/feed.atom", s.feed)
	mux.HandleFunc("GET /p/{id}/export.csv", s.export)
	mux.HandleFunc("GET /p/{id}/admin/{token}", s.admin)
	mux.HandleFunc("POST /p/{id}/admin/{token}/{action}", s.adminAction)
	mux.HandleFunc("/", s.notFound)
	return s.withBasePath(s.middleware(s.rateLimit(mux)))
}

func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		lang := detectLang(r)
		if q := r.URL.Query().Get("lang"); isSupported(q) {
			http.SetCookie(w, &http.Cookie{Name: "lang", Value: q, Path: s.basePath + "/", MaxAge: 365 * 24 * 3600,
				SameSite: http.SameSiteLaxMode, HttpOnly: true, Secure: r.TLS != nil})
		}
		r = r.WithContext(context.WithValue(r.Context(), langKey, lang))
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/static/") && r.URL.Path != "/healthz" {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Microsecond))
		}
	})
}

// ---------------------------------------------------------------- home / create

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	v := s.newView(r)
	v.Draft = Draft{Kind: KindDate, AllowMaybe: true}
	s.render(w, "home", v)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, http.StatusBadRequest, "err_generic", "err_generic")
		return
	}
	v := s.newView(r)
	d := Draft{
		Title:       cleanLine(r.FormValue("title")),
		Description: cleanText(r.FormValue("description")),
		Kind:        KindDate,
		Choices:     r.FormValue("choices"),
		DatesInit:   dateInit(r.Form["date"], r.Form["time"]),
		AllowMaybe:  r.FormValue("allow_maybe") == "1",
	}
	if r.FormValue("kind") == KindText {
		d.Kind = KindText
	}
	v.Draft = d

	labels, msg := s.parseOptions(d.Kind, r, v)
	if msg == "" {
		switch {
		case d.Title == "" || tooLong(d.Title, maxTitle):
			msg = "err_title"
		case tooLong(d.Description, maxDescription):
			msg = "err_desc"
		}
	}
	if msg != "" {
		v.Status = http.StatusUnprocessableEntity
		v.Error = v.T(msg)
		s.render(w, "home", v)
		return
	}

	p := &Poll{ID: randID(10), AdminToken: randID(26), Title: d.Title, Description: d.Description,
		Kind: d.Kind, AllowMaybe: d.AllowMaybe}
	if err := s.store.CreatePoll(p, labels); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/p/"+p.ID+"/admin/"+p.AdminToken, http.StatusSeeOther)
}

// parseOptions reads option labels from a form depending on the poll kind.
// It returns an i18n message key on validation failure.
func (s *Server) parseOptions(kind string, r *http.Request, _ *View) ([]string, string) {
	var labels []string
	var err error
	if kind == KindDate {
		labels, err = parseDates(r.Form["date"], r.Form["time"])
	} else {
		labels, err = parseChoices(r.FormValue("choices"))
	}
	switch {
	case errors.Is(err, errBadDate):
		return nil, "err_date"
	case errors.Is(err, errTooLong):
		return nil, "err_label"
	case err != nil:
		return nil, "err_generic"
	case len(labels) == 0:
		return nil, "err_options"
	case len(labels) > maxOptions:
		return nil, "err_too_many"
	}
	return labels, ""
}

// ---------------------------------------------------------------- poll page

// loadPoll loads the poll and enforces the participant password.
func (s *Server) loadPoll(w http.ResponseWriter, r *http.Request) *PollData {
	d := s.loadPollRaw(w, r)
	if d == nil {
		return nil
	}
	if !s.unlocked(r, &d.Poll, roleParticip) {
		s.renderUnlock(w, r, &d.Poll, roleParticip, "")
		return nil
	}
	return d
}

func (s *Server) loadPollRaw(w http.ResponseWriter, r *http.Request) *PollData {
	d, err := s.store.Load(r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		s.notFound(w, r)
		return nil
	}
	if err != nil {
		s.serverError(w, r, err)
		return nil
	}
	return d
}

func (s *Server) pollView(r *http.Request, d *PollData) *View {
	v := s.newView(r)
	v.Title = d.Poll.Title
	v.Poll = &d.Poll
	sum := Summarize(d, v.Lang)
	v.Opts, v.Groups, v.Best = sum.Options, sum.Groups, sum.Best
	v.NumVoters = len(d.Participants)
	for _, p := range d.Participants {
		row := Row{ID: p.ID, Name: p.Name}
		for _, o := range d.Options {
			row.Cells = append(row.Cells, Cell{OptID: o.ID, Value: p.Votes[o.ID]})
		}
		v.Rows = append(v.Rows, row)
	}
	for _, o := range d.Options {
		v.NewRow = append(v.NewRow, Cell{OptID: o.ID})
	}
	for _, c := range d.Comments {
		v.Comments = append(v.Comments, CommentView{ID: c.ID, Name: c.Name, Text: c.Text,
			When: c.CreatedAt.Format("2006-01-02 15:04")})
	}
	v.PollURL = s.absURL(r, "/p/"+d.Poll.ID)
	return v
}

func (s *Server) poll(w http.ResponseWriter, r *http.Request) {
	d := s.loadPoll(w, r)
	if d == nil {
		return
	}
	v := s.pollView(r, d)
	if id, _ := strconv.ParseInt(r.URL.Query().Get("edit"), 10, 64); id > 0 && !d.Poll.Closed {
		for i := range v.Rows {
			if v.Rows[i].ID == id {
				v.Rows[i].Editing = true
				v.EditID = id
			}
		}
	}
	if id, _ := strconv.ParseInt(r.URL.Query().Get("delete"), 10, 64); id > 0 && !d.Poll.Closed {
		for _, row := range v.Rows {
			if row.ID == id {
				v.DeleteID, v.DeleteName = id, row.Name
			}
		}
	}
	s.render(w, "poll", v)
}

// deleteParticipant removes a participant after confirmation on the poll page.
func (s *Server) deleteParticipant(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, http.StatusBadRequest, "err_generic", "err_generic")
		return
	}
	d := s.loadPoll(w, r)
	if d == nil {
		return
	}
	if d.Poll.Closed {
		s.fail(w, r, http.StatusForbidden, "err_generic", "err_closed")
		return
	}
	id, _ := strconv.ParseInt(r.FormValue("participant"), 10, 64)
	if err := s.store.DeleteParticipant(d.Poll.ID, id); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/p/"+d.Poll.ID+"#participants", http.StatusSeeOther)
}

func (s *Server) vote(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, http.StatusBadRequest, "err_generic", "err_generic")
		return
	}
	d := s.loadPoll(w, r)
	if d == nil {
		return
	}
	pid, _ := strconv.ParseInt(r.FormValue("pid"), 10, 64)
	name := cleanLine(r.FormValue("name"))
	votes := map[int64]int{}
	for _, o := range d.Options {
		val, _ := strconv.Atoi(r.FormValue("o_" + strconv.FormatInt(o.ID, 10)))
		if val < VoteNo || val > VoteMaybe || (val == VoteMaybe && !d.Poll.AllowMaybe) {
			val = VoteNo
		}
		votes[o.ID] = val
	}

	var msg string
	switch {
	case d.Poll.Closed:
		msg = "err_closed"
	case name == "" || tooLong(name, maxName):
		msg = "err_name"
	default:
		_, err := s.store.SaveVote(d.Poll.ID, pid, name, votes)
		switch {
		case err == nil:
			http.Redirect(w, r, "/p/"+d.Poll.ID+"#participants", http.StatusSeeOther)
			return
		case errors.Is(err, ErrNameTaken):
			msg = "err_name_taken"
		case errors.Is(err, ErrClosed):
			msg = "err_closed"
		case errors.Is(err, ErrNotFound):
			s.notFound(w, r)
			return
		default:
			s.serverError(w, r, err)
			return
		}
	}

	// Re-render with the submitted values so nothing is lost.
	v := s.pollView(r, d)
	v.Status = http.StatusUnprocessableEntity
	v.Error = v.T(msg)
	cells := make([]Cell, 0, len(d.Options))
	for _, o := range d.Options {
		cells = append(cells, Cell{OptID: o.ID, Value: votes[o.ID]})
	}
	if pid > 0 {
		for i := range v.Rows {
			if v.Rows[i].ID == pid {
				v.Rows[i].Editing, v.Rows[i].Name, v.Rows[i].Cells = true, name, cells
				v.EditID = pid
			}
		}
	}
	if v.EditID == 0 {
		v.NewName, v.NewRow = name, cells
	}
	s.render(w, "poll", v)
}

func (s *Server) comment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, http.StatusBadRequest, "err_generic", "err_generic")
		return
	}
	d := s.loadPoll(w, r)
	if d == nil {
		return
	}
	name, text := cleanLine(r.FormValue("name")), cleanText(r.FormValue("text"))
	if name == "" || tooLong(name, maxName) || text == "" || tooLong(text, maxComment) {
		v := s.pollView(r, d)
		v.Status = http.StatusUnprocessableEntity
		v.Error = v.T("err_comment")
		v.CmtName, v.CmtText = name, text
		s.render(w, "poll", v)
		return
	}
	if err := s.store.AddComment(d.Poll.ID, name, text); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/p/"+d.Poll.ID+"#comments", http.StatusSeeOther)
}

// ---------------------------------------------------------------- feed / export

type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Link    atomLink    `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
}
type atomEntry struct {
	Title   string   `xml:"title"`
	ID      string   `xml:"id"`
	Updated string   `xml:"updated"`
	Link    atomLink `xml:"link"`
	Content string   `xml:"content"`
}

func (s *Server) feed(w http.ResponseWriter, r *http.Request) {
	d := s.loadPoll(w, r)
	if d == nil {
		return
	}
	lang := langOf(r)
	pollURL := s.absURL(r, "/p/"+d.Poll.ID)
	f := atomFeed{
		Title:   d.Poll.Title,
		ID:      pollURL,
		Updated: d.Poll.UpdatedAt.UTC().Format(time.RFC3339),
		Link:    atomLink{Href: pollURL, Rel: "alternate"},
	}
	for _, p := range d.Participants {
		f.Entries = append(f.Entries, atomEntry{
			Title: p.Name, ID: fmt.Sprintf("%s#participant-%d", pollURL, p.ID),
			Updated: p.CreatedAt.UTC().Format(time.RFC3339), Link: atomLink{Href: pollURL},
			Content: tr(lang, "participants") + ": " + p.Name,
		})
	}
	for _, c := range d.Comments {
		f.Entries = append(f.Entries, atomEntry{
			Title: c.Name, ID: fmt.Sprintf("%s#comment-%d", pollURL, c.ID),
			Updated: c.CreatedAt.UTC().Format(time.RFC3339), Link: atomLink{Href: pollURL},
			Content: c.Text,
		})
	}
	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	fmt.Fprint(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(f); err != nil {
		log.Printf("feed: %v", err)
	}
}

// csvSafe neutralises spreadsheet formula injection.
func csvSafe(s string) string {
	if s != "" && strings.ContainsAny(s[:1], "=+-@\t\r") {
		return "'" + s
	}
	return s
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	d := s.loadPoll(w, r)
	if d == nil {
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+d.Poll.ID+`.csv"`)
	cw := csv.NewWriter(w)
	head := []string{"name"}
	for _, o := range d.Options {
		head = append(head, csvSafe(o.Label))
	}
	cw.Write(head)
	names := map[int]string{VoteNo: "no", VoteYes: "yes", VoteMaybe: "maybe"}
	for _, p := range d.Participants {
		row := []string{csvSafe(p.Name)}
		for _, o := range d.Options {
			row = append(row, names[p.Votes[o.ID]])
		}
		cw.Write(row)
	}
	cw.Flush()
}

// ---------------------------------------------------------------- admin

func (s *Server) loadAdmin(w http.ResponseWriter, r *http.Request) *PollData {
	d := s.loadPollRaw(w, r)
	if d == nil {
		return nil
	}
	if !tokenOK(r.PathValue("token"), d.Poll.AdminToken) {
		s.notFound(w, r)
		return nil
	}
	if !s.unlocked(r, &d.Poll, roleAdmin) {
		s.renderUnlock(w, r, &d.Poll, roleAdmin, "")
		return nil
	}
	return d
}

func (s *Server) adminView(r *http.Request, d *PollData) *View {
	v := s.pollView(r, d)
	v.Title = v.T("admin_title") + " · " + d.Poll.Title
	v.AdminURL = s.absURL(r, "/p/"+d.Poll.ID+"/admin/"+d.Poll.AdminToken)
	if r.URL.Query().Get("saved") == "1" {
		v.Notice = v.T("saved")
	}
	return v
}

func (s *Server) admin(w http.ResponseWriter, r *http.Request) {
	d := s.loadAdmin(w, r)
	if d == nil {
		return
	}
	s.render(w, "admin", s.adminView(r, d))
}

func (s *Server) adminAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, http.StatusBadRequest, "err_generic", "err_generic")
		return
	}
	d := s.loadAdmin(w, r)
	if d == nil {
		return
	}
	id := d.Poll.ID
	back := "/p/" + id + "/admin/" + d.Poll.AdminToken + "?saved=1"
	num := func(key string) int64 { n, _ := strconv.ParseInt(r.FormValue(key), 10, 64); return n }

	var err error
	var msg string
	switch r.PathValue("action") {
	case "settings":
		title, desc := cleanLine(r.FormValue("title")), cleanText(r.FormValue("description"))
		switch {
		case title == "" || tooLong(title, maxTitle):
			msg = "err_title"
		case tooLong(desc, maxDescription):
			msg = "err_desc"
		default:
			err = s.store.UpdateSettings(id, title, desc, r.FormValue("allow_maybe") == "1", r.FormValue("closed") == "1")
		}
	case "add_options":
		var labels []string
		labels, msg = s.parseOptions(d.Poll.Kind, r, nil)
		if msg == "" {
			if len(d.Options)+len(labels) > maxOptions {
				msg = "err_too_many"
			} else {
				err = s.store.AddOptions(id, labels)
			}
		}
	case "delete_option":
		err = s.store.DeleteOption(id, num("option"))
		if errors.Is(err, ErrLastOption) {
			err, msg = nil, "err_last"
		}
	case "delete_participant":
		err = s.store.DeleteParticipant(id, num("participant"))
	case "invite":
		var names []string
		seen := map[string]bool{}
		for _, line := range strings.Split(r.FormValue("names"), "\n") {
			n := cleanLine(line)
			if n == "" || seen[strings.ToLower(n)] {
				continue
			}
			if tooLong(n, maxName) {
				msg = "err_name"
				break
			}
			seen[strings.ToLower(n)] = true
			names = append(names, n)
		}
		if msg == "" && len(names) > 50 {
			msg = "err_too_many"
		}
		if msg == "" && len(names) > 0 {
			_, err = s.store.InviteParticipants(id, names)
			if errors.Is(err, ErrClosed) {
				err, msg = nil, "err_closed"
			}
		}
	case "set_password", "remove_password":
		role := r.FormValue("role")
		if role != roleParticip && role != roleAdmin {
			s.notFound(w, r)
			return
		}
		hash := ""
		if r.PathValue("action") == "set_password" {
			pw := r.FormValue("password")
			switch {
			case len(pw) < pwMinLen || len(pw) > pwMaxLen:
				msg = "err_pw_short"
			case pw != r.FormValue("password2"):
				msg = "err_pw_mismatch"
			default:
				hash, err = hashPassword(pw)
			}
		}
		if err == nil && msg == "" {
			if err = s.store.SetPassword(id, role, hash); err == nil {
				// Keep the current browser unlocked after a change.
				d.Poll.PwParticipant, d.Poll.PwAdmin = pollHash2(d.Poll, role, hash)
				s.setUnlock(w, r, &d.Poll, role)
			}
		}
	case "delete_comment":
		err = s.store.DeleteComment(id, num("comment"))
	case "delete_poll":
		if err = s.store.DeletePoll(id); err == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	default:
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if msg != "" {
		d, err := s.store.Load(id)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		v := s.adminView(r, d)
		v.Status = http.StatusUnprocessableEntity
		v.Error = v.T(msg)
		s.render(w, "admin", v)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// pollHash2 returns the poll's two hashes with the given role's hash replaced.
func pollHash2(p Poll, role, hash string) (participant, admin string) {
	participant, admin = p.PwParticipant, p.PwAdmin
	if role == roleAdmin {
		admin = hash
	} else {
		participant = hash
	}
	return
}

// SetContact sets the operator contact shown in the accessibility statement.
func (s *Server) SetContact(c string) { s.contact = strings.TrimSpace(c) }

// accessibility serves the accessibility statement (German or English).
func (s *Server) accessibility(w http.ResponseWriter, r *http.Request) {
	v := s.newView(r)
	v.Title = v.T("a11y_link")
	v.Contact = s.contact
	page := "a11y_en"
	if v.Lang == "de" {
		page = "a11y_de"
	}
	s.render(w, page, v)
}

// ContactHref turns the configured contact into a link target ("" if it is plain text).
func (v *View) ContactHref() string {
	switch {
	case strings.HasPrefix(v.Contact, "http://") || strings.HasPrefix(v.Contact, "https://"):
		return v.Contact
	case strings.Contains(v.Contact, "@") && !strings.ContainsAny(v.Contact, " \t"):
		return "mailto:" + v.Contact
	}
	return ""
}
