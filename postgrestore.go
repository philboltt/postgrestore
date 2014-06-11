// postgrestore is a PostgreSQL backend for storing Gorilla Web Toolkit sessions.
//
// It is heavily influenced by:
//
//  * redistore - https://github.com/boj/redistore
//  * mysqlstore - https://github.com/srinathgs/mysqlstore
//  * pgstore  - https://github.com/antonlindstrom/pgstore
//
// Removes the dependency on gorp from pgstore  in the spirit of limiting dependencies
package postgrestore

import (
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	_ "github.com/lib/pq"
	"log"
	"net/http"
	"time"
)

type PGStore struct {
	db         *sql.DB
	stmtInsert *sql.Stmt
	stmtDelete *sql.Stmt
	stmtUpdate *sql.Stmt
	stmtSelect *sql.Stmt
	Codecs     []securecookie.Codec
	Options    *sessions.Options
}

// NewPostgreSQLStore opens a connection to the given database URL and checks for the eistence of
// a table named "http_sessions".  If none exists, one is created to store session data.
func NewPostgreSQLStore(dbUrl string, path string, maxAge int, keyPairs ...[]byte) (dbStore *PGStore, err error) {
	db, err := sql.Open("postgres", dbUrl)
	if err != nil {
		return nil, err
	}
	// As of Postgres 9.1 could now use IF NOT EXISTS clause in createTable function, but since
	// this works fine for earlier versions too we might as well leave it here.
	stmt := "SELECT EXISTS(SELECT * FROM information_schema.tables WHERE table_name = 'http_sessions');"
	row := db.QueryRow(stmt)
	var exists bool
	row.Scan(&exists)
	if !exists {
		err = createTable(db)
		if err != nil {
			return nil, err
		}
	}
	insQ := "INSERT INTO http_sessions (data, created_on, modified_on, expires_on) VALUES ($1,$2,$3,$4) RETURNING id;"
	stmtInsert, stmtErr := db.Prepare(insQ)
	if stmtErr != nil {
		return nil, stmtErr
	}
	delQ := "DELETE FROM http_sessions WHERE id = $1;"
	stmtDelete, stmtErr := db.Prepare(delQ)
	if stmtErr != nil {
		return nil, stmtErr
	}
	updQ := "UPDATE http_sessions SET data=$1, modified_on=$2 where id=$3;"
	stmtUpdate, stmtErr := db.Prepare(updQ)
	if stmtErr != nil {
		return nil, stmtErr
	}
	selQ := "SELECT data, created_on, modified_on, expires_on FROM http_sessions WHERE id = $1;"
	stmtSelect, stmtErr := db.Prepare(selQ)
	if stmtErr != nil {
		return nil, stmtErr
	}
	return &PGStore{
		db:         db,
		stmtInsert: stmtInsert,
		stmtDelete: stmtDelete,
		stmtUpdate: stmtUpdate,
		stmtSelect: stmtSelect,
		Codecs:     securecookie.CodecsFromPairs(keyPairs...),
		Options: &sessions.Options{
			Path:   path,
			MaxAge: maxAge,
		},
	}, nil
}

func createTable(db *sql.DB) (err error) {
	stmt := "CREATE TABLE http_sessions (" +
		"id SERIAL PRIMARY KEY," +
		"data BYTEA," +
		"created_on TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP," +
		"modified_on TIMESTAMPTZ," +
		"expires_on TIMESTAMPTZ);"
	_, err = db.Exec(stmt)
	if err != nil {
		msg := fmt.Sprintf("Unable to create http_sessions table in the database: %s\n", err.Error())
		return errors.New(msg)
	} else {
		return nil
	}
}

// Closes the connection to the database.
func (dbStore *PGStore) Close() {
	dbStore.stmtSelect.Close()
	dbStore.stmtUpdate.Close()
	dbStore.stmtDelete.Close()
	dbStore.stmtInsert.Close()
	dbStore.db.Close()
}

// Get returns a session for the given name after it has been added to the registry.
func (dbStore *PGStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(dbStore, name)
}

