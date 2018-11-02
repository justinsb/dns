package main

import (
	"fmt"

	"flag"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/coredns/coredns/coremain"
	_ "github.com/coredns/coredns/plugin/bind"
	_ "github.com/coredns/coredns/plugin/cache"
	_ "github.com/coredns/coredns/plugin/errors"
	_ "github.com/coredns/coredns/plugin/forward"
	_ "github.com/coredns/coredns/plugin/health"
	_ "github.com/coredns/coredns/plugin/log"
	_ "github.com/coredns/coredns/plugin/loop"
	_ "github.com/coredns/coredns/plugin/metrics"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	_ "github.com/coredns/coredns/plugin/reload"
	"github.com/mholt/caddy"
	"k8s.io/kubernetes/pkg/util/dbus"
	utilexec "k8s.io/kubernetes/pkg/util/exec"
	utiliptables "k8s.io/kubernetes/pkg/util/iptables"
)

// configParams lists the configuration options that can be provided to dns-cache
type configParams struct {
	localIP   string        // ip address for the local cache agent to listen for dns requests
	localPort string        // port to listen for dns requests
	intfName  string        // Name of the interface to be created
	interval  time.Duration // specifies how often to run iptables rules check
	exitChan  chan bool     // Channel to terminate background goroutines
}

type iptablesRule struct {
	table utiliptables.Table
	chain utiliptables.Chain
	args  []string
}

type cacheApp struct {
	iptables      utiliptables.Interface
	iptablesRules []iptablesRule
	params        configParams
}

var cache = cacheApp{params: configParams{localPort: "53"}}

func (c *cacheApp) Init() {
	err := c.parseAndValidateFlags()
	if err != nil {
		clog.Errorf("Error parsing flags - %s", err)
		return
	}
	c.initIptables()
	c.teardown()
	err = c.setup()
	if err != nil {
		clog.Errorf("Failed to setup - %s, Exiting", err)
		return
	}
}

func init() {
	cache.Init()
	caddy.OnProcessExit = append(caddy.OnProcessExit, func() { cache.teardown() })
}

func (c *cacheApp) initIptables() {

	c.iptablesRules = []iptablesRule{
		// for packets destined to nodelocalcache
		{utiliptables.Table("raw"), utiliptables.ChainPrerouting, []string{"-p", "tcp", "-d", c.params.localIP,
			"--dport", c.params.localPort, "-j", "NOTRACK"}},
		{utiliptables.Table("raw"), utiliptables.ChainPrerouting, []string{"-p", "udp", "-d", c.params.localIP,
			"--dport", c.params.localPort, "-j", "NOTRACK"}},
		{utiliptables.TableFilter, utiliptables.ChainInput, []string{"-p", "tcp", "-d", c.params.localIP,
			"--dport", c.params.localPort, "-j", "ACCEPT"}},
		{utiliptables.TableFilter, utiliptables.ChainInput, []string{"-p", "udp", "-d", c.params.localIP,
			"--dport", c.params.localPort, "-j", "ACCEPT"}},
		// for replies from nodelocalcache
		{utiliptables.Table("raw"), utiliptables.ChainOutput, []string{"-p", "tcp", "-s", c.params.localIP,
			"--sport", c.params.localPort, "-j", "NOTRACK"}},
		{utiliptables.Table("raw"), utiliptables.ChainOutput, []string{"-p", "udp", "-s", c.params.localIP,
			"--sport", c.params.localPort, "-j", "NOTRACK"}},
		{utiliptables.TableFilter, utiliptables.ChainOutput, []string{"-p", "tcp", "-s", c.params.localIP,
			"--sport", c.params.localPort, "-j", "ACCEPT"}},
		{utiliptables.TableFilter, utiliptables.ChainOutput, []string{"-p", "udp", "-s", c.params.localIP,
			"--sport", c.params.localPort, "-j", "ACCEPT"}},
	}
	c.iptables = newIPTables()
}

