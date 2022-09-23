package coredns_filter

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/nonwriter"
	"github.com/miekg/dns"

	"go4.org/netipx"
)

var log = clog.NewWithPlugin("filter")

type FilterHandler interface {
	filter(r *dns.Msg) (bool, *dns.Msg)
}

type PreferIPv4 struct {
}

func (f *PreferIPv4) filter(r *dns.Msg) (bool, *dns.Msg) {
	hasIPv4 := false
	hasIPv6 := false
	var finalAnswer []dns.RR = []dns.RR{}

	for _, answer := range r.Answer {
		if hasIPv4 && hasIPv6 {
			break
		}

		switch answer.Header().Rrtype {
		case dns.TypeA:
			hasIPv4 = true
		case dns.TypeAAAA:
			hasIPv6 = true
		default:
		}
	}

	if hasIPv4 && hasIPv6 {
		for _, answer := range r.Answer {
			if answer.Header().Rrtype != dns.TypeAAAA {
				finalAnswer = append(finalAnswer, answer)
			} else {
				log.Debugf("Remove %v because prefer ipv4", answer.(*dns.AAAA).AAAA.String())
			}
		}
		r.Answer = finalAnswer
		return true, r
	} else {
		return false, r
	}
}

type PreferIPv6 struct {
}

func (f *PreferIPv6) filter(r *dns.Msg) (bool, *dns.Msg) {
	hasIPv4 := false
	hasIPv6 := false
	var finalAnswer []dns.RR = []dns.RR{}

	for _, answer := range r.Answer {
		if hasIPv4 && hasIPv6 {
			break
		}

		switch answer.Header().Rrtype {
		case dns.TypeA:
			hasIPv4 = true
		case dns.TypeAAAA:
			hasIPv6 = true
		default:
		}
	}

	if hasIPv4 && hasIPv6 {
		for _, answer := range r.Answer {
			if answer.Header().Rrtype != dns.TypeA {
				finalAnswer = append(finalAnswer, answer)
			} else {
				log.Debugf("Remove %v because prefer ipv6", answer.(*dns.A).A.String())
			}
		}
		r.Answer = finalAnswer
		return true, r
	} else {
		return false, r
	}
}

type BogusNxDomain struct {
	ipv4Builder *netipx.IPSetBuilder
	ipv6Builder *netipx.IPSetBuilder
	ipv4Set     *netipx.IPSet
	ipv6Set     *netipx.IPSet
}

func (f *BogusNxDomain) filter(r *dns.Msg) (bool, *dns.Msg) {
	if f.ipv4Set == nil && f.ipv6Set == nil {
		if f.ipv4Builder != nil {
			f.ipv4Set, _ = f.ipv4Builder.IPSet()
		}
		if f.ipv6Builder != nil {
			f.ipv6Set, _ = f.ipv6Builder.IPSet()
		}
	}
	if f.ipv4Set == nil && f.ipv6Set == nil {
		return false, r
	}

	hasBogusNxDomain := false

	var finalAnswer []dns.RR = []dns.RR{}

	for _, answer := range r.Answer {
		switch answer.Header().Rrtype {
		case dns.TypeA:
			addr, ok := netipx.FromStdIP(answer.(*dns.A).A)
			if f.ipv4Set != nil && ok && f.ipv4Set.Contains(addr) {
				hasBogusNxDomain = true
				log.Debugf("Remove %v because it's under bogus-nxdomain", addr.String())
			} else {
				finalAnswer = append(finalAnswer, answer)
			}
		case dns.TypeAAAA:
			addr, ok := netipx.FromStdIP(answer.(*dns.AAAA).AAAA)
			if f.ipv6Set != nil && ok && f.ipv6Set.Contains(addr) {
				hasBogusNxDomain = true
				log.Debugf("Remove %v because it's under bogus-nxdomain", addr.String())
			} else {
				finalAnswer = append(finalAnswer, answer)
			}
		default:
			finalAnswer = append(finalAnswer, answer)
		}
	}

	if hasBogusNxDomain {
		r.Answer = finalAnswer
	}
	// bugos NXDOMAIN
	if hasBogusNxDomain && len(r.Answer) == 0 {
		r.Rcode = dns.RcodeNameError
		log.Debugf("Set rcode to NXDOMAIN because all answers are under bogus-nxdomain")
	}

	return hasBogusNxDomain, r
}

