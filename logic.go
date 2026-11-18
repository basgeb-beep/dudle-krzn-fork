package main

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxTitle       = 120
	maxDescription = 2000
	maxLabel       = 100
	maxName        = 60
	maxComment     = 1000
	maxOptions     = 60
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// randID returns n random lowercase base32 characters.
func randID(n int) string {
	buf := make([]byte, (n*5+7)/8)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return strings.ToLower(b32.EncodeToString(buf))[:n]
}

// cleanLine trims and strips control characters (single-line input).
func cleanLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// cleanText normalizes newlines and strips control characters except \n.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func tooLong(s string, max int) bool { return utf8.RuneCountInString(s) > max }

var errBadDate = errors.New("invalid date")

// parseDates pairs dates ("2006-01-02") with optional times ("15:04") by index
// and returns sorted, de-duplicated option labels ("2006-01-02" or "2006-01-02T15:04").
func parseDates(dates, times []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for i, d := range dates {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return nil, errBadDate
		}
		label := d
		if i < len(times) {
			if t := strings.TrimSpace(times[i]); t != "" {
				if _, err := time.Parse("15:04", t); err != nil {
					return nil, errBadDate
				}
				label = d + "T" + t
			}
		}
		if !seen[label] {
			seen[label] = true
			out = append(out, label)
		}
	}
	sort.Strings(out)
	return out, nil
}

// parseChoices splits free text into unique, non-empty, trimmed lines.
func parseChoices(text string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		line = cleanLine(line)
		if line == "" {
			continue
		}
		if tooLong(line, maxLabel) {
			return nil, errTooLong
		}
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	return out, nil
}

var errTooLong = errors.New("too long")

// dateInit serialises valid date/time pairs for the date picker's data-initial attribute.
func dateInit(dates, times []string) string {
	var parts []string
	for i, d := range dates {
		d = strings.TrimSpace(d)
		if _, err := time.Parse("2006-01-02", d); err != nil {
			continue
		}
		t := ""
		if i < len(times) {
			t = strings.TrimSpace(times[i])
			if _, err := time.Parse("15:04", t); err != nil {
				t = ""
			}
		}
		parts = append(parts, d+"|"+t)
	}
	return strings.Join(parts, ",")
}

// OptionView is an option prepared for display, including tallies.
type OptionView struct {
	ID      int64
	Label   string
	IsDate  bool
	Weekday string
	Day     string
	Month   string // "Dec 2026"
	Time    string
	Full    string
	Yes     int
	Maybe   int
	No      int
	Best    bool
}

type MonthGroup struct {
	Name string
	Span int
}

func langTable(m map[string][]string, lang string) []string {
	if t, ok := m[lang]; ok {
		return t
	}
	return m["en"]
}

// viewOption formats an option label for display in the given language.
func viewOption(kind string, o Option, lang string) OptionView {
	ov := OptionView{ID: o.ID, Label: o.Label, Full: o.Label}
	if kind != KindDate {
		return ov
	}
	datePart, timePart, _ := strings.Cut(o.Label, "T")
	t, err := time.Parse("2006-01-02", datePart)
	if err != nil {
		return ov
	}
	ov.IsDate = true
	ov.Weekday = langTable(weekdays, lang)[t.Weekday()]
	ov.Day = fmt.Sprint(t.Day())
	mon := langTable(months, lang)[t.Month()-1]
	ov.Month = fmt.Sprintf("%s %d", mon, t.Year())
	ov.Time = timePart
	switch {
	case lang == "en":
		ov.Full = fmt.Sprintf("%s, %s %d, %d", ov.Weekday, mon, t.Day(), t.Year())
	case dotDay[lang]:
		ov.Full = fmt.Sprintf("%s, %d. %s %d", ov.Weekday, t.Day(), mon, t.Year())
	default:
		ov.Full = fmt.Sprintf("%s, %d %s %d", ov.Weekday, t.Day(), mon, t.Year())
	}
	if timePart != "" {
		ov.Full += ", " + timePart
	}
	return ov
}

// Summary is the computed overview of a poll.
type Summary struct {
	Options []OptionView
	Groups  []MonthGroup
	Best    []OptionView
}

// Summarize tallies votes per option and marks the best option(s):
// most "yes", ties broken by most "if need be".
func Summarize(d *PollData, lang string) Summary {
	var s Summary
	for _, o := range d.Options {
		ov := viewOption(d.Poll.Kind, o, lang)
		for _, p := range d.Participants {
			switch p.Votes[o.ID] {
			case VoteYes:
				ov.Yes++
			case VoteMaybe:
				ov.Maybe++
			default:
				ov.No++
			}
		}
		s.Options = append(s.Options, ov)
	}
	bestYes, bestMaybe := 0, 0
	for _, ov := range s.Options {
		if ov.Yes > bestYes || (ov.Yes == bestYes && ov.Maybe > bestMaybe) {
			bestYes, bestMaybe = ov.Yes, ov.Maybe
		}
	}
	for i := range s.Options {
		ov := &s.Options[i]
		if bestYes > 0 && ov.Yes == bestYes && ov.Maybe == bestMaybe {
			ov.Best = true
			s.Best = append(s.Best, *ov)
		}
	}
	if d.Poll.Kind == KindDate {
		for _, ov := range s.Options {
			if n := len(s.Groups); n > 0 && s.Groups[n-1].Name == ov.Month {
				s.Groups[n-1].Span++
			} else {
				s.Groups = append(s.Groups, MonthGroup{Name: ov.Month, Span: 1})
			}
		}
	}
	return s
}
