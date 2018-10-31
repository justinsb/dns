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
	"k8s.io/kubernetes/pkg/util/iptables"
	utilexec "k8s.io/kubernetes/pkg/util/exec"
)

type configParams struct {
	localIP        string
	localPort      string
	intfName       string
	interval       time.Duration
	exitChan       chan bool
}

type ruleStruct struct {
	table iptables.Table
	chain iptables.Chain
	args  []string
}

var iptInterface iptables.Interface
var params = configParams{exitChan: make(chan bool, 1), localPort : "53"}

var iptablesRules []ruleStruct

func init() {
	err := parseAndValidateFlags()
	if err != nil {
		clog.Errorf("Error parsing flags - %s", err)
		return
	}
	initIptables()

	teardown(nil)
	err = setup(true)
	if err != nil {
		clog.Errorf("Failed to setup - %s, Exiting", err)
		return
	}
	caddy.OnProcessExit = append(caddy.OnProcessExit, func() { teardown(params.exitChan) })
}

func initIptables() {

	iptablesRules = []ruleStruct{
		// for packets destined to nodelocalcache
		{iptables.Table("raw"), iptables.ChainPrerouting, []string{"-p", "tcp", "-d", params.localIP,
			"--dport", params.localPort, "-j", "NOTRACK"}},
		{iptables.Table("raw"), iptables.ChainPrerouting, []string{"-p", "udp", "-d", params.localIP,
			"--dport", params.localPort, "-j", "NOTRACK"}},
		{iptables.TableFilter, iptables.ChainInput, []string{"-p", "tcp", "-d", params.localIP, "--dport",
			params.localPort, "-j", "ACCEPT"}},
		{iptables.TableFilter, iptables.ChainInput, []string{"-p", "udp", "-d", params.localIP, "--dport",
			params.localPort, "-j", "ACCEPT"}},
		// for replies from nodelocalcache
		{iptables.Table("raw"), iptables.ChainOutput, []string{"-p", "tcp", "-s", params.localIP,
			"--sport", params.localPort, "-j", "NOTRACK"}},
		{iptables.Table("raw"), iptables.ChainOutput, []string{"-p", "udp", "-s", params.localIP,
			"--sport", params.localPort, "-j", "NOTRACK"}},
		{iptables.TableFilter, iptables.ChainOutput, []string{"-p", "tcp", "-s", params.localIP, "--sport",
			params.localPort, "-j", "ACCEPT"}},
		{iptables.TableFilter, iptables.ChainOutput, []string{"-p", "udp", "-s", params.localIP, "--sport",
			params.localPort, "-j", "ACCEPT"}},
	}
	iptInterface = newIPTablesClient()
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

func newIPTablesClient() iptables.Interface {
	execer := utilexec.New()
	dbus := dbus.New()
	return iptables.New(execer, dbus, iptables.ProtocolIpv4)
}

func setup(create bool) error {
	err := manageListenInterface(params.intfName, net.ParseIP(params.localIP), create)
	// ingore error during teardown
	if create && err != nil {
		return err
	}
	for _, rule := range iptablesRules {
		if create {
			_, err = iptInterface.EnsureRule(iptables.Prepend, rule.table, rule.chain, rule.args...)
			if err != nil {
				return err
			}
		} else {
			exists := true
			for exists == true {
				iptInterface.DeleteRule(rule.table, rule.chain, rule.args...)
				exists, _ = iptInterface.EnsureRule(iptables.Prepend, rule.table, rule.chain, rule.args...)
			}
			// Delete the rule one last time since EnsureRule creates the rule if it doesn't exist
			iptInterface.DeleteRule(rule.table, rule.chain, rule.args...)
		}
	}
	return nil
}

func parseAndValidateFlags() error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Runs coreDNS v1.2.5 as a nodelocal cache listening on the specified ip:port")
		flag.PrintDefaults()
	}

	flag.StringVar(&params.localIP, "localip", "", "ip address to bind dnscache to")
	flag.StringVar(&params.intfName, "intfname", "nodelocaldns", "name of the interface to be created")
	flag.DurationVar(&params.interval, "syncinterval", 60, "interval(in seconds) to check for iptables rules")
	flag.Parse()

	if net.ParseIP(params.localIP) == nil {
		return fmt.Errorf("Invalid localip specified - %s", params.localIP)
	}
	// lookup specified dns port
	f := flag.Lookup("dns.port")
	if f != nil {
		params.localPort = f.Value.String()
	}
	if _, err := strconv.Atoi(params.localPort); err != nil {
		return fmt.Errorf("Invalid port specified - %s", params.localPort)
	}
	return nil
}

func teardown(ch chan bool) {
	clog.Infof("Tearing down")
	if ch != nil {
		// ch is a buffered channel of size 1, so this will not block
		ch <- true
	}
	err := setup(false)
	if err != nil {
		clog.Errorf("Ignoring error during teardown - %s\n", err)
	}
}

func main() {

	go func(ch chan bool) {
		tick := time.NewTicker(params.interval * time.Second)
		var exists bool
		var err error
		for {
			select {
			case <-tick.C:
				for _, rule := range iptablesRules {
					exists, err = iptInterface.EnsureRule(iptables.Prepend, rule.table, rule.chain, rule.args...)
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
	}(params.exitChan)

	coremain.Run()
	// Unlikely to reach here, if we did it is because coremain exited and the signal was not trapped.
	teardown(params.exitChan)
}
