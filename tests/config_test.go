package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "v6pfxnatd/app"
)

func TestLoadConfigValidatesAndNormalizes(t *testing.T) {
	path := writeConfig(t, strings.ReplaceAll(validConfigText, firstMapping+secondMapping, secondMapping+firstMapping))
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WANInterface != "ppp-wan1" || cfg.PDPrefixLength != 56 || cfg.NFTTableName != "v6pfxnat_wan1" {
		t.Fatalf("config = %+v", cfg)
	}
	if len(cfg.Mappings) != 2 || cfg.Mappings[0].Name != "vlan10" || cfg.Mappings[1].Name != "vlan20" {
		t.Fatalf("mappings were not normalized: %+v", cfg.Mappings)
	}
}

func TestExampleConfigLoads(t *testing.T) {
	path, err := filepath.Abs("../release/config.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigRejectsUnknownField(t *testing.T) {
	path := writeConfig(t, validConfigText+"\nunknown = true\n")
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateConfigRejectsImportantBoundaries(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, validConfigText))
	if err != nil {
		t.Fatal(err)
	}
	base := Config{
		WANInterface: cfg.WANInterface, PolicyRouteTable: cfg.PolicyRouteTable, PDRouteTable: cfg.PDRouteTable,
		PDPrefixLength: cfg.PDPrefixLength, PDRouteProtocol: cfg.PDRouteProtocol, PDRouteType: cfg.PDRouteType,
		NFTTableName: cfg.NFTTableName, Logging: cfg.Logging,
	}
	for _, item := range cfg.Mappings {
		base.Mappings = append(base.Mappings, MappingConfig(item))
	}

	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"empty interface", func(c *Config) { c.WANInterface = "" }, "wan_interface"},
		{"invalid table", func(c *Config) { c.NFTTableName = "bad table" }, "identifier"},
		{"duplicate name", func(c *Config) { c.Mappings[1].Name = c.Mappings[0].Name }, "duplicate mapping name"},
		{"subnet overflow", func(c *Config) { c.Mappings[0].SubnetID = 0x100 }, "does not fit"},
		{"unknown log format", func(c *Config) { c.Logging.Format = "yaml" }, "logging.format"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			candidate.Mappings = append([]MappingConfig(nil), base.Mappings...)
			test.mutate(&candidate)
			if err := ValidateConfig(candidate); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

const firstMapping = `
[[mappings]]
name = "vlan10"
inside_prefix = "fdff:a887:86e4:10::/64"
subnet_id = 0x10
`

const secondMapping = `
[[mappings]]
name = "vlan20"
inside_prefix = "fdff:a887:86e4:20::/64"
subnet_id = 0x20
`

const validConfigText = `[network]
wan_interface = "ppp-wan1"
policy_route_table = 1001
pd_route_table = 254

[delegated_prefix]
prefix_length = 56
route_protocol = 16
route_type = 7

[nftables]
table_name = "v6pfxnat_wan1"
` + firstMapping + secondMapping + `
[logging]
level = "info"
format = "text"
`
