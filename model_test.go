package main

import (
	"net/netip"
	"testing"
)

func TestSelectDelegatedPrefixRequiresExactlyOne(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8:1234:5600::/56")
	got, err := SelectDelegatedPrefix(NetworkSnapshot{PDCandidates: []netip.Prefix{prefix}})
	if err != nil || got != prefix {
		t.Fatalf("got %s, err=%v", got, err)
	}
	if _, err := SelectDelegatedPrefix(NetworkSnapshot{}); err == nil {
		t.Fatal("zero candidates accepted")
	}
	if _, err := SelectDelegatedPrefix(NetworkSnapshot{PDCandidates: []netip.Prefix{prefix, prefix}}); err == nil {
		t.Fatal("multiple candidates accepted")
	}
}

func TestDeriveMappings(t *testing.T) {
	cfg := testNormalizedConfig(t)
	pd := netip.MustParsePrefix("2001:db8:1234:5600::/56")
	mappings := DeriveMappings(cfg, pd)
	want := map[string]netip.Prefix{
		"vlan10": netip.MustParsePrefix("2001:db8:1234:5610::/64"),
		"vlan20": netip.MustParsePrefix("2001:db8:1234:5620::/64"),
	}
	for _, mapping := range mappings {
		if mapping.Outside != want[mapping.Name] {
			t.Fatalf("%s outside = %s, want %s", mapping.Name, mapping.Outside, want[mapping.Name])
		}
	}
}

func TestDeriveMappingsSupportsZeroLengthPD(t *testing.T) {
	cfg := testNormalizedConfig(t)
	cfg.PDPrefixLength = 0
	cfg.Mappings = []NormalizedMappingConfig{{Name: "max", InsidePrefix: netip.MustParsePrefix("fd00::/64"), SubnetID: ^uint64(0)}}
	mappings := DeriveMappings(cfg, netip.MustParsePrefix("::/0"))
	if got, want := mappings[0].Outside, netip.MustParsePrefix("ffff:ffff:ffff:ffff::/64"); got != want {
		t.Fatalf("outside = %s, want %s", got, want)
	}
}

func TestSortMapElementsUsesNumericAddressOrder(t *testing.T) {
	elements := []MapElementSpec{
		{Key: netip.MustParsePrefix("2001:db8:0:10::/64"), Value: netip.MustParsePrefix("fd00:0:0:10::/64")},
		{Key: netip.MustParsePrefix("2001:db8:0:2::/64"), Value: netip.MustParsePrefix("fd00:0:0:2::/64")},
	}
	sortMapElements(elements)
	if got, want := elements[0].Key, netip.MustParsePrefix("2001:db8:0:2::/64"); got != want {
		t.Fatalf("first key = %s, want %s", got, want)
	}
}

func TestDesiredSpecFingerprintAndCurrentComparison(t *testing.T) {
	cfg := testNormalizedConfig(t)
	snapshot := NetworkSnapshot{PDCandidates: []netip.Prefix{netip.MustParsePrefix("2001:db8:1234:5600::/56")}}
	pd, _ := SelectDelegatedPrefix(snapshot)
	mappings := DeriveMappings(cfg, pd)
	spec := BuildDesiredSpec(cfg, pd, mappings)
	artifact := DesiredArtifact{Spec: spec, Fingerprint: FingerprintDesiredSpec(spec)}
	if artifact.Fingerprint != FingerprintDesiredSpec(spec) {
		t.Fatal("fingerprint is not deterministic")
	}
	changed := spec
	changed.WANInterface = "other"
	if artifact.Fingerprint == FingerprintDesiredSpec(changed) {
		t.Fatal("fingerprint ignored WAN interface")
	}

	current := currentFromDesired(artifact)
	if !IsCurrent(current, artifact) {
		t.Fatal("equivalent current table was not recognized")
	}
	current.SNATComment = "v6pfxnatd:v2:other"
	if IsCurrent(current, artifact) {
		t.Fatal("table with different fingerprint was recognized as current")
	}
}

func currentFromDesired(desired DesiredArtifact) CurrentTable {
	metadata := metadataFor(desired)
	return CurrentTable{
		Exists:      true,
		DNATComment: metadata,
		SNATComment: metadata,
	}
}

func testNormalizedConfig(t *testing.T) NormalizedConfig {
	t.Helper()
	cfg, err := LoadConfig(writeConfig(t, validConfigText))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
