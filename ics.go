package main

import (
	"net/http"
	"strings"
	"time"
)

// ics serves the fixed date of a poll as an iCalendar file. Times are written
// as floating local times, so calendars show them as entered.
func (s *Server) ics(w http.ResponseWriter, r *http.Request) {
	d := s.loadPoll(w, r)
	if d == nil {
		return
	}
	var label string
	for _, o := range d.Options {
		if o.ID == d.Poll.FinalOption {
			label = o.Label
		}
	}
	if label == "" || d.Poll.Kind != KindDate {
		s.notFound(w, r)
		return
	}
	body, err := icsEvent(d.Poll, label, s.absURL(r, "/p/"+d.Poll.ID), time.Now())
	if err != nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="termin.ics"`)
	w.Write([]byte(body))
}

func icsEvent(p Poll, label, pollURL string, now time.Time) (string, error) {
	datePart, timePart, _ := strings.Cut(label, "T")
	day, err := time.Parse("2006-01-02", datePart)
	if err != nil {
		return "", err
	}
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Dudle-NG//Terminfindung//DE", "CALSCALE:GREGORIAN", "METHOD:PUBLISH",
		"BEGIN:VEVENT",
		"UID:" + p.ID + "-" + strings.ReplaceAll(label, ":", "") + "@dudle-ng",
		"DTSTAMP:" + now.UTC().Format("20060102T150405Z"),
	}
	if timePart == "" || p.Duration >= DurAllDay {
		lines = append(lines,
			"DTSTART;VALUE=DATE:"+day.Format("20060102"),
			"DTEND;VALUE=DATE:"+day.AddDate(0, 0, 1).Format("20060102"))
	} else {
		start, err := time.Parse("2006-01-02T15:04", label)
		if err != nil {
			return "", err
		}
		lines = append(lines, "DTSTART:"+start.Format("20060102T150405"))
		if p.Duration > 0 {
			lines = append(lines, "DTEND:"+start.Add(time.Duration(p.Duration)*time.Minute).Format("20060102T150405"))
		}
	}
	desc := icsText(pollURL)
	if p.Description != "" {
		desc = icsText(p.Description) + `\n\n` + desc
	}
	lines = append(lines, "SUMMARY:"+icsText(p.Title), "DESCRIPTION:"+desc, "URL:"+pollURL, "END:VEVENT", "END:VCALENDAR")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(icsFold(l))
		b.WriteString("\r\n")
	}
	return b.String(), nil
}

// icsText escapes a text value (RFC 5545, section 3.3.11).
func icsText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", "").Replace(s)
}

// icsFold breaks lines longer than 75 octets without splitting characters.
func icsFold(l string) string {
	var b strings.Builder
	n := 0
	for _, r := range l {
		size := len(string(r))
		if n+size > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += size
	}
	return b.String()
}
