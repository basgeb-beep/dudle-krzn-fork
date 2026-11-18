package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrNameTaken  = errors.New("name taken")
	ErrLastOption = errors.New("cannot remove last option")
	ErrClosed     = errors.New("poll is closed")
)

// Vote values.
const (
	VoteNo    = 0
	VoteYes   = 1
	VoteMaybe = 2
)

const (
	KindDate = "date"
	KindText = "text"
)

type Poll struct {
	ID          string
	Title       string
	Description string
	Kind        string
	AdminToken  string
	Closed      bool
	AllowMaybe  bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Option struct {
	ID    int64
	Label string
	Sort  int
}

type Participant struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	Votes     map[int64]int // option id -> value
}

type Comment struct {
	ID        int64
	Name      string
	Text      string
	CreatedAt time.Time
}

type PollData struct {
	Poll         Poll
	Options      []Option
	Participants []Participant
	Comments     []Comment
}

const schema = `
CREATE TABLE IF NOT EXISTS polls (
	id          TEXT PRIMARY KEY,
	title       TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	kind        TEXT NOT NULL CHECK (kind IN ('date','text')),
	admin_token TEXT NOT NULL,
	closed      INTEGER NOT NULL DEFAULT 0,
	allow_maybe INTEGER NOT NULL DEFAULT 1,
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS options (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	poll_id TEXT NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
	label   TEXT NOT NULL,
	sort    INTEGER NOT NULL,
	UNIQUE (poll_id, label)
);
CREATE TABLE IF NOT EXISTS participants (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	poll_id    TEXT NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS participants_poll ON participants(poll_id);
CREATE TABLE IF NOT EXISTS votes (
	participant_id INTEGER NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
	option_id      INTEGER NOT NULL REFERENCES options(id) ON DELETE CASCADE,
	value          INTEGER NOT NULL CHECK (value IN (0,1,2)),
	PRIMARY KEY (participant_id, option_id)
);
CREATE TABLE IF NOT EXISTS comments (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	poll_id    TEXT NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	text       TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS comments_poll ON comments(poll_id);
`

type Store struct {
	db *sql.DB
}

