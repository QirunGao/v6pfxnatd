package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
)

const SchemaVersion uint32 = 2

const (
	managedFamily        = "ip6"
	preroutingChainName  = "prerouting"
	postroutingChainName = "postrouting"
	dnatMapName          = "dnat_prefixes"
	snatMapName          = "snat_prefixes"
)

type NetworkSnapshot struct {
	PDCandidates []netip.Prefix
}

type PrefixMapping struct {
	Name     string
	Inside   netip.Prefix
	Outside  netip.Prefix
	SubnetID uint64
}

type DesiredSpec struct {
	SchemaVersion uint32

	Family    string
	TableName string

	WANInterface    string
	DelegatedPrefix netip.Prefix

	Chains   []ChainSpec
	Maps     []MapSpec
	Rules    []RuleSpec
	Mappings []PrefixMapping
}

type ChainSpec struct {
	Name     string
	Type     string
	Hook     string
	Priority int32
	Policy   string
}

type MapSpec struct {
	Name     string
	KeyType  string
	DataType string
	Constant bool
	Interval bool
	Elements []MapElementSpec
}

type MapElementSpec struct {
	Key   netip.Prefix
	Value netip.Prefix
}

type RuleSpec struct {
	Chain         string
	InterfaceKey  string
	InterfaceName string
	MapName       string
	AddressOffset uint32
	NATType       string
}

type DesiredArtifact struct {
	Spec        DesiredSpec
	Fingerprint [32]byte
}

type CurrentTable struct {
	Exists bool

	DNATComment string
	SNATComment string
}

func SelectDelegatedPrefix(snapshot NetworkSnapshot) (netip.Prefix, error) {
	if len(snapshot.PDCandidates) != 1 {
		return netip.Prefix{}, fmt.Errorf("expected exactly one delegated prefix candidate, got %d", len(snapshot.PDCandidates))
	}
	return snapshot.PDCandidates[0], nil
}

func DeriveMappings(cfg NormalizedConfig, delegatedPrefix netip.Prefix) []PrefixMapping {
	base := delegatedPrefix.Addr().As16()
	baseHigh := binary.BigEndian.Uint64(base[:8])
	mappings := make([]PrefixMapping, 0, len(cfg.Mappings))
	for _, item := range cfg.Mappings {
		outsideBytes := base
		binary.BigEndian.PutUint64(outsideBytes[:8], baseHigh|item.SubnetID)
		outside := netip.PrefixFrom(netip.AddrFrom16(outsideBytes), 64).Masked()
		mappings = append(mappings, PrefixMapping{Name: item.Name, Inside: item.InsidePrefix, Outside: outside, SubnetID: item.SubnetID})
	}
	return mappings
}

func BuildDesiredSpec(cfg NormalizedConfig, delegatedPrefix netip.Prefix, mappings []PrefixMapping) DesiredSpec {
	dnatElements := make([]MapElementSpec, 0, len(mappings))
	snatElements := make([]MapElementSpec, 0, len(mappings))
	for _, mapping := range mappings {
		dnatElements = append(dnatElements, MapElementSpec{Key: mapping.Outside, Value: mapping.Inside})
		snatElements = append(snatElements, MapElementSpec{Key: mapping.Inside, Value: mapping.Outside})
	}
	SortMapElements(dnatElements)
	SortMapElements(snatElements)

	return DesiredSpec{
		SchemaVersion:   SchemaVersion,
		Family:          managedFamily,
		TableName:       cfg.NFTTableName,
		WANInterface:    cfg.WANInterface,
		DelegatedPrefix: delegatedPrefix,
		Chains: []ChainSpec{
			{Name: preroutingChainName, Type: "nat", Hook: "prerouting", Priority: -100, Policy: "accept"},
			{Name: postroutingChainName, Type: "nat", Hook: "postrouting", Priority: 100, Policy: "accept"},
		},
		Maps: []MapSpec{
			{Name: dnatMapName, KeyType: "ipv6_addr", DataType: "ipv6_addr", Constant: true, Interval: true, Elements: dnatElements},
			{Name: snatMapName, KeyType: "ipv6_addr", DataType: "ipv6_addr", Constant: true, Interval: true, Elements: snatElements},
		},
		Rules: []RuleSpec{
			{Chain: preroutingChainName, InterfaceKey: "iifname", InterfaceName: cfg.WANInterface, MapName: dnatMapName, AddressOffset: 24, NATType: "dnat"},
			{Chain: postroutingChainName, InterfaceKey: "oifname", InterfaceName: cfg.WANInterface, MapName: snatMapName, AddressOffset: 8, NATType: "snat"},
		},
		Mappings: mappings,
	}
}

