//go:build linux

package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func WatchIPv6Routes(ctx context.Context, cfg NormalizedConfig, ready chan<- struct{}, triggers chan<- struct{}) error {
	updates := make(chan netlink.RouteUpdate)
	subscriptionErrors := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)

	err := netlink.RouteSubscribeWithOptions(updates, done, netlink.RouteSubscribeOptions{
		ErrorCallback: func(err error) {
			select {
			case subscriptionErrors <- err:
			default:
			}
		},
	})
	if err != nil {
		return fmt.Errorf("subscribe IPv6 routes: %w", err)
	}
	close(ready)

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-subscriptionErrors:
			return fmt.Errorf("route subscription: %w", err)
		case update, ok := <-updates:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				select {
				case err := <-subscriptionErrors:
					return fmt.Errorf("route subscription: %w", err)
				default:
				}
				return errors.New("route update channel closed")
			}
			if routeUpdateRelevant(update, cfg) {
				Trigger(triggers)
			}
		}
	}
}

func routeUpdateRelevant(update netlink.RouteUpdate, cfg NormalizedConfig) bool {
	if update.Type != unix.RTM_NEWROUTE && update.Type != unix.RTM_DELROUTE {
		return false
	}
	if update.Family != unix.AF_INET6 {
		return false
	}
	if update.Table == cfg.PolicyRouteTable && isDefaultRouteDestination(update.Dst) {
		return true
	}
	return update.Table == cfg.PDRouteTable &&
		int(update.Protocol) == cfg.PDRouteProtocol &&
		update.Route.Type == cfg.PDRouteType &&
		update.Dst != nil &&
		prefixLength(update.Dst) == cfg.PDPrefixLength
}

func ReadNetworkSnapshot(cfg NormalizedConfig) (NetworkSnapshot, error) {
	link, err := netlink.LinkByName(cfg.WANInterface)
	if err != nil {
		return NetworkSnapshot{}, fmt.Errorf("read WAN interface %q: %w", cfg.WANInterface, err)
	}
	wanIfIndex := link.Attrs().Index

	defaultRoutes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: cfg.PolicyRouteTable}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return NetworkSnapshot{}, fmt.Errorf("list policy routes: %w", err)
	}
	foundDefault := false
	for _, route := range defaultRoutes {
		if route.LinkIndex == wanIfIndex && isDefaultRouteDestination(route.Dst) {
			foundDefault = true
			break
		}
	}
	if !foundDefault {
		return NetworkSnapshot{}, fmt.Errorf("IPv6 default route for %s not found in table %d", cfg.WANInterface, cfg.PolicyRouteTable)
	}

	pdRoutes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: cfg.PDRouteTable}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return NetworkSnapshot{}, fmt.Errorf("list delegated-prefix routes: %w", err)
	}
	candidates := make([]netip.Prefix, 0)
	for _, route := range pdRoutes {
		if int(route.Protocol) != cfg.PDRouteProtocol || route.Type != cfg.PDRouteType || route.Dst == nil || prefixLength(route.Dst) != cfg.PDPrefixLength {
			continue
		}
		candidates = append(candidates, prefixFromIPNet(route.Dst))
	}
	return NetworkSnapshot{PDCandidates: candidates}, nil
}

func isDefaultRouteDestination(dst *net.IPNet) bool {
	if dst == nil {
		return true
	}
	ones, _ := dst.Mask.Size()
	return ones == 0 && dst.IP.IsUnspecified()
}

func prefixLength(network *net.IPNet) int {
	ones, _ := network.Mask.Size()
	return ones
}

func prefixFromIPNet(network *net.IPNet) netip.Prefix {
	ones, _ := network.Mask.Size()
	address, _ := netip.AddrFromSlice(network.IP)
	return netip.PrefixFrom(address, ones).Masked()
}
