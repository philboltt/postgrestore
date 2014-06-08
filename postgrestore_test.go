package postgrestore

import (
	// "github.com/gorilla/securecookie"
	"encoding/gob"
	"github.com/gorilla/sessions"
	"net/http"
	"net/http/httptest"
	"testing"
)

// URL for travis db instance
const dbUrl = "postgres://postgres@localhost/travis_postgrestore_test"

type FlashMessage struct {
	Type    int
	Message string
}

func Test_PostgreSQLStore(t *testing.T) {

	// Test code derived from github.com/boj/redistore and
	// github.com/srinathgs/mysqlstore
	//
	// -- github.com/boj/redistore attribution
	// Copyright 2012 The Gorilla Authors. All rights reserved.
	// Use of this source code is governed by a BSD-style
	// license that can be found in the LICENSE file.
	//
	// -- github.com/srinathgs/mysqlstore attribution
	// Copyright (c) 2013 Gregor Robinson.
	// Copyright (c) 2013 Brian Jones.
	// All rights reserved.
	// Use of this source code is governed by a MIT-style
	// license that can be found in the LICENSE file.

	// Round 1 ----------------------------------------------------------------

	store, err := NewPostgreSQLStore(dbUrl, "/", 60*60*24*30, []byte("my-secret-key"))
	if err != nil {
		t.Fatalf("failed to open a database connection: %#v", err)
	}
	defer store.Close()

	var req *http.Request
	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)

	// Get a session
	var session *sessions.Session
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("error getting session: %#v", err)
	}
	t.Logf("session id: %s", session.ID)

	// Get a flash
	var flashes []interface{}
	flashes = session.Flashes()
	if len(flashes) != 0 {
		t.Errorf("expected 0 flashes, got %#v", flashes)
	}

	// Add flashes
	session.AddFlash("foo")
	session.AddFlash("bar")

	// Add custom key
	session.AddFlash("baz", "custom_key")

	// Save
	var rsp *httptest.ResponseRecorder
	rsp = httptest.NewRecorder()
	if err = sessions.Save(req, rsp); err != nil {
		t.Fatalf("error saving session: %#v", err.Error())
	}
	t.Logf("session id: %s", session.ID)

	var hdr http.Header
	hdr = rsp.Header()
	var cookies []string
	var ok bool
	cookies, ok = hdr["Set-Cookie"]
	if !ok || len(cookies) != 1 {
		t.Fatalf("no cookies in header: %#v", hdr)
	}
	t.Logf("Cookie: %#v", cookies[0])

	// Round 2 ----------------------------------------------------------------

	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)
	req.Header.Add("Cookie", cookies[0])
	rsp = httptest.NewRecorder()
	// Get a session.
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("Error getting session: %v", err)
	}
	// Check all saved values.
	flashes = session.Flashes()
	if len(flashes) != 2 {
		t.Fatalf("Expected flashes; Got %v", flashes)
	}
	if flashes[0] != "foo" || flashes[1] != "bar" {
		t.Errorf("Expected foo,bar; Got %v", flashes)
	}
	flashes = session.Flashes()
	if len(flashes) != 0 {
		t.Errorf("Expected dumped flashes; Got %v", flashes)
	}
	// Custom key.
	flashes = session.Flashes("custom_key")
	if len(flashes) != 1 {
		t.Errorf("Expected flashes; Got %v", flashes)
	} else if flashes[0] != "baz" {
		t.Errorf("Expected baz; Got %v", flashes)
	}
	flashes = session.Flashes("custom_key")
	if len(flashes) != 0 {
		t.Errorf("Expected dumped flashes; Got %v", flashes)
	}

	session.Options.MaxAge = -1
	// Save.
	if err = sessions.Save(req, rsp); err != nil {
		t.Fatalf("Error saving session: %v", err)
	}

	// Round 3 ----------------------------------------------------------------
	// Custom type

	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)
	rsp = httptest.NewRecorder()
	// Get a session.
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("Error getting session: %v", err)
	}
	// Get a flash.
	flashes = session.Flashes()
	if len(flashes) != 0 {
		t.Errorf("Expected empty flashes; Got %v", flashes)
	}
	// Add some flashes.
	session.AddFlash(&FlashMessage{42, "foo"})
	// Save.
	if err = sessions.Save(req, rsp); err != nil {
		t.Fatalf("Error saving session: %v", err)
	}
	hdr = rsp.Header()
	cookies, ok = hdr["Set-Cookie"]
	t.Logf("%#v", cookies)
	if !ok || len(cookies) != 1 {
		t.Fatalf("No cookies. Header:", hdr)
	}

	// Round 4 ----------------------------------------------------------------
	// Custom type

	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)
	req.Header.Add("Cookie", cookies[0])
	t.Logf("%#v", cookies[0])
	rsp = httptest.NewRecorder()
	// Get a session.
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("Error getting session: %v", err)
	}
	// Check all saved values.
	flashes = session.Flashes()
	if len(flashes) != 1 {
		t.Fatalf("Expected flashes; Got %v", flashes)
	}
	custom := flashes[0].(FlashMessage)
	if custom.Type != 42 || custom.Message != "foo" {
		t.Errorf("Expected %#v, got %#v", FlashMessage{42, "foo"}, custom)
	}

	// Delete session.
	session.Options.MaxAge = -1
	// Save.
	if err = sessions.Save(req, rsp); err != nil {
		t.Fatalf("Error saving session: %v", err)
	}

	// Round 5 ----------------------------------------------------------------
	// MySQLStore Delete session (not exposed by gorilla sessions interface).

	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)
	req.Header.Add("Cookie", cookies[0])
	rsp = httptest.NewRecorder()
	// Get a session.
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("Error getting session: %v", err)
	}

	if err = store.Delete(rsp, session); err != nil {
		t.Fatalf("Error deleting session: %v", err)
	}

	// Get a flash.
	flashes = session.Flashes()
	if len(flashes) != 0 {
		t.Errorf("Expected empty flashes; Got %v", flashes)
	}
	hdr = rsp.Header()
	cookies, ok = hdr["Set-Cookie"]
	if !ok || len(cookies) != 1 {
		t.Fatalf("No cookies. Header:", hdr)
	}
}

func init() {
	gob.Register(FlashMessage{})
}
