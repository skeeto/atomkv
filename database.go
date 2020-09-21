package main

import (
	"context"
	"strings"
	"time"
)

const (
	defaultBuflen = 64
	defaultExpiry = time.Hour * 24 * 7
)

type Update struct {
	Key      string
	Value    string
	Revision int
}

type entry struct {
	value    string
	revision int
}

type requestGet struct {
	key  string
	resp chan<- struct {
		value    string
		revision int
		ok       bool
	}
}

type requestSet struct {
	key   string
	value string
}

type requestUpdate struct {
	key      string
	value    string
	revision int
	resp     chan<- bool
}

type requestSubscribe struct {
	path string
	ch   chan Update
}

type requestUnsubscribe struct {
	ch <-chan Update
}

type subscriber struct {
	path string
	ch   chan Update
}

type Database struct {
	values        map[string]entry
	subscriptions map[string]map[chan Update]struct{}
	subscribers   map[<-chan Update]subscriber
	buflen        int
	expiry        time.Duration

	chGet         chan requestGet
	chSet         chan requestSet
	chUpdate      chan requestUpdate
	chDelete      chan string
	chSubscribe   chan requestSubscribe
	chUnsubscribe chan requestUnsubscribe
	chStop        chan struct{}
}

func NewDatabase(buflen int, expiry time.Duration) *Database {
	if buflen == 0 {
		buflen = defaultBuflen
	}
	if expiry == 0 {
		expiry = defaultExpiry
	}

	database := Database{
		values:        make(map[string]entry),
		subscriptions: make(map[string]map[chan Update]struct{}),
		subscribers:   make(map[<-chan Update]subscriber),
		buflen:        buflen,
		expiry:        expiry,

		chGet:         make(chan requestGet),
		chSet:         make(chan requestSet),
		chUpdate:      make(chan requestUpdate),
		chDelete:      make(chan string),
		chSubscribe:   make(chan requestSubscribe),
		chUnsubscribe: make(chan requestUnsubscribe),
		chStop:        make(chan struct{}),
	}
	go database.dispatch()
	return &database
}

func (d *Database) dispatch() {
	for {
		select {
		case r := <-d.chGet:
			e, ok := d.get(r.key)
			r.resp <- struct {
				value    string
				revision int
				ok       bool
			}{e.value, e.revision, ok}
		case r := <-d.chSet:
			d.set(r.key, r.value)
		case r := <-d.chUpdate:
			r.resp <- d.update(r.key, r.value, r.revision)
		case key := <-d.chDelete:
			delete(d.values, key)
		case r := <-d.chSubscribe:
			d.subscribe(r.path, r.ch)
		case r := <-d.chUnsubscribe:
			d.unsubscribe(r.ch)
		case <-d.chStop:
			return
		}
	}
}

func (d *Database) get(key string) (entry, bool) {
	e, ok := d.values[key]
	return e, ok
}

func (d *Database) set(key string, value string) {
	e, ok := d.values[key]
	if ok {
		e.revision++
	} else {
		go d.expire(key)
	}
	e.value = value
	d.values[key] = e
	d.notify(key, e)
}

func (d *Database) update(key string, value string, revision int) bool {
	e, ok := d.values[key]
	if !ok {
		e.revision = -1
	}
	if e.revision+1 != revision {
		return false
	}
	if !ok {
		go d.expire(key)
	}
	e = entry{value, revision}
	d.values[key] = e
	d.notify(key, e)
	return true
}

func (d *Database) expire(key string) {
	t := time.NewTimer(d.expiry)
	ch := d.Subscribe(key)
	for {
		select {
		case <-ch:
			if !t.Stop() {
				<-t.C
			}
			t.Reset(d.expiry)
		case <-t.C:
			d.Delete(key)
		}
	}
}

func (d *Database) subscribe(path string, ch chan Update) {
	m, ok := d.subscriptions[path]
	if !ok {
		m = make(map[chan Update]struct{})
		d.subscriptions[path] = m
	}
	m[ch] = struct{}{}
	d.subscribers[ch] = subscriber{path: path, ch: ch}
}

func (d *Database) unsubscribe(ch <-chan Update) {
	sub := d.subscribers[ch]
	delete(d.subscribers, ch)
	m := d.subscriptions[sub.path]
	delete(m, sub.ch)
	if len(m) == 0 {
		delete(d.subscriptions, sub.path)
	}
}

func (d *Database) notify(key string, e entry) {
	seen := make(map[chan Update]struct{})
	part := key
	for {
		if m, ok := d.subscriptions[part]; ok {
			for s := range m {
				if _, ok := seen[s]; !ok {
					seen[s] = struct{}{}
					select {
					case s <- Update{Key: key, Value: e.value, Revision: e.revision}:
					default: // drop
					}
				}
			}
		}
		slash := strings.LastIndexByte(part[:len(part)-1], '/')
		if slash == -1 {
			break
		}
		part = part[:slash+1]
	}
}

func validate(key string) {
	if len(key) == 0 || key[0] != '/' {
		panic("invalid key")
	}
}

func (d *Database) Get(key string) (string, int, bool) {
	validate(key)
	resp := make(chan struct {
		value    string
		revision int
		ok       bool
	})
	d.chGet <- requestGet{key, resp}
	e := <-resp
	return e.value, e.revision, e.ok
}

func (d *Database) Set(key string, value string) {
	validate(key)
	d.chSet <- requestSet{key: key, value: value}
}

func (d *Database) Update(key string, value string, revision int) bool {
	validate(key)
	resp := make(chan bool)
	d.chUpdate <- requestUpdate{
		key:      key,
		value:    value,
		revision: revision,
		resp:     resp,
	}
	return <-resp
}

func (d *Database) Delete(key string) {
	d.chDelete <- key
}

func (d *Database) Subscribe(path string) <-chan Update {
	validate(path)
	ch := make(chan Update, d.buflen)
	d.chSubscribe <- requestSubscribe{path: path, ch: ch}
	return ch
}

func (d *Database) Unsubscribe(ch <-chan Update) {
	d.chUnsubscribe <- requestUnsubscribe{ch: ch}
}

func (d *Database) Close() {
	close(d.chStop)
}

type key int

func (d *Database) NewContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, key(0), d)
}

func FromContext(ctx context.Context) (*Database, bool) {
	d, ok := ctx.Value(key(0)).(*Database)
	return d, ok
}