// Filter implements the plugin.Handler interface.
type Filter struct {
	Next          plugin.Handler
	Prefer        FilterHandler
	BugosNxDomain FilterHandler
}

func NewFilterHandler() Filter {
	return Filter{
		Next:          nil,
		Prefer:        nil,
		BugosNxDomain: nil,
	}
}

func (m *Filter) Name() string { return "filter" }

func (m *Filter) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	// Do nothing
	if m.Prefer == nil && m.BugosNxDomain == nil {
		return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	}

	recordCount.WithLabelValues(metrics.WithServer(ctx)).Inc()
	defer exportRecordDuration(ctx, time.Now())

	nw := nonwriter.New(w)
	rcode, err := plugin.NextOrFailure(m.Name(), m.Next, ctx, nw, r)
	if err != nil {
		return rcode, err
	}

	r = nw.Msg
	if r == nil {
		return 1, fmt.Errorf("no answer received")
	}

	if m.Prefer != nil {
		_, r = m.Prefer.filter(r)
	}

	if m.BugosNxDomain != nil {
		_, r = m.BugosNxDomain.filter(r)
	}

	err = w.WriteMsg(r)
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func (m *Filter) AddCommand(cmd string, options []string) error {
	switch strings.ToLower(cmd) {
	case "prefer":
		{
			err := m.AddPreferHandler(options)
			if err != nil {
				return err
			}
		}
	case "bogus-nxdomain":
		{
			err := m.AddBogusNxDomainHandler(options)
			if err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid command %v with options %v", cmd, options)
	}
	return nil
}

func (m *Filter) AddPreferHandler(options []string) error {
	if len(options) != 1 {
		return fmt.Errorf("command prefer must be set [none/ipv4/ipv6]")
	} else if strings.ToLower(options[0]) == "none" {
		return nil
	} else if strings.ToLower(options[0]) == "ipv4" {
		m.Prefer = &PreferIPv4{}
		return nil
	} else if strings.ToLower(options[0]) == "ipv6" {
		m.Prefer = &PreferIPv6{}
		return nil
	} else {
		return fmt.Errorf("command prefer must be set [none/ipv4/ipv6]")
	}
}

func mutableIPv4Builder(handle *BogusNxDomain) *netipx.IPSetBuilder {
	if handle.ipv4Builder == nil {
		handle.ipv4Builder = new(netipx.IPSetBuilder)
	}

	return handle.ipv4Builder
}

func mutableIPv6Builder(handle *BogusNxDomain) *netipx.IPSetBuilder {
	if handle.ipv6Builder == nil {
		handle.ipv6Builder = new(netipx.IPSetBuilder)
	}

	return handle.ipv6Builder
}

func (m *Filter) AddBogusNxDomainHandler(options []string) error {
	if len(options) == 0 {
		return nil
	}

	handle := m.BugosNxDomain.(*BogusNxDomain)

	if handle == nil {
		handle = &BogusNxDomain{
			ipv4Builder: nil,
			ipv6Builder: nil,
			ipv4Set:     nil,
			ipv6Set:     nil,
		}
		m.BugosNxDomain = handle
	}

	for _, opt := range options {
		tryParseIp, err := netip.ParseAddr(opt)
		if err == nil {
			if tryParseIp.Is4() {
				mutableIPv4Builder(handle).Add(tryParseIp)
			} else if tryParseIp.Is6() {
				mutableIPv6Builder(handle).Add(tryParseIp)
			} else {
				return fmt.Errorf("command bogus-nxdomain can not parse ip address %v", opt)
			}

			continue
		}

		tryParseIpPrefix, err := netip.ParsePrefix(opt)
		if err == nil {
			if tryParseIpPrefix.Addr().Is4() {
				mutableIPv4Builder(handle).AddPrefix(tryParseIpPrefix)
			} else if tryParseIpPrefix.Addr().Is6() {
				mutableIPv6Builder(handle).AddPrefix(tryParseIpPrefix)
			} else {
				return fmt.Errorf("command bogus-nxdomain can not parse ip address %v", opt)
			}
			continue
		}

		return fmt.Errorf("command bogus-nxdomain can not parse ip address %v", opt)
	}

	handle.ipv4Set = nil
	handle.ipv6Set = nil
	return nil
}

func exportRecordDuration(ctx context.Context, start time.Time) {
	recordDuration.WithLabelValues(metrics.WithServer(ctx)).
		Observe(float64(time.Since(start).Microseconds()))
}
