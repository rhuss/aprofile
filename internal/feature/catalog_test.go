package feature_test

import (
	"testing"

	"github.com/cc-deck/aprofile/internal/feature"
)

// FR-003: the v1 catalog contains exactly telemetry and automode-log with the
// spec's params.
func TestCatalogV1Contents(t *testing.T) {
	all := feature.All()
	if len(all) != 2 {
		t.Fatalf("v1 catalog has %d features, want 2 (telemetry, automode-log): %+v", len(all), all)
	}
	tel, ok := feature.ByID("telemetry")
	if !ok {
		t.Fatal("catalog has no telemetry feature")
	}
	if tel.Group != "telemetry" {
		t.Errorf("telemetry group = %q, want telemetry", tel.Group)
	}
	want := []feature.Param{
		{Name: "traces", Type: feature.TypeBool},
		{Name: "logs", Type: feature.TypeBool},
		{Name: "endpoint", Type: feature.TypeString},
		{Name: "protocol", Type: feature.TypeEnum},
	}
	if len(tel.Params) != len(want) {
		t.Fatalf("telemetry params = %+v, want %+v", tel.Params, want)
	}
	for i, p := range want {
		got := tel.Params[i]
		if got.Name != p.Name || got.Type != p.Type {
			t.Errorf("telemetry param %d = %+v, want %+v", i, got, p)
		}
	}
	// The enum's values are the cross-harness union (data-model.md "Param").
	if len(tel.Params[3].Values) == 0 {
		t.Error("telemetry protocol param declares no enum values")
	}
	for _, v := range []string{"grpc", "http/json", "http/protobuf", "otlp-http json", "otlp-http binary", "otlp-grpc"} {
		if !tel.Params[3].HasValue(v) {
			t.Errorf("telemetry protocol enum lacks %q (cross-harness union)", v)
		}
	}

	am, ok := feature.ByID("automode-log")
	if !ok {
		t.Fatal("catalog has no automode-log feature")
	}
	if am.Group != "logging" {
		t.Errorf("automode-log group = %q, want logging", am.Group)
	}
	if len(am.Params) != 0 {
		t.Errorf("automode-log params = %+v, want none", am.Params)
	}
}

func TestCatalogLookups(t *testing.T) {
	if _, ok := feature.ByID("nope"); ok {
		t.Error("unknown feature id resolved")
	}
	groups := feature.ByGroup("logging")
	if len(groups) != 1 || groups[0].ID != "automode-log" {
		t.Errorf("ByGroup(logging) = %+v, want automode-log", groups)
	}
	if g := feature.ByGroup("nope"); len(g) != 0 {
		t.Errorf("ByGroup(nope) = %+v, want none", g)
	}
	// All is ordered by feature id.
	all := feature.All()
	for i := 1; i < len(all); i++ {
		if all[i-1].ID > all[i].ID {
			t.Errorf("catalog is not sorted by id: %s after %s", all[i].ID, all[i-1].ID)
		}
	}
}

func TestParamLookup(t *testing.T) {
	tel, _ := feature.ByID("telemetry")
	p, ok := tel.Param("protocol")
	if !ok || p.Type != feature.TypeEnum {
		t.Fatalf("telemetry.Param(protocol) = %+v, %v", p, ok)
	}
	if _, ok := tel.Param("nope"); ok {
		t.Error("unknown param resolved")
	}
	am, _ := feature.ByID("automode-log")
	if _, ok := am.Param("traces"); ok {
		t.Error("automode-log resolved a param it does not have")
	}
}
