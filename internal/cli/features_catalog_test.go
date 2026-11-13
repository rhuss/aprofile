package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/feature"
	"github.com/spf13/cobra"
)

// A new catalog parameter or parameterized feature must be accepted and routed
// to its owner, including explicit empty string values, without command edits.
func TestCatalogParametersRegisterAndCollectMutation(t *testing.T) {
	catalog := []feature.Feature{
		{ID: "telemetry", Params: []feature.Param{{Name: "traces", Type: feature.TypeBool}, {Name: "sample", Type: feature.TypeString}}},
		{ID: "audit", Params: []feature.Param{{Name: "destination", Type: feature.TypeString}}},
	}
	for _, tc := range []struct {
		args  []string
		owner string
		want  map[string]string
	}{
		{[]string{"--sample", ""}, "telemetry", map[string]string{"sample": ""}},
		{[]string{"--destination", "/tmp/audit"}, "audit", map[string]string{"destination": "/tmp/audit"}},
	} {
		cmd := &cobra.Command{}
		f := &featuresFlags{}
		if err := registerFeatureParams(cmd, f, catalog); err != nil {
			t.Fatal(err)
		}
		if err := cmd.ParseFlags(tc.args); err != nil {
			t.Fatal(err)
		}
		owner, params, err := featureParamMutation(cmd, f)
		if err != nil {
			t.Fatal(err)
		}
		if owner != tc.owner || !reflect.DeepEqual(params, tc.want) {
			t.Errorf("%v: owner %s params %v; want %s %v", tc.args, owner, params, tc.owner, tc.want)
		}
	}
}

func TestCatalogParameterOwnershipConflicts(t *testing.T) {
	catalog := []feature.Feature{
		{ID: "first", Params: []feature.Param{{Name: "destination", Type: feature.TypeString}}},
		{ID: "second", Params: []feature.Param{{Name: "destination", Type: feature.TypeString}}},
	}
	err := registerFeatureParams(&cobra.Command{}, &featuresFlags{}, catalog)
	if err == nil {
		t.Fatal("ambiguous catalog parameter ownership accepted")
	}
	for _, want := range []string{"destination", "first", "second"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%v lacks %s", err, want)
		}
	}
}

func TestCatalogParameterMutationRejectsMixedOwners(t *testing.T) {
	cmd := &cobra.Command{}
	f := &featuresFlags{}
	catalog := []feature.Feature{
		{ID: "first", Params: []feature.Param{{Name: "source", Type: feature.TypeString}}},
		{ID: "second", Params: []feature.Param{{Name: "destination", Type: feature.TypeString}}},
	}
	if err := registerFeatureParams(cmd, f, catalog); err != nil {
		t.Fatal(err)
	}
	if err := cmd.ParseFlags([]string{"--source", "x", "--destination", "y"}); err != nil {
		t.Fatal(err)
	}
	_, _, err := featureParamMutation(cmd, f)
	if err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Errorf("mixed-owner flags should fail naming features: %v", err)
	}
}
