// Package stdb talks to a SpacetimeDB server over its HTTP API: SQL queries and statements
// (/v1/database/<db>/sql) and reducer calls (/v1/database/<db>/call/<reducer>). LyraCore, the
// SpacetimeDB 1.12.1 server, keeps its world in such databases (internal/lyracore).
package stdb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a SpacetimeDB server: its base URL (http://127.0.0.1:3000) and the token its
// requests are sent with (a database owner's, for SQL that writes).
type Client struct {
	URL   string
	Token string
	// HTTP is the client requests go through; nil for one with a 60 second timeout.
	HTTP *http.Client
}

var defaultHTTP = &http.Client{Timeout: 60 * time.Second}

// ReducerFailedStatus is SpacetimeDB's HTTP status for a reducer that returned an error.
const ReducerFailedStatus = 530

type HTTPError struct {
	StatusCode int
	Status     string
	Path       string
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("spacetimedb %s: %s: %s", e.Path, e.Status, e.Body)
}

// Table is one statement's result: its column names and rows, each value as the server's JSON,
// numbers kept exact (a u64 does not fit a float64).
type Table struct {
	Columns []string
	Rows    [][]json.RawMessage
}

// Col is the index of the named column, -1 if the table has none.
func (t Table) Col(name string) int {
	for i, c := range t.Columns {
		if c == name {
			return i
		}
	}
	return -1
}

// SQL runs one or more statements against the database and returns a table per statement.
func (c *Client) SQL(db, query string) ([]Table, error) {
	body, err := c.post("/v1/database/"+url.PathEscape(db)+"/sql", "text/plain", []byte(query))
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Schema struct {
			Elements []struct {
				Name struct {
					Some string `json:"some"`
				} `json:"name"`
			} `json:"elements"`
		} `json:"schema"`
		Rows [][]json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("spacetimedb sql: %w", err)
	}
	out := make([]Table, len(raw))
	for i, r := range raw {
		for _, e := range r.Schema.Elements {
			out[i].Columns = append(out[i].Columns, e.Name.Some)
		}
		out[i].Rows = r.Rows
	}
	return out, nil
}

// Query runs a single SELECT and returns its table.
func (c *Client) Query(db, query string) (Table, error) {
	ts, err := c.SQL(db, query)
	if err != nil {
		return Table{}, err
	}
	if len(ts) == 0 {
		return Table{}, nil
	}
	return ts[0], nil
}

// Call calls a reducer with its arguments, in the reducer's parameter order.
func (c *Client) Call(db, reducer string, args ...any) error {
	if args == nil {
		args = []any{}
	}
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	_, err = c.post("/v1/database/"+url.PathEscape(db)+"/call/"+url.PathEscape(reducer), "application/json", b)
	return err
}

// Ping says whether the server answers.
func (c *Client) Ping() error {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(c.URL, "/")+"/v1/ping", nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("spacetimedb ping: %s", res.Status)
	}
	return nil
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTP
}

func (c *Client) post(path, contentType string, body []byte) ([]byte, error) {
	if c.URL == "" {
		return nil, errors.New("no SpacetimeDB URL is set")
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(c.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("spacetimedb: %w", err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("spacetimedb: %w", err)
	}
	if res.StatusCode/100 != 2 {
		return nil, &HTTPError{StatusCode: res.StatusCode, Status: res.Status, Path: path, Body: strings.TrimSpace(string(out))}
	}
	return out, nil
}

// Uint reads an unsigned integer value.
func Uint(v json.RawMessage) (uint64, error) {
	return strconv.ParseUint(strings.TrimSpace(string(v)), 10, 64)
}

// Int reads a signed integer value.
func Int(v json.RawMessage) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64)
}

// Float reads a float value. SpacetimeDB writes the non-finite ones as strings or null.
func Float(v json.RawMessage) (float64, error) {
	s := strings.Trim(strings.TrimSpace(string(v)), `"`)
	switch s {
	case "NaN", "null":
		return math.NaN(), nil
	case "inf", "Infinity":
		return math.Inf(1), nil
	case "-inf", "-Infinity":
		return math.Inf(-1), nil
	}
	return strconv.ParseFloat(s, 64)
}

// String reads a string value.
func String(v json.RawMessage) (string, error) {
	var s string
	err := json.Unmarshal(v, &s)
	return s, err
}

// Bool reads a bool value.
func Bool(v json.RawMessage) (bool, error) {
	var b bool
	err := json.Unmarshal(v, &b)
	return b, err
}

// Timestamp reads a Timestamp value, which the API writes as a one-element product of the
// microseconds since the Unix epoch.
func Timestamp(v json.RawMessage) (int64, error) {
	var p []json.Number
	if err := json.Unmarshal(v, &p); err != nil || len(p) != 1 {
		return 0, fmt.Errorf("timestamp %s: not a [micros] product", v)
	}
	return p[0].Int64()
}

// Row reads a table's rows by column name, keeping the first error: a reader over one row.
type Row struct {
	t   *Table
	r   []json.RawMessage
	Err error
}

// Each calls f with a Row reader for each of the table's rows, stopping at the first error.
func (t *Table) Each(f func(r *Row) error) error {
	for _, raw := range t.Rows {
		r := Row{t: t, r: raw}
		if err := f(&r); err != nil {
			return err
		}
		if r.Err != nil {
			return r.Err
		}
	}
	return nil
}

func (r *Row) val(name string) (json.RawMessage, bool) {
	if r.Err != nil {
		return nil, false
	}
	i := r.t.Col(name)
	if i < 0 || i >= len(r.r) {
		r.Err = fmt.Errorf("spacetimedb: no column %q", name)
		return nil, false
	}
	return r.r[i], true
}

func (r *Row) fail(name string, err error) {
	if err != nil && r.Err == nil {
		r.Err = fmt.Errorf("spacetimedb: column %q: %w", name, err)
	}
}

// Uint is the named column as an unsigned integer.
func (r *Row) Uint(name string) uint64 {
	v, ok := r.val(name)
	if !ok {
		return 0
	}
	n, err := Uint(v)
	r.fail(name, err)
	return n
}

// Int is the named column as a signed integer.
func (r *Row) Int(name string) int64 {
	v, ok := r.val(name)
	if !ok {
		return 0
	}
	n, err := Int(v)
	r.fail(name, err)
	return n
}

// Float is the named column as a float.
func (r *Row) Float(name string) float64 {
	v, ok := r.val(name)
	if !ok {
		return 0
	}
	n, err := Float(v)
	r.fail(name, err)
	return n
}

// String is the named column as a string.
func (r *Row) String(name string) string {
	v, ok := r.val(name)
	if !ok {
		return ""
	}
	s, err := String(v)
	r.fail(name, err)
	return s
}

// Timestamp is the named column as microseconds since the Unix epoch.
func (r *Row) Timestamp(name string) int64 {
	v, ok := r.val(name)
	if !ok {
		return 0
	}
	n, err := Timestamp(v)
	r.fail(name, err)
	return n
}

// Quote quotes a string for a SQL statement.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// FloatLit writes a float for a SQL statement, keeping a float32's every digit.
func FloatLit(v float32) string {
	s := strconv.FormatFloat(float64(v), 'f', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