// New returns a new session for the given name without adding it to the registry.
// Note: the "created_on" date is only set when 'Save' is called.  "created_on" is only
// set once.  Changes to this field in the session struct are ignored.
func (dbStore *PGStore) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(dbStore, name)
	session.Options = &sessions.Options{
		Path:   dbStore.Options.Path,
		MaxAge: dbStore.Options.MaxAge,
	}
	session.IsNew = true

	var err error
	if c, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, c.Value, &session.ID, dbStore.Codecs...)
		if err == nil {
			err = dbStore.load(session)
			if err == nil {
				session.IsNew = false
			} else if err == sql.ErrNoRows || err.Error() == "Session expired" {
				// found a matching cookie, but no valid session in the db OR
				// the session has actually expired -
				// treat either case as expired and just create a new session
				err = nil
			}
		}
	}
	return session, err
}

// load fetches a session by ID from the database and decodes its content into session.Values
func (dbStore *PGStore) load(session *sessions.Session) error {
	row := dbStore.stmtSelect.QueryRow(session.ID)
	var encodedData string
	var createdOn, modifiedOn, expiresOn time.Time
	err := row.Scan(&encodedData, &createdOn, &modifiedOn, &expiresOn)
	if err != nil {
		return err
	}
	// check session expiration date
	if expiresOn.Sub(time.Now()) < 0 {
		log.Printf("Session expired on %s, but it is %s now.", expiresOn, time.Now())
		return errors.New("Session expired")
	}
	err = securecookie.DecodeMulti(session.Name(), encodedData, &session.Values, dbStore.Codecs...)
	if err != nil {
		return err
	}
	session.Values["created_on"] = createdOn
	session.Values["modified_on"] = modifiedOn
	session.Values["expires_on"] = expiresOn
	return nil
}

// Save either inserts a new row in the database if none exists for the given session, or updates
// the existing session if it already exists.  It also adds the session ID as a client-side cookie.
func (dbStore *PGStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	var err error
	if session.IsNew {
		if err = dbStore.insert(session); err != nil {
			return err
		}
	} else {
		if err = dbStore.update(session); err != nil {
			return err
		}
	}
	// Keep the session ID key in a cookie so it can be looked up in DB later.
	encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, dbStore.Codecs...)
	if err != nil {
		return err
	}
	http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	return nil
}

// insert creates a new row in the database for the given session.  This is the only
// time that the "created_on" field is set.
func (dbStore *PGStore) insert(session *sessions.Session) error {
	// createdOn is only set once, when the row is saved to the database.
	// this avoids any ambiguity due to caller action.
	var createdOn time.Time
	createdOn = time.Now()
	var modifiedOn time.Time
	modifiedOn = createdOn
	var expiresOn time.Time
	exOn := session.Values["expires_on"]
	if exOn == nil {
		expiresOn = time.Now().Add(time.Second * time.Duration(session.Options.MaxAge))
	} else {
		expiresOn = exOn.(time.Time)
	}
	// clear any timestamp fields from the session data
	delete(session.Values, "created_on")
	delete(session.Values, "expires_on")
	delete(session.Values, "modified_on")
	// string encode the session data and insert it into the database
	encoded, encErr := securecookie.EncodeMulti(session.Name(), session.Values, dbStore.Codecs...)
	if encErr != nil {
		return encErr
	}
	row := dbStore.stmtInsert.QueryRow(encoded, createdOn, modifiedOn, expiresOn)
	var id int64
	err := row.Scan(&id)
	if err != nil {
		return err
	} else {
		session.ID = fmt.Sprintf("%d", id)
		session.IsNew = false
		return nil
	}
}

// update writes encoded session.Values, and an updated "modified_on" timestamp,
// to the database record.  The "created_on" and "expires_on" fields cannot be
// modified using this method.
func (dbStore *PGStore) update(session *sessions.Session) error {
	encoded, err := securecookie.EncodeMulti(session.Name(), session.Values,
		dbStore.Codecs...)
	if err != nil {
		return err
	}
	_, err = dbStore.stmtUpdate.Exec(encoded, time.Now(), session.ID)
	return err
}

// Delete removes the given session from the databae and clears the session id
// from the client cookie.
func (dbStore *PGStore) Delete(w http.ResponseWriter, session *sessions.Session) error {
	// Set cookie to expire.
	options := *session.Options
	options.MaxAge = -1
	http.SetCookie(w, sessions.NewCookie(session.Name(), "", &options))
	// Clear session values.
	for k := range session.Values {
		delete(session.Values, k)
	}
	_, err := dbStore.stmtDelete.Exec(session.ID)
	if err != nil {
		return err
	}
	return nil
}

func init() {
	gob.Register(time.Time{})
}
