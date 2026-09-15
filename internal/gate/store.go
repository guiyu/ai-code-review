package gate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

type Run struct {
	PublicationInvalid bool
	Scope              string
	Attempts           int
	NextAttemptUnix    int64
	Key                string
	PR                 PR
	Result             *Result
	ReviewError        string
	ReportID           int64
	ReportURL          string
	ReportBody         string
	Status             string
	Notified           bool
	Recipient          string
	MessageID          string
	NotifyError        string
}
type Store struct {
	Runs map[string]*Run
	dir  string
	lock *os.File
}

func OpenStore(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	l, e := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(l.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		l.Close()
		return nil, errors.New("controller state is locked")
	}
	s := &Store{Runs: map[string]*Run{}, dir: dir, lock: l}
	b, e := os.ReadFile(filepath.Join(dir, "state.json"))
	if e == nil {
		e = json.Unmarshal(b, &s.Runs)
	}
	if e == nil && s.Runs == nil {
		s.Close()
		return nil, errors.New("invalid null state")
	}
	if e != nil && !os.IsNotExist(e) {
		s.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() {
	if s.lock != nil {
		syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		s.lock.Close()
		s.lock = nil
	}
}
func (s *Store) Save() error {
	b, e := json.MarshalIndent(s.Runs, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(s.dir, ".state-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(name, filepath.Join(s.dir, "state.json")); e != nil {
		return e
	}
	d, e := os.Open(s.dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
