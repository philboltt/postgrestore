# postgrestore

[![build status](https://secure.travis-ci.org/philboltt/postgrestore.png)](http://travis-ci.org/philboltt/postgrestore)

A PostgreSQL store for [gorilla/sessions](http://www.gorillatoolkit.org/pkg/sessions) - [src](https://github.com/gorilla/sessions).

## Dependencies

* [github.com/gorilla/securecookie](https://github.com/gorilla/securecookie)
* [github.com/gorilla/sessions](https://github.com/gorilla/sessions)
* [github.com/lib/pq](https://github.com/lib/pq)

## Installation

    go get github.com/philboltt/postgrestore

## Documentation

Available on [godoc.org](http://www.godoc.org/github.com/philboltt/postgrestore).

See http://www.gorillatoolkit.org/pkg/sessions for full documentation on underlying interface.

### Example

    // Fetch new store (path="/", MaxAge=30days, i.e. 60sec*60min*24hrs*30days)
    store := NewPostgreSQLStore("postgres://user:password@server:port/database?sslmode=disable", "/", 60*60*24*30, []byte("secret-key"))
    defer store.Close()

    // Get a session.
    session, err = store.Get(req, "mySessionName")
    if err != nil {
        log.Error(err.Error())
    }

    // Add a value.
    session.Values["foo"] = "bar"

    // Save - creates a new record in the database if none exists, or updates an existing one.
    if err = sessions.Save(req, rsp); err != nil {
        t.Fatalf("Error saving session: %v", err)
    }

    // Delete - removes session record from the database and clears the session ID from the client cookie.
    store.Delete(resp, session)

See the tests for more examples.

## Thanks

This package is largely a rehash of the code and ideas from these fine repos:

* [pgstore](https://github.com/antonlindstrom/pgstore)
* [redistore](https://github.com/boj/redistore)
* [mysqlstore](https://github.com/srinathgs/mysqlstore)

Thank you all for sharing your code!
