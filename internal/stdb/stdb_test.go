package stdb

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSQLReadsRowsByColumn(t *testing.T) {
	var gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/database/lyracore/sql" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		gotQuery, gotAuth = string(b), r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[{"schema":{"elements":[{"name":{"some":"guid"}},{"name":{"some":"x"}},{"name":{"some":"name"}},{"name":{"some":"respawn_at"}}]},
			"rows":[[17379390974119196744,-8954.53,"Rabbit",[6086778182594974]]]}]`))
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL + "/", Token: "tok"}
	tab, err := c.Query("lyracore", "SELECT * FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "SELECT * FROM t" || gotAuth != "Bearer tok" {
		t.Fatalf("sent %q with %q", gotQuery, gotAuth)
	}
	var guid uint64
	var x float64
	var name string
	var at int64
	err = tab.Each(func(r *Row) error {
		guid, x, name, at = r.Uint("guid"), r.Float("x"), r.String("name"), r.Timestamp("respawn_at")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The guid needs all 64 bits: a float64 would round it.
	if guid != 17379390974119196744 || x != -8954.53 || name != "Rabbit" || at != 6086778182594974 {
		t.Fatalf("read %d %v %q %d", guid, x, name, at)
	}
	if err := tab.Each(func(r *Row) error { r.Uint("nope"); return nil }); err == nil {
		t.Fatal("a missing column read without an error")
	}
}

func TestCallSendsArgsAsArray(t *testing.T) {
	var got []json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/database/lyracore/call/debug_teleport" {
			http.Error(w, "no such reducer", http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()

	c := &Client{URL: srv.URL}
	if err := c.Call("lyracore", "debug_teleport", uint64(5), 0, float32(1.5)); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || string(got[0]) != "5" || string(got[2]) != "1.5" {
		t.Fatalf("args %s", got)
	}
	if err := c.Call("lyracore", "missing"); err == nil {
		t.Fatal("a 404 returned no error")
	}
}

func TestLiterals(t *testing.T) {
	if got := Quote("Hogger's"); got != "'Hogger''s'" {
		t.Fatal(got)
	}
	for v, want := range map[float32]string{1: "1.0", -8954.53: "-8954.53", 0.1: "0.1"} {
		if got := FloatLit(v); got != want {
			t.Errorf("FloatLit(%v) = %s, want %s", v, got, want)
		}
	}
}
