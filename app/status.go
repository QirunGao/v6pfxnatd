package app

import (
	"fmt"
	"net/netip"
	"strings"
)

const operationalStatusPath = "/run/v6pfxnatd/status"

func BuildOperationalStatus(cfg NormalizedConfig, mappings []PrefixMapping) []byte {
	mappingsByName := make(map[string]PrefixMapping, len(mappings))
	for _, mapping := range mappings {
		mappingsByName[mapping.Name] = mapping
	}

	var status strings.Builder
	for _, mapping := range mappings {
		fmt.Fprintf(&status, "mapping %s %s\n", mapping.Inside, mapping.Outside)
	}
	for _, item := range cfg.Addresses {
		outside := mappingsByName[item.Mapping].Outside.Addr().As16()
		suffix := item.Suffix.As16()
		copy(outside[8:], suffix[8:])
		fmt.Fprintf(&status, "address %s %s\n", item.Name, netip.AddrFrom16(outside))
	}
	return []byte(status.String())
}