// OpenStore opens (and migrates) the SQLite database at path.
func OpenStore(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) CreatePoll(p *Poll, labels []string) error {
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO polls (id,title,description,kind,admin_token,closed,allow_maybe,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Title, p.Description, p.Kind, p.AdminToken, b2i(p.Closed), b2i(p.AllowMaybe), now.Unix(), now.Unix())
	if err != nil {
		return err
	}
	for i, l := range labels {
		if _, err := tx.Exec(`INSERT INTO options (poll_id,label,sort) VALUES (?,?,?)`, p.ID, l, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetPoll(id string) (*Poll, error) {
	var p Poll
	var closed, maybe int
	var created, updated int64
	err := s.db.QueryRow(`SELECT id,title,description,kind,admin_token,closed,allow_maybe,created_at,updated_at
		FROM polls WHERE id=?`, id).
		Scan(&p.ID, &p.Title, &p.Description, &p.Kind, &p.AdminToken, &closed, &maybe, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Closed, p.AllowMaybe = closed != 0, maybe != 0
	p.CreatedAt, p.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	return &p, nil
}

// Load fetches a poll with all options, participants, votes and comments.
func (s *Store) Load(id string) (*PollData, error) {
	p, err := s.GetPoll(id)
	if err != nil {
		return nil, err
	}
	d := &PollData{Poll: *p}

	rows, err := s.db.Query(`SELECT id,label,sort FROM options WHERE poll_id=? ORDER BY sort,id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.ID, &o.Label, &o.Sort); err != nil {
			rows.Close()
			return nil, err
		}
		d.Options = append(d.Options, o)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	if p.Kind == KindDate {
		sort.SliceStable(d.Options, func(i, j int) bool { return d.Options[i].Label < d.Options[j].Label })
	}

	idx := map[int64]int{}
	rows, err = s.db.Query(`SELECT id,name,created_at FROM participants WHERE poll_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pt Participant
		var ts int64
		if err := rows.Scan(&pt.ID, &pt.Name, &ts); err != nil {
			rows.Close()
			return nil, err
		}
		pt.CreatedAt = time.Unix(ts, 0)
		pt.Votes = map[int64]int{}
		idx[pt.ID] = len(d.Participants)
		d.Participants = append(d.Participants, pt)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}

	rows, err = s.db.Query(`SELECT v.participant_id, v.option_id, v.value FROM votes v
		JOIN participants p ON p.id = v.participant_id WHERE p.poll_id=?`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pid, oid int64
		var v int
		if err := rows.Scan(&pid, &oid, &v); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := idx[pid]; ok {
			d.Participants[i].Votes[oid] = v
		}
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}

	rows, err = s.db.Query(`SELECT id,name,text,created_at FROM comments WHERE poll_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Comment
		var ts int64
		if err := rows.Scan(&c.ID, &c.Name, &c.Text, &ts); err != nil {
			rows.Close()
			return nil, err
		}
		c.CreatedAt = time.Unix(ts, 0)
		d.Comments = append(d.Comments, c)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return d, nil
}

func closeRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	return rows.Close()
}

func touch(tx *sql.Tx, pollID string) error {
	_, err := tx.Exec(`UPDATE polls SET updated_at=? WHERE id=?`, time.Now().Unix(), pollID)
	return err
}

// SaveVote creates (pid == 0) or updates a participant and replaces their votes.
// Votes for unknown options are ignored. It returns the participant id.
func (s *Store) SaveVote(pollID string, pid int64, name string, votes map[int64]int) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var closed int
	if err := tx.QueryRow(`SELECT closed FROM polls WHERE id=?`, pollID).Scan(&closed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if closed != 0 {
		return 0, ErrClosed
	}

	valid := map[int64]bool{}
	rows, err := tx.Query(`SELECT id FROM options WHERE poll_id=?`, pollID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		valid[id] = true
	}
	if err := closeRows(rows); err != nil {
		return 0, err
	}

	existing := map[int64]string{}
	rows, err = tx.Query(`SELECT id,name FROM participants WHERE poll_id=?`, pollID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return 0, err
		}
		existing[id] = n
	}
	if err := closeRows(rows); err != nil {
		return 0, err
	}

	for id, n := range existing {
		if id != pid && strings.EqualFold(n, name) {
			return 0, ErrNameTaken
		}
	}

	if pid == 0 {
		res, err := tx.Exec(`INSERT INTO participants (poll_id,name,created_at) VALUES (?,?,?)`, pollID, name, time.Now().Unix())
		if err != nil {
			return 0, err
		}
		if pid, err = res.LastInsertId(); err != nil {
			return 0, err
		}
	} else {
		if _, ok := existing[pid]; !ok {
			return 0, ErrNotFound
		}
		if _, err := tx.Exec(`UPDATE participants SET name=? WHERE id=?`, name, pid); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM votes WHERE participant_id=?`, pid); err != nil {
			return 0, err
		}
	}
	for oid, v := range votes {
		if !valid[oid] || v < VoteNo || v > VoteMaybe {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO votes (participant_id,option_id,value) VALUES (?,?,?)`, pid, oid, v); err != nil {
			return 0, err
		}
	}
	if err := touch(tx, pollID); err != nil {
		return 0, err
	}
	return pid, tx.Commit()
}

func (s *Store) AddComment(pollID, name, text string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM polls WHERE id=?`, pollID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`INSERT INTO comments (poll_id,name,text,created_at) VALUES (?,?,?,?)`,
		pollID, name, text, time.Now().Unix()); err != nil {
		return err
	}
	if err := touch(tx, pollID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteComment(pollID string, id int64) error {
	_, err := s.db.Exec(`DELETE FROM comments WHERE id=? AND poll_id=?`, id, pollID)
	return err
}

func (s *Store) DeleteParticipant(pollID string, id int64) error {
	_, err := s.db.Exec(`DELETE FROM participants WHERE id=? AND poll_id=?`, id, pollID)
	return err
}

// DeleteOption removes an option; the last remaining option cannot be removed.
func (s *Store) DeleteOption(pollID string, id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM options WHERE poll_id=?`, pollID).Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastOption
	}
	if _, err := tx.Exec(`DELETE FROM options WHERE id=? AND poll_id=?`, id, pollID); err != nil {
		return err
	}
	if err := touch(tx, pollID); err != nil {
		return err
	}
	return tx.Commit()
}

// AddOptions appends options, ignoring labels that already exist.
func (s *Store) AddOptions(pollID string, labels []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var next int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sort)+1,0) FROM options WHERE poll_id=?`, pollID).Scan(&next); err != nil {
		return err
	}
	for _, l := range labels {
		res, err := tx.Exec(`INSERT OR IGNORE INTO options (poll_id,label,sort) VALUES (?,?,?)`, pollID, l, next)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			next++
		}
	}
	if err := touch(tx, pollID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateSettings(pollID, title, description string, allowMaybe, closed bool) error {
	res, err := s.db.Exec(`UPDATE polls SET title=?, description=?, allow_maybe=?, closed=?, updated_at=? WHERE id=?`,
		title, description, b2i(allowMaybe), b2i(closed), time.Now().Unix(), pollID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePoll(pollID string) error {
	_, err := s.db.Exec(`DELETE FROM polls WHERE id=?`, pollID)
	return err
}

// DeleteInactive removes polls that were not touched since before.
func (s *Store) DeleteInactive(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM polls WHERE updated_at < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
