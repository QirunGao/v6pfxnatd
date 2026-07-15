# v6pfxnatd

`v6pfxnatd` is an event-driven nftables IPv6 prefix NAT reconciler for networks
whose public prefix changes with DHCPv6-PD. It watches IPv6 route changes,
derives the configured public `/64` mappings, and atomically replaces only its
own nftables NAT table when the desired state changes.

The daemon does not manage firewall filter policy, persist runtime state, call
the `ip` or `nft` commands, or modify conntrack state. Existing `input` and
`forward` chains remain the administrator's responsibility.

## Install

Download a package from GitHub Releases and install it:

```sh
sudo apt install ./v6pfxnatd_0.2.0_amd64.deb
sudoedit /etc/v6pfxnatd/config.toml
sudo systemctl enable --now v6pfxnatd.service
```

RPM packages are also published for supported architectures.

Packages install:

```text
/usr/sbin/v6pfxnatd
/etc/v6pfxnatd/config.toml
/lib/systemd/system/v6pfxnatd.service        # Debian
/usr/lib/systemd/system/v6pfxnatd.service    # RPM
/etc/modules-load.d/v6pfxnatd.conf
/usr/share/doc/v6pfxnatd/README.md
```

## Configuration

```toml
[network]
wan_interface = "ppp-wan1"
policy_route_table = 1001
pd_route_table = 254

[delegated_prefix]
prefix_length = 56
route_protocol = 16
route_type = 7

[nftables]
table_name = "v6pfxnat_wan1"

[[mappings]]
name = "vlan10"
inside_prefix = "fdff:a887:86e4:10::/64"
subnet_id = 0x10

[[mappings]]
name = "vlan20"
inside_prefix = "fdff:a887:86e4:20::/64"
subnet_id = 0x20

[logging]
level = "info"
format = "text"
```

The configuration is decoded strictly. Interfaces, route selectors, nftables
identifiers, prefix lengths, subnet IDs, mappings, and logging settings are
validated before the daemon changes nftables.

## Operation

```sh
v6pfxnatd -c /etc/v6pfxnatd/config.toml
v6pfxnatd --version
systemctl status v6pfxnatd.service
journalctl -u v6pfxnatd.service
```

The service runs as root with its capability set restricted to
`CAP_NET_ADMIN`. It requires no writable runtime directory and creates no PID,
lock, state, or temporary rules file.

The daemon owns one dedicated nftables NAT table. Avoid a global
`flush ruleset` unless `v6pfxnatd` is restarted afterward, because such a flush
also removes the managed table.

## Build From Source

```sh
go test ./...
go build ./cmd/v6pfxnatd
```

Linux is the supported runtime platform. The source tree can still be compiled
on other platforms so configuration and command behavior can be tested.

## Releases

Pushing a `v*` tag publishes static Linux `amd64` and `arm64` binaries plus
`.deb` and `.rpm` packages through GoReleaser. An existing tag can be rerun from
the manual GitHub Actions workflow.

## License

Licensed under the Apache License 2.0. See `LICENSE`.
