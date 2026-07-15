//go:build linux

package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	mdnetlink "github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func Reconcile(ctx context.Context, cfg NormalizedConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := ReadNetworkSnapshot(cfg)
	if err != nil {
		return fmt.Errorf("read network snapshot: %w", err)
	}
	delegatedPrefix, err := SelectDelegatedPrefix(snapshot)
	if err != nil {
		return fmt.Errorf("select delegated prefix: %w", err)
	}
	mappings := DeriveMappings(cfg, delegatedPrefix)
	desiredSpec := BuildDesiredSpec(cfg, delegatedPrefix, mappings)
	desired := DesiredArtifact{Spec: desiredSpec, Fingerprint: FingerprintDesiredSpec(desiredSpec)}
	current, err := ReadCurrentTable(cfg.NFTTableName)
	if err != nil {
		return fmt.Errorf("read current nftables table: %w", err)
	}
	if IsCurrent(current, desired) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("create nftables connection: %w", err)
	}
	if err := BuildReplacement(conn, current, desired); err != nil {
		return fmt.Errorf("build nftables replacement: %w", err)
	}
	if err := CommitReplacement(conn); err != nil {
		return fmt.Errorf("commit nftables replacement: %w", err)
	}
	slog.Info("nftables table replaced", "table", cfg.NFTTableName, "delegated_prefix", delegatedPrefix)
	return nil
}

func ReadCurrentTable(tableName string) (CurrentTable, error) {
	conn, err := nftables.New()
	if err != nil {
		return CurrentTable{}, fmt.Errorf("create query connection: %w", err)
	}
	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyIPv6)
	if err != nil {
		return CurrentTable{}, fmt.Errorf("list IPv6 nftables tables: %w", err)
	}

	exists := false
	for _, item := range tables {
		if item.Name == tableName {
			exists = true
			break
		}
	}
	if !exists {
		return CurrentTable{Exists: false}, nil
	}

	current := CurrentTable{Exists: true}
	comments, err := readManagedSetComments(tableName)
	if err != nil {
		return CurrentTable{}, fmt.Errorf("read managed set comments: %w", err)
	}
	current.DNATComment = comments[dnatMapName]
	current.SNATComment = comments[snatMapName]
	return current, nil
}

func BuildReplacement(conn *nftables.Conn, current CurrentTable, desired DesiredArtifact) error {
	if current.Exists {
		conn.DelTable(&nftables.Table{Family: nftables.TableFamilyIPv6, Name: desired.Spec.TableName})
	}
	table := conn.AddTable(&nftables.Table{Family: nftables.TableFamilyIPv6, Name: desired.Spec.TableName})
	policy := nftables.ChainPolicyAccept
	chains := map[string]*nftables.Chain{
		preroutingChainName: conn.AddChain(&nftables.Chain{
			Name: preroutingChainName, Table: table, Type: nftables.ChainTypeNAT,
			Hooknum: nftables.ChainHookPrerouting, Priority: nftables.ChainPriorityNATDest, Policy: &policy,
		}),
		postroutingChainName: conn.AddChain(&nftables.Chain{
			Name: postroutingChainName, Table: table, Type: nftables.ChainTypeNAT,
			Hooknum: nftables.ChainHookPostrouting, Priority: nftables.ChainPriorityNATSource, Policy: &policy,
		}),
	}

	metadata := MetadataFor(desired)
	maps := make(map[string]*nftables.Set, len(desired.Spec.Maps))
	dataType := prefixMapDataType()
	for _, item := range desired.Spec.Maps {
		set := &nftables.Set{
			Table: table, Name: item.Name, IsMap: true, Constant: item.Constant, Interval: item.Interval,
			KeyType: nftables.TypeIP6Addr, DataType: dataType, Comment: metadata,
		}
		elements := EncodeMapElements(item.Elements)
		if err := conn.AddSet(set, elements); err != nil {
			return fmt.Errorf("add map %q: %w", item.Name, err)
		}
		maps[item.Name] = set
	}

	for _, rule := range desired.Spec.Rules {
		chain := chains[rule.Chain]
		set := maps[rule.MapName]
		conn.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: EncodeRuleExpressions(rule, set)})
	}
	return nil
}

func CommitReplacement(conn *nftables.Conn) error {
	return conn.Flush()
}

func prefixMapDataType() nftables.SetDatatype {
	dataType := nftables.TypeIP6Addr
	dataType.Bytes *= 2
	return dataType
}

