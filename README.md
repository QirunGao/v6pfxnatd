# v6pfxnatd

`v6pfxnatd` is an event-driven nftables IPv6 prefix NAT reconciler for networks
whose public prefix changes with DHCPv6-PD. It watches IPv6 route changes,
derives the configured public `/64` mappings, and atomically replaces only its
own nftables NAT table when the desired state changes.

Each mapping replaces only the high 64-bit network prefix. The low 64-bit
interface identifier is preserved in both directions, for example
`fdff:a887:86e4:10::1234` maps to `2001:db8:1234:5610::1234`.

The daemon does not manage firewall filter policy, persist applied state, call
the `ip` or `nft` commands, or modify conntrack state. Existing `input` and
`forward` chains remain the administrator's responsibility. After a successful
nftables replacement it publishes a read-only operational snapshot under
`/run`; that file is output only and is never read by the daemon.

## Install

Download a package from GitHub Releases and install it:

```sh
sudo apt install ./v6pfxnatd_0.2.2_amd64.deb
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

[[addresses]]
name = "gateway-c"
mapping = "vlan20"
suffix = "::c"

[logging]
level = "info"
format = "text"
```

The configuration is decoded strictly. Interfaces, route selectors, nftables
identifiers, prefix lengths, subnet IDs, mappings, address names, mapping
references, suffixes, and logging settings are validated before the daemon
changes nftables. Address suffixes contain only the low 64 bits; the selected
mapping supplies the current public `/64`.

## Operation

```sh
v6pfxnatd -c /etc/v6pfxnatd/config.toml
v6pfxnatd --version
systemctl status v6pfxnatd.service
journalctl -u v6pfxnatd.service
cat /run/v6pfxnatd/status
```

The service runs as root with its capability set restricted to
`CAP_NET_ADMIN`. systemd creates and preserves `/run/v6pfxnatd` across service
restarts. After an actual successful nftables replacement, the daemon
best-effort atomically replaces `/run/v6pfxnatd/status` with mode `0444`:

```text
mapping fdff:a887:86e4:10::/64 2001:db8:1234:5610::/64
mapping fdff:a887:86e4:20::/64 2001:db8:1234:5620::/64
address gateway-c 2001:db8:1234:5620::c
```

The file contains only mapping and calculated-address records. `cat` and other
shell tools may consume it for operations. An unchanged nftables table, a
failed reconciliation, or a missing PD does not refresh or remove it. Snapshot
write failure is logged but does not turn a successful nftables replacement
into a failed reconciliation.

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
