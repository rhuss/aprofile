package layer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

func treatmentOf(entries []layer.EntryTreatment, entry string) *layer.EntryTreatment {
	for i := range entries {
		if entries[i].Entry == entry {
			return &entries[i]
		}
	}
	return nil
}

// spec 003 T012: a member's history links are linked treatments, not leaks.
func TestTreatmentsHistoryMember(t *testing.T) {
	h := testutil.HermeticHome(t)
	def := loadToy(t, toyHistDef)
	work := fakeStore{root: filepath.Join(h.DataHome, "store"), name: "work"}
	dir := filepath.Join(h.DataHome, "p")
	testutil.WriteFile(t, filepath.Join(dir, "sessions", "real.jsonl"), "{}") // real entry: drift
	if err := os.Symlink(work.EntryPath("history.jsonl"), filepath.Join(dir, "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(h.Root, "elsewhere"), filepath.Join(dir, "todos")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &profile.Profile{Harness: "toy", Name: "p", Share: profile.ShareAll, HistoryGroup: "work"}
	got, err := layer.TreatmentsWith(def, p, h.Root, dir, work)
	if err != nil {
		t.Fatal(err)
	}
	hist := treatmentOf(got, "history.jsonl")
	if hist == nil || hist.Treatment != layer.TreatmentLinked || hist.Reason != "history group work" || hist.Status != layer.StatusOK {
		t.Errorf("history.jsonl = %+v", hist)
	}
	if s := treatmentOf(got, "sessions"); s == nil || s.Treatment != layer.TreatmentLinked || s.Status != layer.StatusLinkReplacedByFile {
		t.Errorf("sessions (real entry) = %+v", s)
	}
	if td := treatmentOf(got, "todos"); td == nil || td.Status != layer.StatusWrongHistoryLink {
		t.Errorf("todos (wrong target) = %+v", td)
	}
	if l := treatmentOf(got, "logs"); l == nil || l.Treatment != layer.TreatmentPrivate {
		t.Errorf("logs = %+v", l)
	}

	// A missing history entry is listed as not synced.
	if err := os.Remove(filepath.Join(dir, "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	got, _ = layer.TreatmentsWith(def, p, h.Root, dir, work)
	if hist := treatmentOf(got, "history.jsonl"); hist == nil || hist.Treatment != layer.TreatmentLinked || hist.Status != layer.StatusNotSynced {
		t.Errorf("missing history.jsonl = %+v", hist)
	}
}

// Isolated profiles are unchanged: a history link is a leak, history entries are
// private state.
func TestTreatmentsHistoryIsolatedUnchanged(t *testing.T) {
	h := testutil.HermeticHome(t)
	def := loadToy(t, toyHistDef)
	dir := filepath.Join(h.DataHome, "p")
	testutil.WriteFile(t, filepath.Join(dir, "sessions", "s.jsonl"), "{}")
	if err := os.Symlink(filepath.Join(h.Root, "store", "history.jsonl"), filepath.Join(dir, "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	p := &profile.Profile{Harness: "toy", Name: "p", Share: profile.ShareAll}
	a, _ := layer.Treatments(def, p, h.Root, dir)
	b, _ := layer.TreatmentsWith(def, p, h.Root, dir, nil)
	if len(a) != len(b) {
		t.Fatalf("Treatments and TreatmentsWith(nil) differ: %v vs %v", a, b)
	}
	if hist := treatmentOf(a, "history.jsonl"); hist == nil || hist.Status != layer.StatusLeakingLink {
		t.Errorf("isolated history link = %+v, want leaking-link", hist)
	}
	if s := treatmentOf(a, "sessions"); s == nil || s.Treatment != layer.TreatmentPrivate || s.Status != layer.StatusOK {
		t.Errorf("isolated sessions = %+v", s)
	}
}

// Feature 004 T024: a content profile whose own settings.json differs from the
// default setup's only by the launch entries aprofile placed does not read
// "differs from the default setup".
func TestTreatmentsIgnorePlacedLaunchEntries(t *testing.T) {
	h := testutil.HermeticHome(t)
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	def, _ := set.Get("claude")
	testutil.WriteFile(t, filepath.Join(h.Root, ".claude", "settings.json"), `{"theme":"dark","env":{"MLFLOW":"x"}}`)
	dir := filepath.Join(h.DataHome, "p")
	testutil.WriteFile(t, filepath.Join(dir, "settings.json"),
		`{"theme":"dark","env":{"MLFLOW":"x","ANTHROPIC_MODEL":"m","ANTHROPIC_API_KEY":""},"apiKeyHelper":"cat k"}`)
	p := &profile.Profile{Harness: "claude", Name: "p", Share: profile.ShareContent}

	plain, _ := layer.TreatmentsWith(def, p, h.Root, dir, nil)
	if s := treatmentOf(plain, "settings.json"); s == nil || s.Status != layer.StatusDiffersFromDefault {
		t.Fatalf("without placed keys: %+v", s)
	}
	placed := layer.PlacedKeys{"settings.json": {"env.ANTHROPIC_MODEL", "env.ANTHROPIC_API_KEY", "apiKeyHelper"}}
	got, _ := layer.TreatmentsWithPlaced(def, p, h.Root, dir, nil, placed)
	if s := treatmentOf(got, "settings.json"); s == nil || s.Status != layer.StatusOK {
		t.Errorf("with placed keys: %+v", s)
	}
	// A user change besides the placed entries still differs.
	testutil.WriteFile(t, filepath.Join(dir, "settings.json"),
		`{"theme":"light","env":{"MLFLOW":"x","ANTHROPIC_MODEL":"m"},"apiKeyHelper":"cat k"}`)
	got, _ = layer.TreatmentsWithPlaced(def, p, h.Root, dir, nil, placed)
	if s := treatmentOf(got, "settings.json"); s == nil || s.Status != layer.StatusDiffersFromDefault {
		t.Errorf("a user change must still differ: %+v", s)
	}
}
