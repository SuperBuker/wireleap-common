// Copyright (c) 2021 Wireleap

// The ststore package provides a concurrent in-memory sharetoken store which
// is synced to disk after modifications.
package ststore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wireleap/common/api/sharetoken"
	"github.com/wireleap/common/cli/fsdir"
)

type (
	st1map map[string]*sharetoken.T
	st2map map[string]map[string]*sharetoken.T
	st3map map[string]map[string]map[string]*sharetoken.T
)

// DuplicateSTError is returned if a sharetoken which was already seen is
// being added to the store.
var DuplicateSTError = errors.New("duplicate sharetoken")

// T is the type of a sharetoken store.
type T struct {
	m    fsdir.T
	mu   sync.RWMutex
	sts  st3map
	keyf KeyFunc
}

// New initializes a sharetoken store in the directory under the path given by
// the dir argument.
func New(dir string, keyf KeyFunc) (t *T, err error) {
	t = &T{keyf: keyf, sts: st3map{}}
	t.m, err = fsdir.New(dir)

	if err != nil {
		return
	}

	err = filepath.Walk(t.m.Path(), func(path string, info os.FileInfo, err error) error {
		switch {
		case err != nil:
			return err
		case !strings.HasSuffix(info.Name(), ".json"):
			return nil
		}

		st := &sharetoken.T{}
		ps := strings.Split(path, "/")
		n := len(ps)
		err = t.m.Get(st, ps[n-2:n]...)

		if err != nil {
			return err
		}

		return t.Add(st)
	})

	return
}

// Add adds a sharetoken (st) to the map of accumulated sharetokens under the
// keys generated by t.keyf. It returns DuplicateSTError if this sharetoken
// was already seen.
func (t *T) Add(st *sharetoken.T) (err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k1, k2, k3 := t.keyf(st)

	if t.sts[k1] == nil {
		t.sts[k1] = st2map{}
	}

	if t.sts[k1][k2] == nil {
		t.sts[k1][k2] = st1map{}
	}

	if t.sts[k1][k2][k3] == nil {
		t.sts[k1][k2][k3] = st
	} else {
		return DuplicateSTError
	}

	err = t.m.Set(st, k1, k3+".json")

	if err != nil {
		return
	}

	return
}

// Del deletes a sharetoken (st) from the map of accumulated sharetokens
// under the keys generated by t.keyf. It can return errors from attempting to
// delete the file associated with the sharetoken on disk.
func (t *T) Del(st *sharetoken.T) (err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k1, k2, k3 := t.keyf(st)

	switch {
	case t.sts[k1] == nil, t.sts[k1][k2] == nil, t.sts[k1][k2][k3] == nil:
		return
	}

	delete(t.sts[k1][k2], k3)
	err = t.m.Del(k1, k3+".json")

	if err != nil {
		return
	}

	if len(t.sts[k1][k2]) == 0 {
		delete(t.sts[k1], k2)
	}

	if len(t.sts[k1]) == 0 {
		delete(t.sts, k1)
		err = t.m.Del(k1)
	}

	return
}

// Filter returns a list of sharetokens matching the given keys k1 and k2. An
// empty string for either of the keys is assumed to mean "for all values of
// this key".
func (t *T) Filter(k1, k2 string) (r []*sharetoken.T) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	switch {
	case k1 == "" && k2 == "":
		// for all k1, k2
		for _, m1 := range t.sts {
			for _, m2 := range m1 {
				for _, st := range m2 {
					r = append(r, st)
				}
			}
		}
	case k1 == "":
		// for all k1, some k2
		for _, m1 := range t.sts {
			for _, st := range m1[k2] {
				r = append(r, st)
			}
		}
	case k2 == "":
		// for some k1, all k2
		for _, m2 := range t.sts[k1] {
			for _, st := range m2 {
				r = append(r, st)
			}
		}
	default:
		// for some k1, some k2
		for _, st := range t.sts[k1][k2] {
			r = append(r, st)
		}
	}

	return
}

// SettlingAt returns a map with counts of sharetokens currently still being
// settled indexed by relay public key.
func (t *T) SettlingAt(rpk string, utime int64) map[string]int {
	r := map[string]int{}

	for _, st := range t.Filter("", rpk) {
		if st.IsSettlingAt(utime) {
			r[rpk]++
		}
	}

	return r
}
