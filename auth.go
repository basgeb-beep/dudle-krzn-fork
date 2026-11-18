package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Access control: a poll can have a participant password (needed to see and
// answer the poll) and an admin password (needed in addition to the secret
// admin link). Passwords are stored as salted PBKDF2 hashes. After a correct
// password the browser gets a signed cookie bound to the current hash, so
// changing or removing a password invalidates existing unlocks.

const (
	pwMinLen     = 8
	pwMaxLen     = 200
	pbkdf2Iters  = 200_000
	roleParticip = "participant"
	roleAdmin    = "admin"
)

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iters, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iters, hex.EncodeToString(salt), hex.EncodeToString(key)), nil
}

func checkPassword(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iters, err := strconv.Atoi(parts[1])
	if err != nil || iters < 1 || iters > 10_000_000 {
		return false
	}
	salt, err1 := hex.DecodeString(parts[2])
	want, err2 := hex.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iters, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func cookieName(role, pollID string) string { return "pw_" + role + "_" + pollID }

func (s *Server) sign(role, pollID, hash string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(role + "|" + pollID + "|" + hash))
	return hex.EncodeToString(m.Sum(nil))
}

func pollHash(p *Poll, role string) string {
	if role == roleAdmin {
		return p.PwAdmin
	}
	return p.PwParticipant
}

// unlocked reports whether the request may access the given role's area.
// Without a password set, access is open. The admin cookie also opens the
// participant area.
func (s *Server) unlocked(r *http.Request, p *Poll, role string) bool {
	hash := pollHash(p, role)
	if hash == "" {
		return true
	}
	roles := []string{role}
	if role == roleParticip {
		roles = append(roles, roleAdmin)
	}
	for _, rl := range roles {
		h := pollHash(p, rl)
		if h == "" {
			continue
		}
		c, err := r.Cookie(cookieName(rl, p.ID))
		if err == nil && hmac.Equal([]byte(c.Value), []byte(s.sign(rl, p.ID, h))) {
			return true
		}
	}
	return false
}

func (s *Server) setUnlock(w http.ResponseWriter, r *http.Request, p *Poll, role string) {
	h := pollHash(p, role)
	if h == "" {
		return
	}
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name: cookieName(role, p.ID), Value: s.sign(role, p.ID, h),
		Path: s.basePath + "/p/" + p.ID, MaxAge: 30 * 24 * 3600,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

// renderUnlock shows the password form (HTTP 401).
func (s *Server) renderUnlock(w http.ResponseWriter, r *http.Request, p *Poll, role, errKey string) {
	v := s.newView(r)
	v.Status = http.StatusUnauthorized
	v.Title = v.T("unlock_title")
	v.Poll = p
	v.UnlockRole = role
	v.UnlockAction = "/p/" + p.ID + "/unlock"
	if role == roleAdmin {
		v.UnlockAction = "/p/" + p.ID + "/admin/" + p.AdminToken + "/unlock"
	}
	if errKey != "" {
		v.Error = v.T(errKey)
	}
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, "unlock", v)
}

// unlock handles the password form for both roles.
func (s *Server) unlock(role string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			s.fail(w, r, http.StatusBadRequest, "err_generic", "err_generic")
			return
		}
		p, err := s.store.GetPoll(r.PathValue("id"))
		if err != nil {
			s.notFound(w, r)
			return
		}
		if role == roleAdmin && !tokenOK(r.PathValue("token"), p.AdminToken) {
			s.notFound(w, r)
			return
		}
		back := "/p/" + p.ID
		if role == roleAdmin {
			back = "/p/" + p.ID + "/admin/" + p.AdminToken
		}
		hash := pollHash(p, role)
		if hash == "" {
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		if ok, _ := s.unlockLimiter.allow(clientIP(r, s.trustProxy) + "|" + p.ID); !ok {
			v := s.newView(r)
			v.Status = http.StatusTooManyRequests
			v.Title = v.T("unlock_title")
			v.Poll = p
			v.UnlockRole = role
			v.UnlockAction = r.URL.Path
			v.Error = v.T("err_pw_rate")
			s.render(w, "unlock", v)
			return
		}
		pw := r.FormValue("password")
		if len(pw) > pwMaxLen || !checkPassword(pw, hash) {
			s.renderUnlock(w, r, p, role, "err_pw_wrong")
			return
		}
		s.setUnlock(w, r, p, role)
		http.Redirect(w, r, back, http.StatusSeeOther)
	}
}

func tokenOK(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
