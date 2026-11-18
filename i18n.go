package main

import (
	"fmt"
	"net/http"
	"strings"
)

// Languages of the original dudle, plus English. Codes with an underscore
// (es_AR, pt_BR) are written as BCP 47 tags (es-AR, pt-BR) in HTML.
var supportedLangs = []string{"en", "de", "ar", "bg", "ca", "cs", "da", "eo", "es", "es_AR", "et", "fi", "fr", "gl", "he", "hu", "it", "ln", "nl", "no", "pl", "pt_BR", "ru", "sv", "sw", "tr"}

var langNames = map[string]string{
	"en": "English", "de": "Deutsch", "ar": "العربية", "bg": "Български", "ca": "Català", "cs": "Čeština",
	"da": "Dansk", "eo": "Esperanto", "es": "Español", "es_AR": "Español (Argentina)", "et": "Eesti",
	"fi": "Suomi", "fr": "Français", "gl": "Galego", "he": "עברית", "hu": "Magyar", "it": "Italiano",
	"ln": "Lingála", "nl": "Nederlands", "no": "Norsk", "pl": "Polski", "pt_BR": "Português (Brasil)",
	"ru": "Русский", "sv": "Svenska", "sw": "Kiswahili", "tr": "Türkçe",
}

func isSupported(l string) bool {
	for _, s := range supportedLangs {
		if s == l {
			return true
		}
	}
	return false
}

// matchLang maps a language tag such as "pt-br", "nb" or "de-AT" to a supported code ("" if none).
func matchLang(tag string) string {
	tag = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "-", "_"))
	for _, s := range supportedLangs {
		if strings.ToLower(s) == tag {
			return s
		}
	}
	if i := strings.Index(tag, "_"); i > 0 {
		tag = tag[:i]
	}
	switch tag {
	case "nb", "nn":
		tag = "no"
	}
	for _, s := range supportedLangs {
		if s == tag {
			return s
		}
	}
	return ""
}

// rtlLangs are written right to left.
var rtlLangs = map[string]bool{"ar": true, "he": true}

// detectLang picks the language from ?lang, the cookie, or Accept-Language.
func detectLang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); isSupported(l) {
		return l
	}
	if c, err := r.Cookie("lang"); err == nil && isSupported(c.Value) {
		return c.Value
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		if l := matchLang(strings.SplitN(part, ";", 2)[0]); l != "" {
			return l
		}
	}
	return "en"
}

func tr(lang, key string, args ...any) string {
	m, ok := messages[lang][key]
	if !ok {
		m, ok = messages["en"][key]
	}
	if !ok {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(m, args...)
	}
	return m
}

