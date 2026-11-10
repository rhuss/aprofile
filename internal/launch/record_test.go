package launch_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cc-deck/aprofile/internal/launch"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func TestRecordMissingIsEmpty(t *testing.T) {
	h := testutil.HermeticHome(t)
	rec, err := launch.Load(filepath.Join(h.Root, "nope", "x.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Entries) != 0 || rec.FormatVersion != launch.RecordFormatVersion {
		t.Errorf("missing record = %+v", rec)
	}
}

func TestRecordRoundTripAndWriteOnlyOnChange(t *testing.T) {
	h := testutil.HermeticHome(t)
	path := filepath.Join(h.Root, "data", "managed-launch", "claude", "w.json")
	// Entries in the record's deterministic order (file, then location).
	rec := launch.Record{Entries: []launch.Entry{
		{File: "config.toml", Location: "model_providers.enmaas.auth", Replaced: map[string]string{"env_key": `"OPENAI_API_KEY"`}},
		{File: "settings.json", Location: "apiKeyHelper"},
		{File: "settings.json", Location: "env.ANTHROPIC_MODEL"},
	}}
	changed, err := rec.Save(path)
	if err != nil || !changed {
		t.Fatalf("first save: changed=%v err=%v", changed, err)
	}
	got, err := launch.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Entries, rec.Entries) {
		t.Errorf("round trip = %+v, want %+v", got.Entries, rec.Entries)
	}
	st1, _ := os.Stat(path)
	time.Sleep(10 * time.Millisecond)
	changed, err = got.Save(path)
	if err != nil || changed {
		t.Fatalf("unchanged save: changed=%v err=%v", changed, err)
	}
	st2, _ := os.Stat(path)
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Error("unchanged save rewrote the file")
	}
	// No temp files left behind (atomic write).
	ents, _ := os.ReadDir(filepath.Dir(path))
	if len(ents) != 1 {
		t.Errorf("record dir holds %d entries, want 1", len(ents))
	}
}

func TestRecordEmptyDeletes(t *testing.T) {
	h := testutil.HermeticHome(t)
	path := filepath.Join(h.Root, "rec.json")
	rec := launch.Record{Entries: []launch.Entry{{File: "settings.json", Location: "apiKeyHelper"}}}
	if _, err := rec.Save(path); err != nil {
		t.Fatal(err)
	}
	empty := launch.Record{}
	changed, err := empty.Save(path)
	if err != nil || !changed {
		t.Fatalf("empty save: changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("empty record should delete the file")
	}
	changed, err = empty.Save(path)
	if err != nil || changed {
		t.Errorf("empty save of missing file: changed=%v err=%v", changed, err)
	}
}

func TestRecordMoveAndDelete(t *testing.T) {
	h := testutil.HermeticHome(t)
	from := filepath.Join(h.Root, "a", "x.json")
	to := filepath.Join(h.Root, "b", "y.json")
	rec := launch.Record{Entries: []launch.Entry{{File: "settings.json", Location: "env.A"}}}
	if _, err := rec.Save(from); err != nil {
		t.Fatal(err)
	}
	if err := launch.Move(from, to); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Error("move left the old record")
	}
	if got, _ := launch.Load(to); len(got.Entries) != 1 {
		t.Errorf("moved record = %+v", got)
	}
	if err := launch.Move(from, to); err != nil {
		t.Errorf("moving a missing record should be a no-op: %v", err)
	}
	if err := launch.Delete(to); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Error("delete left the record")
	}
	if err := launch.Delete(to); err != nil {
		t.Errorf("deleting a missing record should be a no-op: %v", err)
	}
}

// The record holds locations and replaced keys only, never the value of an env
// entry (Principle IV): an entry's JSON form has exactly file, location, replaced.
func TestRecordStoresNoValues(t *testing.T) {
	data, err := json.Marshal(launch.Entry{File: "settings.json", Location: "env.ANTHROPIC_MODEL"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	for k := range m {
		if k != "file" && k != "location" && k != "replaced" {
			t.Errorf("record entry has unexpected field %q", k)
		}
	}
	typ := reflect.TypeOf(launch.Entry{})
	for i := 0; i < typ.NumField(); i++ {
		if n := typ.Field(i).Name; n != "File" && n != "Location" && n != "Replaced" {
			t.Errorf("Entry has field %s; the record must never store values", n)
		}
	}
}