func manageListenInterface(name string, ip net.IP, add bool) error {
	oper := "del"
	if add {
		oper = "add"
	}
	intfCmd := exec.Command("ip", "link", oper, name, "type", "dummy")
	out, err := intfCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to %s local interface for dns cache: Error - %s, Output - %s", oper, err, out)
	}
	if !add {
		// nothing more to do
		return nil
	}
	ipNet := net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
	ipCmd := exec.Command("ip", "addr", "add", ipNet.String(), "dev", name)
	out, err = ipCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to assign ip to local interface: Error - %s, Output - %s", err, out)
	}
	return nil
}

func newIPTables() utiliptables.Interface {
	execer := utilexec.New()
	dbus := dbus.New()
	return utiliptables.New(execer, dbus, utiliptables.ProtocolIpv4)
}

func (c *cacheApp) setup() error {
	return c.setupInternal(true)
}

func (c *cacheApp) setupInternal(create bool) error {
	err := manageListenInterface(c.params.intfName, net.ParseIP(c.params.localIP), create)
	// ingore error during teardown
	if create && err != nil {
		return err
	}
	for _, rule := range c.iptablesRules {
		if create {
			_, err = c.iptables.EnsureRule(utiliptables.Prepend, rule.table, rule.chain, rule.args...)
			if err != nil {
				return err
			}
		} else {
			exists := true
			for exists == true {
				c.iptables.DeleteRule(rule.table, rule.chain, rule.args...)
				exists, _ = c.iptables.EnsureRule(utiliptables.Prepend, rule.table, rule.chain, rule.args...)
			}
			// Delete the rule one last time since EnsureRule creates the rule if it doesn't exist
			c.iptables.DeleteRule(rule.table, rule.chain, rule.args...)
		}
	}
	return nil
}

func (c *cacheApp) parseAndValidateFlags() error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Runs coreDNS v1.2.5 as a nodelocal cache listening on the specified ip:port")
		flag.PrintDefaults()
	}

	flag.StringVar(&c.params.localIP, "localip", "", "ip address to bind dnscache to")
	flag.StringVar(&c.params.intfName, "intfname", "nodelocaldns", "name of the interface to be created")
	flag.DurationVar(&c.params.interval, "syncinterval", 60, "interval(in seconds) to check for iptables rules")
	flag.Parse()

	if net.ParseIP(c.params.localIP) == nil {
		return fmt.Errorf("Invalid localip specified - %s", c.params.localIP)
	}
	// lookup specified dns port
	f := flag.Lookup("dns.port")
	if f != nil {
		c.params.localPort = f.Value.String()
	}
	if _, err := strconv.Atoi(c.params.localPort); err != nil {
		return fmt.Errorf("Invalid port specified - %s", c.params.localPort)
	}
	return nil
}

func (c *cacheApp) teardown() {
	clog.Infof("Tearing down")
	if c.params.exitChan != nil {
		// exitChan is a buffered channel of size 1, so this will not block
		c.params.exitChan <- true
	}
	err := c.setupInternal(false)
	if err != nil {
		clog.Errorf("Ignoring error during teardown - %s\n", err)
	}
}

func (c *cacheApp) Run() {
	c.params.exitChan = make(chan bool, 1)
	go func(ch chan bool) {
		tick := time.NewTicker(c.params.interval * time.Second)
		var exists bool
		var err error
		for {
			select {
			case <-tick.C:
				for _, rule := range c.iptablesRules {
					exists, err = c.iptables.EnsureRule(utiliptables.Prepend, rule.table, rule.chain, rule.args...)
					if !exists {
						clog.Infof("Added back nonexistent rule - %v", rule)
					}
					if err != nil {
						clog.Errorf("Failed to check rule %v - %s", rule, err)
					}
				}
			case <-ch:
				clog.Errorf("Exiting iptables check goroutine")
				return
			}
		}
	}(c.params.exitChan)
}

func main() {

	cache.Run()
	coremain.Run()
	// Unlikely to reach here, if we did it is because coremain exited and the signal was not trapped.
	clog.Errorf("Untrapped signal, tearing down")
	cache.teardown()
}
