package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"
	"unicode"
)

type handler struct{}

func validKey(key string) bool {
	if len(key) < 2 || key[0] != '/' {
		return false
	}
	tail := key[1:]
	for i, r := range tail {
		if r == '/' {
			if tail[i-1] == '/' {
				return false
			}
		} else if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return false
		}
	}
	return key[len(key)-1] != '/'
}

func validPath(path string) bool {
	if len(path) == 0 {
		return false
	}
	if path[len(path)-1] == '/' {
		return validKey(path + "_")
	}
	return validKey(path)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	hdr := w.Header()
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Content-Type", "application/json")

	key := r.URL.Path
	log.Printf("GET %s %s", r.RemoteAddr, key)
	if !validKey(key) {
		http.Error(w, "invalid key", 400)
		return
	}

	db, _ := FromContext(r.Context())
	value, revision, ok := db.Get(key)
	if !ok {
		http.Error(w, "no such key", 404)
		return
	}
	hdr.Set("X-Revision", strconv.Itoa(revision))
	io.WriteString(w, value)
}

func normalize(r io.Reader) (string, bool) {
	buf, err := ioutil.ReadAll(r)
	if err != nil {
		return "", false
	}

	var data interface{}
	if json.Unmarshal(buf, &data) != nil {
		return "", false
	}
	buf, _ = json.Marshal(data)
	return string(buf), true
}

func (h *handler) post(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	key := r.URL.Path
	log.Printf("POST %s %s", r.RemoteAddr, key)
	if !validKey(key) {
		http.Error(w, "invalid key", 400)
		return
	}

	value, ok := normalize(r.Body)
	if !ok {
		http.Error(w, "invalid JSON", 400)
		return
	}
	db, _ := FromContext(r.Context())
	db.Set(key, value)
}

func (h *handler) put(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	key := r.URL.Path
	log.Printf("PUT %s %s", r.RemoteAddr, key)
	if !validKey(key) {
		http.Error(w, "invalid key", 400)
		return
	}

	xrevision := r.Header.Get("X-Revision")
	if xrevision == "" {
		http.Error(w, "missing revision", 400)
		return
	}
	revision, err := strconv.Atoi(xrevision)
	if err != nil {
		http.Error(w, "invalid revision", 400)
		return
	}

	json, ok := normalize(r.Body)
	if !ok {
		http.Error(w, "invalid JSON", 400)
		return
	}

	db, _ := FromContext(r.Context())
	if !db.Update(key, json, revision) {
		http.Error(w, "revision conflict", 409)
		return
	}
}

func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	hdr := w.Header()
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Transfer-Encoding", "chunked")

	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "unsupported", 500)
		return
	}

	path := r.URL.Path
	log.Printf("SSE %s %s", r.RemoteAddr, path)
	if !validPath(path) {
		http.Error(w, "invalid path/key", 400)
		return
	}

	ctx := r.Context()
	db, _ := FromContext(r.Context())
	ch := db.Subscribe(path)
	defer db.Unsubscribe(ch)
	for {
		select {
		case v := <-ch:
			_, err := fmt.Fprintf(w, "data:%s\nid:%s:%d\n\n", v.Value, v.Key, v.Revision)
			if err != nil {
				return
			}
			f.Flush()
		case <-ctx.Done():
			return
		}
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hdr := w.Header()
	hdr.Set("Access-Control-Allow-Headers", "*")
	hdr.Set("Access-Control-Allow-Methods", "*")
	hdr.Set("Access-Control-Allow-Origin", "*")
	hdr.Set("Access-Control-Expose-Headers", "X-Revision")
	if r.URL.Path == "/" {
		script(w, r)
	} else {
		switch r.Method {
		case "GET":
			switch r.Header.Get("Accept") {
			case "text/event-stream":
				h.events(w, r)
			default:
				h.get(w, r)
			}
		case "POST":
			h.post(w, r)
		case "PUT":
			h.put(w, r)
		case "OPTIONS":
			log.Printf("OPTIONS %s", r.RemoteAddr)
		}
	}
}

func main() {
	addr := flag.String("addr", ":8000", "Server's host address")
	flag.Parse()

	db := NewDatabase(0, 0)
	ctx := db.NewContext(context.Background())
	s := &http.Server{
		Addr:        *addr,
		Handler:     &handler{},
		BaseContext: func(l net.Listener) context.Context { return ctx },
	}
	log.Printf("listening at %s", *addr)
	log.Fatal(s.ListenAndServe())
}
