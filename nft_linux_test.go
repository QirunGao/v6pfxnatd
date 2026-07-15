//go:build linux

package main

import (
	"net/netip"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/userdata"
	"github.com/mdlayher/netlink"
)

func TestReplacementIsBufferedAndSubmittedAsOneBatch(t *testing.T) {
	artifact := testDesiredArtifact(t)
	batches := 0
	conn, err := nftables.New(nftables.WithTestDial(func(request []netlink.Message) ([]netlink.Message, error) {
		if len(request) == 0 {
			return []netlink.Message{{Header: netlink.Header{Type: netlink.Error}, Data: make([]byte, 4)}}, nil
		}
		batches++
		if len(request) < 10 {
			t.Fatalf("batch has %d messages, want complete table replacement", len(request))
		}
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := BuildReplacement(conn, CurrentTable{Exists: true}, artifact); err != nil {
		t.Fatal(err)
	}
	if batches != 0 {
		t.Fatalf("BuildReplacement submitted %d batches", batches)
	}
	if err := CommitReplacement(conn); err != nil {
		t.Fatal(err)
	}
	if batches != 1 {
		t.Fatalf("commit submitted %d batches, want 1", batches)
	}
}

func TestIntervalMapElementEncoding(t *testing.T) {
	element := MapElementSpec{
		Key:   netip.MustParsePrefix("2001:db8:1234:5610::/64"),
		Value: netip.MustParsePrefix("fdff:a887:86e4:10::/64"),
	}
	encoded := encodeMapElements([]MapElementSpec{element})
	if len(encoded) != 3 || !encoded[0].IntervalEnd || encoded[1].IntervalEnd || !encoded[2].IntervalEnd || len(encoded[1].Val) != 16 || len(encoded[2].Val) != 0 {
		t.Fatalf("encoded interval = %+v", encoded)
	}
}

func TestAdjacentIntervalMapElementEncoding(t *testing.T) {
	elements := []MapElementSpec{
		{Key: netip.MustParsePrefix("2001:db8:1234:5611::/64"), Value: netip.MustParsePrefix("fdff:a887:86e4:11::/64")},
		{Key: netip.MustParsePrefix("2001:db8:1234:5610::/64"), Value: netip.MustParsePrefix("fdff:a887:86e4:10::/64")},
	}
	sortMapElements(elements)
	encoded := encodeMapElements(elements)
	if len(encoded) != 4 || !encoded[0].IntervalEnd || encoded[1].IntervalEnd || encoded[2].IntervalEnd || !encoded[3].IntervalEnd {
		t.Fatalf("encoded adjacent intervals = %+v", encoded)
	}
}

func TestIntervalMapElementEncodingAcrossAddressSpaceBoundary(t *testing.T) {
	elements := []MapElementSpec{
		{Key: netip.MustParsePrefix("::/64"), Value: netip.MustParsePrefix("fdff:a887:86e4:10::/64")},
		{Key: netip.MustParsePrefix("ffff:ffff:ffff:ffff::/64"), Value: netip.MustParsePrefix("fdff:a887:86e4:20::/64")},
	}
	sortMapElements(elements)
	encoded := encodeMapElements(elements)
	if len(encoded) != 3 {
		t.Fatalf("encoded boundary intervals = %+v", encoded)
	}
}

func TestIntervalMapElementEncodingAtFinalPrefix(t *testing.T) {
	element := MapElementSpec{
		Key:   netip.MustParsePrefix("ffff:ffff:ffff:ffff::/64"),
		Value: netip.MustParsePrefix("fdff:a887:86e4:20::/64"),
	}
	encoded := encodeMapElements([]MapElementSpec{element})
	if len(encoded) != 2 || !encoded[0].IntervalEnd || encoded[1].IntervalEnd {
		t.Fatalf("encoded final interval = %+v", encoded)
	}
}

func TestDecodeSetComment(t *testing.T) {
	data := userdata.AppendUint32(nil, userdata.NFTNL_UDATA_SET_KEYBYTEORDER, 2)
	data = userdata.AppendString(data, userdata.NFTNL_UDATA_SET_COMMENT, "v6pfxnatd:v2:test")
	comment, err := decodeSetComment(data)
	if err != nil {
		t.Fatal(err)
	}
	if comment != "v6pfxnatd:v2:test" {
		t.Fatalf("comment = %q", comment)
	}
	if _, err := decodeSetComment([]byte{byte(userdata.NFTNL_UDATA_SET_COMMENT), 10, 'x'}); err == nil {
		t.Fatal("malformed userdata accepted")
	}
}

func testDesiredArtifact(t *testing.T) DesiredArtifact {
	t.Helper()
	cfg := testNormalizedConfig(t)
	pd := netip.MustParsePrefix("2001:db8:1234:5600::/56")
	mappings := DeriveMappings(cfg, pd)
	spec := BuildDesiredSpec(cfg, pd, mappings)
	return DesiredArtifact{Spec: spec, Fingerprint: FingerprintDesiredSpec(spec)}
}
