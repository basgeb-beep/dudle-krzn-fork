package main

import (
	"crypto/hmac"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Who may change a participant row: the browser that created it (it keeps the
// row token in a cookie), anyone for unclaimed rows (invited by the admin or
// created before ownership existed; the first save claims them), and the
// poll manager (a browser that opened the admin page).

func ownCookie(pollID string) string     { return "me_" + pollID }
func managerCookie(pollID string) string { return "mgr_" + pollID }

// participant returns the row with the given id, or nil.
func (d *PollData) participant(id int64) *Participant {
	for i := range d.Participants {
		if d.Participants[i].ID == id {
			return &d.Participants[i]
		}
	}
	return nil
}

func (s *Server) canEdit(r *http.Request, p *Poll, pt Participant) bool {
	return pt.Token == "" || s.isManager(r, p) || s.owns(r, p, pt)
}

// owns reports whether this browser created (or claimed) the row.
func (s *Server) owns(r *http.Request, p *Poll, pt Participant) bool {
	if pt.Token == "" {
		return false
	}
	c, err := r.Cookie(ownCookie(p.ID))
	if err != nil {
		return false
	}
	for _, e := range strings.Split(c.Value, ".") {
		id, tok, ok := strings.Cut(e, "-")
		if ok && id == strconv.FormatInt(pt.ID, 10) && hmac.Equal([]byte(tok), []byte(pt.Token)) {
			return true
		}
	}
	return false
}

func (s *Server) cookiePath(pollID string) string { return s.basePath + "/p/" + pollID }

func (s *Server) secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// rememberOwn adds a row token to this browser's list for the poll.
func (s *Server) rememberOwn(w http.ResponseWriter, r *http.Request, pollID string, pid int64, token string) {
	entries := []string{strconv.FormatInt(pid, 10) + "-" + token}
	if c, err := r.Cookie(ownCookie(pollID)); err == nil {
		for _, e := range strings.Split(c.Value, ".") {
			if e != "" && len(entries) < 20 {
				entries = append(entries, e)
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: ownCookie(pollID), Value: strings.Join(entries, "."),
		Path: s.cookiePath(pollID), MaxAge: 400 * 24 * 3600, HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteLaxMode})
}

func (s *Server) managerValue(p *Poll) string { return s.sign("manager", p.ID, p.AdminToken) }

func (s *Server) rememberManager(w http.ResponseWriter, r *http.Request, p *Poll) {
	http.SetCookie(w, &http.Cookie{Name: managerCookie(p.ID), Value: s.managerValue(p),
		Path: s.cookiePath(p.ID), MaxAge: 400 * 24 * 3600, HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteLaxMode})
}

func (s *Server) isManager(r *http.Request, p *Poll) bool {
	c, err := r.Cookie(managerCookie(p.ID))
	return err == nil && hmac.Equal([]byte(c.Value), []byte(s.managerValue(p)))
}

var secretInPath = regexp.MustCompile(`/admin/[^/]+`)

// redactPath hides admin secrets so they never end up in logs.
func redactPath(p string) string { return secretInPath.ReplaceAllString(p, "/admin/[redacted]") }