var messages = map[string]map[string]string{
	"en": {
		"footer_note":         "Open source scheduling – no ads, no tracking.",
		"language":            "Language",
		"home_h1":             "Find the best date, together.",
		"home_lead":           "Create a poll, share the link and see at a glance what works for everyone. No sign-up needed.",
		"f_title":             "Title",
		"f_title_ph":          "e.g. Team dinner",
		"f_desc":              "Description (optional)",
		"f_desc_ph":           "Where, what, anything participants should know …",
		"f_kind":              "What kind of poll?",
		"kind_date":           "Scheduling poll",
		"kind_text":           "Normal poll",
		"f_dates":             "Pick the possible dates",
		"f_dates_hint":        "Click days in the calendar. Add a time if you need one.",
		"f_choices":           "Options, one per line",
		"f_choices_ph":        "Pizza\nSushi\nBurgers",
		"f_maybe":             "Allow “if need be” answers",
		"create":              "Create poll",
		"my_polls":            "Your polls on this device",
		"cal_prev":            "Previous month",
		"cal_next":            "Next month",
		"dp_none":             "No dates selected yet.",
		"dp_time":             "Time (optional)",
		"dp_add_time":         "Add another time",
		"dp_remove":           "Remove",
		"participants":        "Participants",
		"your_name":           "Your name",
		"save":                "Save",
		"cancel":              "Cancel",
		"edit":                "Edit",
		"best_option":         "Best option",
		"best_options":        "Best options",
		"no_votes":            "No votes yet – be the first!",
		"closed":              "Closed",
		"closed_note":         "This poll is closed. Votes can no longer be changed.",
		"share":               "Share this poll",
		"copy":                "Copy",
		"copied":              "Copied!",
		"comments":            "Comments",
		"comment_ph":          "Write a comment …",
		"post":                "Post",
		"no_comments":         "No comments yet.",
		"export_csv":          "Export CSV",
		"feed":                "Atom feed",
		"v_yes":               "Yes",
		"v_maybe":             "If need be",
		"v_no":                "No",
		"total":               "Total",
		"pw_section":          "Access control",
		"pw_participant":      "Participant password",
		"pw_participant_hint": "Visitors must enter this password to see and answer the poll.",
		"pw_admin":            "Admin password",
		"pw_admin_hint":       "Required in addition to the secret admin link to manage the poll.",
		"pw_new":              "New password",
		"pw_repeat":           "Repeat password",
		"pw_set":              "Set password",
		"pw_change":           "Change password",
		"pw_remove":           "Remove password",
		"pw_active":           "Active",
		"pw_inactive":         "Not set",
		"pw_label":            "Password",
		"err_pw_short":        "The password must be at least 8 characters.",
		"err_pw_mismatch":     "The passwords do not match.",
		"err_pw_wrong":        "Wrong password.",
		"err_pw_rate":         "Too many attempts. Please wait a minute.",
		"unlock_title":        "Password required",
		"unlock_lead":         "This poll is protected. Enter the password to continue.",
		"unlock_admin_lead":   "Enter the admin password to manage this poll.",
		"unlock_btn":          "Continue",
		"invite_title":        "Invite participants",
		"invite_hint":         "One name per line. They show up in the table and can fill in their answers.",
		"invite_btn":          "Invite",
		"del_title":           "Delete participant",
		"del_q":               "Do you really want to delete %s?",
		"admin_title":         "Manage poll",
		"admin_intro":         "Keep this page’s address private – anyone who has it can manage the poll.",
		"link_public":         "Link for participants",
		"link_admin":          "Admin link (private)",
		"settings":            "Settings",
		"close_poll":          "Close poll (no more votes)",
		"options":             "Options",
		"add_options":         "Add options",
		"delete":              "Delete",
		"danger":              "Danger zone",
		"delete_poll":         "Delete poll permanently",
		"delete_poll_p":       "This removes the poll, all votes and comments. It cannot be undone.",
		"confirm_delete":      "Really delete this?",
		"saved":               "Saved.",
		"open_poll":           "Open poll",
		"err_title":           "Please enter a title (up to 120 characters).",
		"err_desc":            "The description is too long.",
		"err_options":         "Please add at least one option.",
		"err_too_many":        "Too many options (maximum 60).",
		"err_date":            "One of the dates is invalid.",
		"err_label":           "An option is too long (maximum 100 characters).",
		"err_name":            "Please enter your name (up to 60 characters).",
		"err_name_taken":      "This name is already taken. Please choose another one.",
		"err_closed":          "This poll is closed.",
		"err_last":            "The last option cannot be removed.",
		"err_comment":         "Please enter your name and a comment (up to 1000 characters).",
		"err_generic":         "Something went wrong. Please try again.",
		"not_found_h":         "Page not found",
		"not_found_p":         "This poll doesn’t exist (anymore). Polls are removed after a period of inactivity.",
		"back_home":           "Back to start",
		"dp_copy_all":         "Apply these times to all days",
		"more_settings":       "More settings",
		"imprint":             "Legal notice",
		"privacy":             "Privacy",
		"err_not_yours":       "You can only change your own entry. Please ask the organiser if something needs to be corrected.",
		"dur_label":           "Duration",
		"dur_none":            "Not specified",
		"dur_allday":          "All day",
		"dur_custom":          "Custom …",
		"dur_custom_min":      "Duration in minutes",
		"dur_info":            "Duration: %s",
		"err_duration":        "Please enter a duration between 1 and 1440 minutes.",
		"skip":                "Skip to content",
		"a11y_link":           "Accessibility",
		"dp_count":            "Selected: {n}",
	},
	"de": {
		"footer_note":         "Open-Source-Terminfindung – keine Werbung, kein Tracking.",
		"language":            "Sprache",
		"home_h1":             "Gemeinsam den besten Termin finden.",
		"home_lead":           "Umfrage erstellen, Link teilen und auf einen Blick sehen, was für alle passt. Ohne Anmeldung.",
		"f_title":             "Titel",
		"f_title_ph":          "z. B. Teamessen",
		"f_desc":              "Beschreibung (optional)",
		"f_desc_ph":           "Wo, was, alles was die Teilnehmenden wissen sollten …",
		"f_kind":              "Welche Art von Umfrage?",
		"kind_date":           "Terminumfrage",
		"kind_text":           "Normale Umfrage",
		"f_dates":             "Mögliche Termine auswählen",
		"f_dates_hint":        "Klicke Tage im Kalender an. Bei Bedarf kannst du eine Uhrzeit ergänzen.",
		"f_choices":           "Optionen, eine pro Zeile",
		"f_choices_ph":        "Pizza\nSushi\nBurger",
		"f_maybe":             "Antwort „Zur Not“ erlauben",
		"create":              "Umfrage erstellen",
		"my_polls":            "Deine Umfragen auf diesem Gerät",
		"cal_prev":            "Voriger Monat",
		"cal_next":            "Nächster Monat",
		"dp_none":             "Noch keine Termine ausgewählt.",
		"dp_time":             "Uhrzeit (optional)",
		"dp_add_time":         "Weitere Uhrzeit",
		"dp_remove":           "Entfernen",
		"participants":        "Teilnehmende",
		"your_name":           "Dein Name",
		"save":                "Speichern",
		"cancel":              "Abbrechen",
		"edit":                "Bearbeiten",
		"best_option":         "Beste Option",
		"best_options":        "Beste Optionen",
		"no_votes":            "Noch keine Stimmen – mach den Anfang!",
		"closed":              "Geschlossen",
		"closed_note":         "Diese Umfrage ist geschlossen. Stimmen können nicht mehr geändert werden.",
		"share":               "Umfrage teilen",
		"copy":                "Kopieren",
		"copied":              "Kopiert!",
		"comments":            "Kommentare",
		"comment_ph":          "Kommentar schreiben …",
		"post":                "Senden",
		"no_comments":         "Noch keine Kommentare.",
		"export_csv":          "CSV exportieren",
		"feed":                "Atom-Feed",
		"v_yes":               "Ja",
		"v_maybe":             "Zur Not",
		"v_no":                "Nein",
		"total":               "Summe",
		"pw_section":          "Zugriffskontrolle",
		"pw_participant":      "Teilnehmer-Passwort",
		"pw_participant_hint": "Besucher müssen dieses Passwort eingeben, um die Umfrage zu sehen und abzustimmen.",
		"pw_admin":            "Admin-Passwort",
		"pw_admin_hint":       "Wird zusätzlich zum geheimen Admin-Link benötigt, um die Umfrage zu verwalten.",
		"pw_new":              "Neues Passwort",
		"pw_repeat":           "Passwort wiederholen",
		"pw_set":              "Passwort setzen",
		"pw_change":           "Passwort ändern",
		"pw_remove":           "Passwort entfernen",
		"pw_active":           "Aktiv",
		"pw_inactive":         "Nicht gesetzt",
		"pw_label":            "Passwort",
		"err_pw_short":        "Das Passwort muss mindestens 8 Zeichen lang sein.",
		"err_pw_mismatch":     "Die Passwörter stimmen nicht überein.",
		"err_pw_wrong":        "Falsches Passwort.",
		"err_pw_rate":         "Zu viele Versuche. Bitte eine Minute warten.",
		"unlock_title":        "Passwort erforderlich",
		"unlock_lead":         "Diese Umfrage ist geschützt. Gib das Passwort ein, um fortzufahren.",
		"unlock_admin_lead":   "Gib das Admin-Passwort ein, um diese Umfrage zu verwalten.",
		"unlock_btn":          "Weiter",
		"invite_title":        "Teilnehmer einladen",
		"invite_hint":         "Ein Name pro Zeile. Sie erscheinen in der Tabelle und können ihre Antworten eintragen.",
		"invite_btn":          "Einladen",
		"del_title":           "Teilnehmer löschen",
		"del_q":               "Möchtest du %s wirklich löschen?",
		"admin_title":         "Umfrage verwalten",
		"admin_intro":         "Halte die Adresse dieser Seite geheim – wer sie kennt, kann die Umfrage verwalten.",
		"link_public":         "Link für Teilnehmende",
		"link_admin":          "Admin-Link (privat)",
		"settings":            "Einstellungen",
		"close_poll":          "Umfrage schließen (keine Stimmen mehr)",
		"options":             "Optionen",
		"add_options":         "Optionen hinzufügen",
		"delete":              "Löschen",
		"danger":              "Gefahrenzone",
		"delete_poll":         "Umfrage endgültig löschen",
		"delete_poll_p":       "Entfernt die Umfrage samt allen Stimmen und Kommentaren. Das lässt sich nicht rückgängig machen.",
		"confirm_delete":      "Wirklich löschen?",
		"saved":               "Gespeichert.",
		"open_poll":           "Umfrage öffnen",
		"err_title":           "Bitte gib einen Titel ein (maximal 120 Zeichen).",
		"err_desc":            "Die Beschreibung ist zu lang.",
		"err_options":         "Bitte füge mindestens eine Option hinzu.",
		"err_too_many":        "Zu viele Optionen (maximal 60).",
		"err_date":            "Eines der Daten ist ungültig.",
		"err_label":           "Eine Option ist zu lang (maximal 100 Zeichen).",
		"err_name":            "Bitte gib deinen Namen ein (maximal 60 Zeichen).",
		"err_name_taken":      "Dieser Name ist bereits vergeben. Bitte wähle einen anderen.",
		"err_closed":          "Diese Umfrage ist geschlossen.",
		"err_last":            "Die letzte Option kann nicht entfernt werden.",
		"err_comment":         "Bitte gib deinen Namen und einen Kommentar ein (maximal 1000 Zeichen).",
		"err_generic":         "Etwas ist schiefgelaufen. Bitte versuche es erneut.",
		"not_found_h":         "Seite nicht gefunden",
		"not_found_p":         "Diese Umfrage gibt es (nicht mehr). Umfragen werden nach längerer Inaktivität gelöscht.",
		"back_home":           "Zur Startseite",
		"dp_copy_all":         "Uhrzeiten auf alle Tage übertragen",
		"more_settings":       "Weitere Einstellungen",
		"imprint":             "Impressum",
		"privacy":             "Datenschutz",
		"err_not_yours":       "Du kannst nur deinen eigenen Eintrag ändern. Bitte wende dich an die Organisation der Umfrage, wenn etwas korrigiert werden muss.",
		"dur_label":           "Dauer",
		"dur_none":            "Keine Angabe",
		"dur_allday":          "Ganztägig",
		"dur_custom":          "Freie Angabe …",
		"dur_custom_min":      "Dauer in Minuten",
		"dur_info":            "Dauer: %s",
		"err_duration":        "Bitte gib eine Dauer zwischen 1 und 1440 Minuten ein.",
		"skip":                "Zum Inhalt springen",
		"a11y_link":           "Barrierefreiheit",
		"dp_count":            "Ausgewählt: {n}",
	},
}