func EncodeMapElements(items []MapElementSpec) []nftables.SetElement {
	if len(items) == 0 {
		return nil
	}
	encoded := make([]nftables.SetElement, 0, len(items)*2)
	var zero [16]byte
	firstKey := items[0].Key.Addr().As16()
	if firstKey != zero {
		encoded = append(encoded, nftables.SetElement{Key: make([]byte, 16), IntervalEnd: true})
	}
	for i, element := range items {
		key := element.Key.Addr().As16()
		value := element.Value.Addr().As16()
		valueEnd := prefixLast64(value)
		encodedValue := make([]byte, 32)
		copy(encodedValue, value[:])
		copy(encodedValue[16:], valueEnd[:])
		end := prefixEnd64(key)
		encoded = append(encoded, nftables.SetElement{
			Key: append([]byte(nil), key[:]...),
			Val: encodedValue,
		})
		if end == zero {
			continue
		}
		if i+1 == len(items) || items[i+1].Key.Addr().As16() != end {
			encoded = append(encoded, nftables.SetElement{
				Key:         append([]byte(nil), end[:]...),
				IntervalEnd: true,
			})
		}
	}
	return encoded
}

func readManagedSetComments(tableName string) (map[string]string, error) {
	attributes, err := mdnetlink.MarshalAttributes([]mdnetlink.Attribute{{Type: unix.NFTA_SET_TABLE, Data: append([]byte(tableName), 0)}})
	if err != nil {
		return nil, fmt.Errorf("marshal set query: %w", err)
	}
	request := mdnetlink.Message{
		Header: mdnetlink.Header{
			Type:  mdnetlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_GETSET),
			Flags: mdnetlink.Request | mdnetlink.Dump,
		},
		Data: append([]byte{byte(nftables.TableFamilyIPv6), unix.NFNETLINK_V0, 0, 0}, attributes...),
	}
	conn, err := mdnetlink.Dial(unix.NETLINK_NETFILTER, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	messages, err := conn.Execute(request)
	if err != nil {
		return nil, err
	}
	comments := make(map[string]string, 2)
	for _, message := range messages {
		decoder, err := mdnetlink.NewAttributeDecoder(message.Data[4:])
		if err != nil {
			return nil, err
		}
		decoder.ByteOrder = binary.BigEndian
		var table, name string
		var rawUserData []byte
		for decoder.Next() {
			switch decoder.Type() {
			case unix.NFTA_SET_TABLE:
				table = decoder.String()
			case unix.NFTA_SET_NAME:
				name = decoder.String()
			case unix.NFTA_SET_USERDATA:
				rawUserData = decoder.Bytes()
			}
		}
		if err := decoder.Err(); err != nil {
			return nil, err
		}
		if table == tableName && (name == dnatMapName || name == snatMapName) {
			comment, _ := DecodeSetComment(rawUserData)
			comments[name] = comment
		}
	}
	return comments, nil
}

func DecodeSetComment(data []byte) (string, error) {
	for len(data) != 0 {
		if len(data) < 2 {
			return "", errors.New("malformed nftables set userdata")
		}
		typeID, length := userdata.Type(data[0]), int(data[1])
		if len(data) < 2+length {
			return "", errors.New("malformed nftables set userdata length")
		}
		value := data[2 : 2+length]
		if typeID == userdata.NFTNL_UDATA_SET_COMMENT {
			value = bytes.TrimSuffix(value, []byte{0})
			return string(value), nil
		}
		data = data[2+length:]
	}
	return "", nil
}

func prefixEnd64(start [16]byte) [16]byte {
	end := start
	high := binary.BigEndian.Uint64(end[:8])
	binary.BigEndian.PutUint64(end[:8], high+1)
	binary.BigEndian.PutUint64(end[8:], 0)
	return end
}

func prefixLast64(start [16]byte) [16]byte {
	last := start
	binary.BigEndian.PutUint64(last[8:], ^uint64(0))
	return last
}

func EncodeRuleExpressions(rule RuleSpec, set *nftables.Set) []expr.Any {
	interfaceData := make([]byte, unix.IFNAMSIZ)
	copy(interfaceData, rule.InterfaceName)
	metaKey := expr.MetaKeyIIFNAME
	if rule.InterfaceKey == "oifname" {
		metaKey = expr.MetaKeyOIFNAME
	}
	natType := expr.NATTypeDestNAT
	if rule.NATType == "snat" {
		natType = expr.NATTypeSourceNAT
	}
	return []expr.Any{
		&expr.Meta{Key: metaKey, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: interfaceData},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: rule.AddressOffset, Len: 16},
		&expr.Lookup{SourceRegister: 1, DestRegister: 1, IsDestRegSet: true, SetName: set.Name, SetID: set.ID},
		&expr.NAT{Type: natType, Family: unix.NFPROTO_IPV6, RegAddrMin: 1, RegAddrMax: 2, Prefix: true},
	}
}
