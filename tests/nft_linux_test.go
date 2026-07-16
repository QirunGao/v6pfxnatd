//go:build linux

package tests

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
	. "v6pfxnatd/app"
)

func TestReplacementIsBufferedAndSubmittedAsOneBatch(t *testing.T) {
	artifact := testDesiredArtifact(t)
	batches := 0
	var committed []netlink.Message
	conn, err := nftables.New(nftables.WithTestDial(func(request []netlink.Message) ([]netlink.Message, error) {
		if len(request) == 0 {
			return []netlink.Message{{Header: netlink.Header{Type: netlink.Error}, Data: make([]byte, 4)}}, nil
		}
		batches++
		committed = append([]netlink.Message(nil), request...)
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
	assertPrefixMapDataLengths(t, committed)
}

func TestIntervalMapElementEncoding(t *testing.T) {
	element := MapElementSpec{
		Key:   netip.MustParsePrefix("2001:db8:1234:5610::/64"),
		Value: netip.MustParsePrefix("fdff:a887:86e4:10::/64"),
	}
	encoded := EncodeMapElements([]MapElementSpec{element})
	if len(encoded) != 3 || !encoded[0].IntervalEnd || encoded[1].IntervalEnd || !encoded[2].IntervalEnd || len(encoded[1].Val) != 32 || len(encoded[2].Val) != 0 {
		t.Fatalf("encoded interval = %+v", encoded)
	}
	valueMin := netip.MustParseAddr("fdff:a887:86e4:10::").As16()
	valueMax := netip.MustParseAddr("fdff:a887:86e4:10:ffff:ffff:ffff:ffff").As16()
	if !bytes.Equal(encoded[1].Val[:16], valueMin[:]) || !bytes.Equal(encoded[1].Val[16:], valueMax[:]) {
		t.Fatalf("encoded value range = %x, want %x%x", encoded[1].Val, valueMin, valueMax)
	}
}

func TestPrefixNATUsesIPv6AddressRangeRegisters(t *testing.T) {
	set := &nftables.Set{Name: "prefixes", ID: 7}
	expressions := EncodeRuleExpressions(RuleSpec{
		Chain:         "postrouting",
		InterfaceKey:  "oifname",
		InterfaceName: "ppp-wan1",
		MapName:       "prefixes",
		AddressOffset: 8,
		NATType:       "snat",
	}, set)
	if len(expressions) != 5 {
		t.Fatalf("expression count = %d, want 5", len(expressions))
	}
	lookup, ok := expressions[3].(*expr.Lookup)
	if !ok || lookup.SourceRegister != 1 || lookup.DestRegister != 1 || !lookup.IsDestRegSet {
		t.Fatalf("lookup expression = %#v", expressions[3])
	}
	nat, ok := expressions[4].(*expr.NAT)
	if !ok {
		t.Fatalf("NAT expression type = %T", expressions[4])
	}
	if nat.Type != expr.NATTypeSourceNAT || nat.Family != unix.NFPROTO_IPV6 || nat.RegAddrMin != 1 || nat.RegAddrMax != 2 || nat.RegProtoMin != 0 || !nat.Prefix {
		t.Fatalf("NAT expression = %#v", nat)
	}
}

func TestAdjacentIntervalMapElementEncoding(t *testing.T) {
	elements := []MapElementSpec{
		{Key: netip.MustParsePrefix("2001:db8:1234:5611::/64"), Value: netip.MustParsePrefix("fdff:a887:86e4:11::/64")},
		{Key: netip.MustParsePrefix("2001:db8:1234:5610::/64"), Value: netip.MustParsePrefix("fdff:a887:86e4:10::/64")},
	}
	SortMapElements(elements)
	encoded := EncodeMapElements(elements)
	if len(encoded) != 4 || !encoded[0].IntervalEnd || encoded[1].IntervalEnd || encoded[2].IntervalEnd || !encoded[3].IntervalEnd {
		t.Fatalf("encoded adjacent intervals = %+v", encoded)
	}
}

func TestIntervalMapElementEncodingAcrossAddressSpaceBoundary(t *testing.T) {
	elements := []MapElementSpec{
		{Key: netip.MustParsePrefix("::/64"), Value: netip.MustParsePrefix("fdff:a887:86e4:10::/64")},
		{Key: netip.MustParsePrefix("ffff:ffff:ffff:ffff::/64"), Value: netip.MustParsePrefix("fdff:a887:86e4:20::/64")},
	}
	SortMapElements(elements)
	encoded := EncodeMapElements(elements)
	if len(encoded) != 3 {
		t.Fatalf("encoded boundary intervals = %+v", encoded)
	}
}

func TestIntervalMapElementEncodingAtFinalPrefix(t *testing.T) {
	element := MapElementSpec{
		Key:   netip.MustParsePrefix("ffff:ffff:ffff:ffff::/64"),
		Value: netip.MustParsePrefix("fdff:a887:86e4:20::/64"),
	}
	encoded := EncodeMapElements([]MapElementSpec{element})
	if len(encoded) != 2 || !encoded[0].IntervalEnd || encoded[1].IntervalEnd {
		t.Fatalf("encoded final interval = %+v", encoded)
	}
}

func TestDecodeSetComment(t *testing.T) {
	data := userdata.AppendUint32(nil, userdata.NFTNL_UDATA_SET_KEYBYTEORDER, 2)
	data = userdata.AppendString(data, userdata.NFTNL_UDATA_SET_COMMENT, "v6pfxnatd:test")
	comment := DecodeSetComment(data)
	if comment != "v6pfxnatd:test" {
		t.Fatalf("comment = %q", comment)
	}
	if comment := DecodeSetComment([]byte{byte(userdata.NFTNL_UDATA_SET_COMMENT), 10, 'x'}); comment != "" {
		t.Fatalf("malformed userdata comment = %q", comment)
	}
}

func assertPrefixMapDataLengths(t *testing.T, messages []netlink.Message) {
	t.Helper()
	wantType := netlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_NEWSET)
	sets := 0
	for _, message := range messages {
		if message.Header.Type != wantType || len(message.Data) < 4 {
			continue
		}
		decoder, err := netlink.NewAttributeDecoder(message.Data[4:])
		if err != nil {
			t.Fatal(err)
		}
		decoder.ByteOrder = binary.BigEndian
		dataLength := uint32(0)
		for decoder.Next() {
			if decoder.Type() == unix.NFTA_SET_DATA_LEN {
				dataLength = decoder.Uint32()
			}
		}
		if err := decoder.Err(); err != nil {
			t.Fatal(err)
		}
		sets++
		if dataLength != 32 {
			t.Fatalf("map data length = %d, want 32", dataLength)
		}
	}
	if sets != 2 {
		t.Fatalf("managed map count = %d, want 2", sets)
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
