package app

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

var nftIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type rawConfig struct {
	Network         rawNetworkConfig         `toml:"network"`
	DelegatedPrefix rawDelegatedPrefixConfig `toml:"delegated_prefix"`
	NFTables        rawNFTablesConfig        `toml:"nftables"`
	Mappings        []rawMappingConfig       `toml:"mappings"`
	Logging         rawLoggingConfig         `toml:"logging"`
}

type rawNetworkConfig struct {
	WANInterface     string `toml:"wan_interface"`
	PolicyRouteTable int64  `toml:"policy_route_table"`
	PDRouteTable     int64  `toml:"pd_route_table"`
}

type rawDelegatedPrefixConfig struct {
	PrefixLength  int64 `toml:"prefix_length"`
	RouteProtocol int64 `toml:"route_protocol"`
	RouteType     int64 `toml:"route_type"`
}

type rawNFTablesConfig struct {
	TableName string `toml:"table_name"`
}

type rawMappingConfig struct {
	Name         string `toml:"name"`
	InsidePrefix string `toml:"inside_prefix"`
	SubnetID     uint64 `toml:"subnet_id"`
}

type rawLoggingConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type Config struct {
	WANInterface string

	PolicyRouteTable int
	PDRouteTable     int

	PDPrefixLength  int
	PDRouteProtocol int
	PDRouteType     int

	NFTTableName string
	Mappings     []MappingConfig
	Logging      LoggingConfig
}

type MappingConfig struct {
	Name         string
	InsidePrefix netip.Prefix
	SubnetID     uint64
}

type LoggingConfig struct {
	Level  string
	Format string
}

type NormalizedConfig struct {
	WANInterface string

	PolicyRouteTable int
	PDRouteTable     int

	PDPrefixLength  int
	PDRouteProtocol int
	PDRouteType     int

	NFTTableName string
	Mappings     []NormalizedMappingConfig
	Logging      LoggingConfig
}

type NormalizedMappingConfig struct {
	Name         string
	InsidePrefix netip.Prefix
	SubnetID     uint64
}

func LoadConfig(path string) (NormalizedConfig, error) {
	var raw rawConfig
	metadata, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return NormalizedConfig{}, fmt.Errorf("decode config: %w", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) != 0 {
		return NormalizedConfig{}, fmt.Errorf("decode config: unknown field %s", undecoded[0])
	}

	cfg, err := parseRawConfig(raw)
	if err != nil {
		return NormalizedConfig{}, err
	}
	if err := ValidateConfig(cfg); err != nil {
		return NormalizedConfig{}, err
	}
	return NormalizeConfig(cfg), nil
}

func parseRawConfig(raw rawConfig) (Config, error) {
	mappings := make([]MappingConfig, 0, len(raw.Mappings))
	for i, item := range raw.Mappings {
		prefix, err := netip.ParsePrefix(item.InsidePrefix)
		if err != nil {
			return Config{}, fmt.Errorf("mappings[%d].inside_prefix: %w", i, err)
		}
		mappings = append(mappings, MappingConfig{
			Name:         item.Name,
			InsidePrefix: prefix,
			SubnetID:     item.SubnetID,
		})
	}

	return Config{
		WANInterface:     raw.Network.WANInterface,
		PolicyRouteTable: int(raw.Network.PolicyRouteTable),
		PDRouteTable:     int(raw.Network.PDRouteTable),
		PDPrefixLength:   int(raw.DelegatedPrefix.PrefixLength),
		PDRouteProtocol:  int(raw.DelegatedPrefix.RouteProtocol),
		PDRouteType:      int(raw.DelegatedPrefix.RouteType),
		NFTTableName:     raw.NFTables.TableName,
		Mappings:         mappings,
		Logging: LoggingConfig{
			Level:  raw.Logging.Level,
			Format: raw.Logging.Format,
		},
	}, nil
}

func ValidateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.WANInterface) == "" {
		return errors.New("network.wan_interface must not be empty")
	}
	if cfg.PolicyRouteTable <= 0 || uint64(cfg.PolicyRouteTable) > math.MaxUint32 {
		return errors.New("network.policy_route_table must be in 1..4294967295")
	}
	if cfg.PDRouteTable <= 0 || uint64(cfg.PDRouteTable) > math.MaxUint32 {
		return errors.New("network.pd_route_table must be in 1..4294967295")
	}
	if cfg.PDPrefixLength < 0 || cfg.PDPrefixLength > 64 {
		return errors.New("delegated_prefix.prefix_length must be in 0..64")
	}
	if cfg.PDRouteProtocol < 0 || cfg.PDRouteProtocol > 255 {
		return errors.New("delegated_prefix.route_protocol must be in 0..255")
	}
	if cfg.PDRouteType < 0 || cfg.PDRouteType > 255 {
		return errors.New("delegated_prefix.route_type must be in 0..255")
	}
	if !nftIdentifierPattern.MatchString(cfg.NFTTableName) {
		return fmt.Errorf("nftables.table_name %q is not a single nftables identifier", cfg.NFTTableName)
	}
	if len(cfg.Mappings) == 0 {
		return errors.New("at least one mappings entry is required")
	}

	names := make(map[string]struct{}, len(cfg.Mappings))
	inside := make(map[netip.Prefix]struct{}, len(cfg.Mappings))
	subnets := make(map[uint64]struct{}, len(cfg.Mappings))
	for i, item := range cfg.Mappings {
		if strings.TrimSpace(item.Name) == "" {
			return fmt.Errorf("mappings[%d].name must not be empty", i)
		}
		if _, exists := names[item.Name]; exists {
			return fmt.Errorf("duplicate mapping name %q", item.Name)
		}
		names[item.Name] = struct{}{}

		if !item.InsidePrefix.IsValid() || !item.InsidePrefix.Addr().Is6() || item.InsidePrefix.Bits() != 64 || item.InsidePrefix != item.InsidePrefix.Masked() {
			return fmt.Errorf("mappings[%d].inside_prefix must be a masked IPv6 /64", i)
		}
		if _, exists := inside[item.InsidePrefix]; exists {
			return fmt.Errorf("duplicate inside prefix %s", item.InsidePrefix)
		}
		inside[item.InsidePrefix] = struct{}{}

		if _, exists := subnets[item.SubnetID]; exists {
			return fmt.Errorf("duplicate subnet ID %#x", item.SubnetID)
		}
		subnets[item.SubnetID] = struct{}{}
		if !subnetIDFits(item.SubnetID, cfg.PDPrefixLength) {
			return fmt.Errorf("mappings[%d].subnet_id %#x does not fit in %d bits", i, item.SubnetID, 64-cfg.PDPrefixLength)
		}
	}

	if cfg.Logging.Level != "debug" && cfg.Logging.Level != "info" && cfg.Logging.Level != "warn" && cfg.Logging.Level != "error" {
		return fmt.Errorf("logging.level %q is invalid", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" && cfg.Logging.Format != "json" {
		return fmt.Errorf("logging.format %q is invalid", cfg.Logging.Format)
	}
	return nil
}

func NormalizeConfig(cfg Config) NormalizedConfig {
	mappings := make([]NormalizedMappingConfig, len(cfg.Mappings))
	for i, item := range cfg.Mappings {
		mappings[i] = NormalizedMappingConfig(item)
	}
	sort.Slice(mappings, func(i, j int) bool {
		return mappings[i].InsidePrefix.Addr().Compare(mappings[j].InsidePrefix.Addr()) < 0
	})
	return NormalizedConfig{
		WANInterface:     cfg.WANInterface,
		PolicyRouteTable: cfg.PolicyRouteTable,
		PDRouteTable:     cfg.PDRouteTable,
		PDPrefixLength:   cfg.PDPrefixLength,
		PDRouteProtocol:  cfg.PDRouteProtocol,
		PDRouteType:      cfg.PDRouteType,
		NFTTableName:     cfg.NFTTableName,
		Mappings:         mappings,
		Logging:          cfg.Logging,
	}
}

func subnetIDFits(id uint64, prefixLength int) bool {
	bits := 64 - prefixLength
	if bits >= 64 {
		return true
	}
	return id < uint64(1)<<bits
}
