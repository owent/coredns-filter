package coredns_filter

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() {
	plugin.Register("filter", setup)
}

func setup(c *caddy.Controller) error {
	handle := NewFilterHandler()
	err := parse(c, &handle)
	if err != nil {
		return plugin.Error("filter", err)
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		handle.Next = next
		return &handle
	})

	log.Debug("Add filter plugin to dnsserver")

	return nil
}

func parse(c *caddy.Controller, handle *Filter) error {
	for c.Next() {
		args := c.RemainingArgs()
		if len(args) > 0 {
			err := handle.AddCommand(args[0], args[1:])
			if err != nil {
				return c.Errf("%v", err)
			}
		}

		// Refinements? In an extra block.
		for c.NextBlock() {
			err := handle.AddCommand(c.Val(), c.RemainingArgs())
			if err != nil {
				return c.Errf("%v", err)
			}
		}

		log.Debug("Successfully parsed configuration")
	}

	return nil
}
