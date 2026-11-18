package main

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
	Final   bool
	IsFull  bool   // no free place left
	Seats   string // "2/3" when places are limited
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
func viewOption(kind string, o Option, lang string, duration int) OptionView {
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
	if timePart != "" {
		ov.Time = timeRange(timePart, duration)
	}
	switch {
	case lang == "en":
		ov.Full = fmt.Sprintf("%s, %s %d, %d", ov.Weekday, mon, t.Day(), t.Year())
	case dotDay[lang]:
		ov.Full = fmt.Sprintf("%s, %d. %s %d", ov.Weekday, t.Day(), mon, t.Year())
	default:
		ov.Full = fmt.Sprintf("%s, %d %s %d", ov.Weekday, t.Day(), mon, t.Year())
	}
	if timePart != "" {
		ov.Full += ", " + ov.Time
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
		ov := viewOption(d.Poll.Kind, o, lang, d.Poll.Duration)
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

// DurAllDay is the stored duration for "all day" options.
const DurAllDay = 1440

// durationChoices are the quick-pick durations in minutes.
var durationChoices = []int{15, 30, 45, 60, 90, 120, 150, 180}

var errBadDuration = errors.New("bad duration")

// parseDuration reads the duration select ("0", minutes, "1440" or "custom")
// and the free entry in minutes.
func parseDuration(sel, custom string) (int, error) {
	sel = strings.TrimSpace(sel)
	if sel == "" || sel == "0" {
		return 0, nil
	}
	if sel == "custom" {
		sel = strings.TrimSpace(custom)
	}
	n, err := strconv.Atoi(sel)
	if err != nil || n < 1 || n > DurAllDay {
		return 0, errBadDuration
	}
	return n, nil
}

// dotDecimal lists languages that write 1.5 instead of 1,5.
var dotDecimal = map[string]bool{"en": true, "he": true, "ar": true, "sw": true}

// formatDuration renders minutes compactly: "15 min", "1 h", "1,5 h", "1 h 20 min".
func formatDuration(min int, lang string) string {
	switch {
	case min < 60:
		return fmt.Sprintf("%d min", min)
	case min%60 == 0:
		return fmt.Sprintf("%d h", min/60)
	case min%30 == 0:
		sep := ","
		if dotDecimal[lang] {
			sep = "."
		}
		return fmt.Sprintf("%d%s5 h", min/60, sep)
	}
	return fmt.Sprintf("%d h %d min", min/60, min%60)
}

// timeRange turns a start time and a duration into "09:00–10:30". Without a
// duration, or for all-day polls, it returns the start time unchanged.
func timeRange(start string, duration int) string {
	if duration <= 0 || duration >= DurAllDay {
		return start
	}
	t, err := time.Parse("15:04", start)
	if err != nil {
		return start
	}
	return start + "–" + t.Add(time.Duration(duration)*time.Minute).Format("15:04")
}

// validDeadline accepts "" (no deadline) or a date from today on.
func validDeadline(d string) bool {
	if d == "" {
		return true
	}
	if _, err := time.Parse("2006-01-02", d); err != nil {
		return false
	}
	return d >= today()
}

// parseMaxPer reads the optional number of places per option.
func parseMaxPer(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" || v == "0" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 9999 {
		return 0, errors.New("bad number")
	}
	return n, nil
}