func FingerprintDesiredSpec(desired DesiredSpec) [32]byte {
	var buffer bytes.Buffer
	writeUint32(&buffer, desired.SchemaVersion)
	writeString(&buffer, desired.Family)
	writeString(&buffer, desired.TableName)
	writeString(&buffer, desired.WANInterface)
	writeString(&buffer, desired.DelegatedPrefix.String())
	writeUint32(&buffer, uint32(len(desired.Chains)))
	for _, chain := range desired.Chains {
		writeString(&buffer, chain.Name)
		writeString(&buffer, chain.Type)
		writeString(&buffer, chain.Hook)
		writeUint32(&buffer, uint32(chain.Priority))
		writeString(&buffer, chain.Policy)
	}
	writeUint32(&buffer, uint32(len(desired.Maps)))
	for _, item := range desired.Maps {
		writeString(&buffer, item.Name)
		writeString(&buffer, item.KeyType)
		writeString(&buffer, item.DataType)
		writeBool(&buffer, item.Constant)
		writeBool(&buffer, item.Interval)
		writeUint32(&buffer, uint32(len(item.Elements)))
		for _, element := range item.Elements {
			writeString(&buffer, element.Key.String())
			writeString(&buffer, element.Value.String())
		}
	}
	writeUint32(&buffer, uint32(len(desired.Rules)))
	for _, rule := range desired.Rules {
		writeString(&buffer, rule.Chain)
		writeString(&buffer, rule.InterfaceKey)
		writeString(&buffer, rule.InterfaceName)
		writeString(&buffer, rule.MapName)
		writeUint32(&buffer, rule.AddressOffset)
		writeString(&buffer, rule.NATType)
	}
	writeUint32(&buffer, uint32(len(desired.Mappings)))
	for _, mapping := range desired.Mappings {
		writeString(&buffer, mapping.Name)
		writeString(&buffer, mapping.Inside.String())
		writeString(&buffer, mapping.Outside.String())
		writeUint64(&buffer, mapping.SubnetID)
	}
	return sha256.Sum256(buffer.Bytes())
}

func MetadataFor(desired DesiredArtifact) string {
	return fmt.Sprintf("v6pfxnatd:v%d:%s", desired.Spec.SchemaVersion, hex.EncodeToString(desired.Fingerprint[:]))
}

func IsCurrent(current CurrentTable, desired DesiredArtifact) bool {
	if !current.Exists {
		return false
	}
	metadata := MetadataFor(desired)
	return current.DNATComment == metadata && current.SNATComment == metadata
}

func SortMapElements(elements []MapElementSpec) {
	sort.Slice(elements, func(i, j int) bool {
		return elements[i].Key.Addr().Compare(elements[j].Key.Addr()) < 0
	})
}

func writeString(buffer *bytes.Buffer, value string) {
	writeUint32(buffer, uint32(len(value)))
	buffer.WriteString(value)
}

func writeBool(buffer *bytes.Buffer, value bool) {
	if value {
		buffer.WriteByte(1)
		return
	}
	buffer.WriteByte(0)
}

func writeUint32(buffer *bytes.Buffer, value uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	buffer.Write(data[:])
}

func writeUint64(buffer *bytes.Buffer, value uint64) {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	buffer.Write(data[:])
}
