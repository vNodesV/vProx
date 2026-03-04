package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	pushcfg "github.com/vNodesV/vProx/internal/push/config"
	"github.com/vNodesV/vProx/internal/push/status"
	"github.com/vNodesV/vProx/internal/chain/upgrade"
)

// runChainCmd handles: vprox chain <sub> [flags]
//
//	chain status [--chain name]          — poll all (or one) chain(s) across registered VMs
//	chain upgrade --prop <id> <rest-url> — fetch gov proposal details (upgrade info)
func runChainCmd(home string, args []string) {
	usage := func() {
		fmt.Fprintln(os.Stderr, "Usage: vprox chain <sub> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  status [--chain name]               poll node status for all registered chains")
		fmt.Fprintln(os.Stderr, "  upgrade --prop <id> <rest-url>      show governance upgrade proposal details")
	}

	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	switch args[0] {
	case "status":
		runChainStatus(home, args[1:])
	case "upgrade":
		runChainUpgrade(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "vprox chain: unknown subcommand %q\n\n", args[0])
		usage()
		os.Exit(1)
	}
}

// ── chain status ─────────────────────────────────────────────────────────────

func runChainStatus(home string, args []string) {
	fs := flag.NewFlagSet("chain status", flag.ExitOnError)
	chainFilter := fs.String("chain", "", "filter to a specific chain name")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfgPath := filepath.Join(home, "config", "vms.toml")
	cfg, err := pushcfg.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chain status: load vms.toml: %v\n", err)
		os.Exit(1)
	}

	// Collect unique (chain, rpc, rest) tuples across all VMs.
	type entry struct {
		chain   string
		rpcURL  string
		restURL string
	}
	seen := map[string]bool{}
	var entries []entry
	for _, vm := range cfg.VMs {
		if *chainFilter != "" && vm.Name != *chainFilter {
			continue
		}
		key := vm.Name + "|" + vm.RPC()
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, entry{vm.Name, vm.RPC(), vm.REST()})
	}

	if len(entries) == 0 {
		if *chainFilter != "" {
			fmt.Fprintf(os.Stderr, "chain status: no VMs registered for chain %q\n", *chainFilter)
		} else {
			fmt.Fprintln(os.Stderr, "chain status: no chains registered in vms.toml")
		}
		os.Exit(1)
	}

	ctx := context.Background()
	results := make([]*status.ChainStatus, len(entries))
	ch := make(chan struct{ idx int; s *status.ChainStatus }, len(entries))
	for i, e := range entries {
		go func(idx int, e entry) {
			s := status.Poll(ctx, e.chain, e.rpcURL, e.restURL)
			ch <- struct{ idx int; s *status.ChainStatus }{idx, s}
		}(i, e)
	}
	for range entries {
		r := <-ch
		results[r.idx] = r.s
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CHAIN\tHEIGHT\tSTATUS\tUPGRADE\tPROPOSALS\tUPDATED")
	for _, s := range results {
		upg := "-"
		if s.UpgradePending {
			upg = fmt.Sprintf("%s@%d", s.UpgradeName, s.UpgradeHeight)
		}
		props := fmt.Sprintf("%d", s.ActiveProposals)
		errStr := ""
		if s.Error != "" {
			errStr = "  ERR: " + s.Error
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s%s\n",
			s.Chain, s.Height, s.NodeStatus, upg, props,
			s.UpdatedAt.Format(time.RFC3339), errStr)
	}
	tw.Flush()
}

// ── chain upgrade ────────────────────────────────────────────────────────────

func runChainUpgrade(args []string) {
	fs := flag.NewFlagSet("chain upgrade", flag.ExitOnError)
	propID := fs.Uint64("prop", 0, "governance proposal ID")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	rest := fs.Arg(0)
	if *propID == 0 || rest == "" {
		fmt.Fprintln(os.Stderr, "chain upgrade: --prop <id> <rest-url> required")
		fmt.Fprintln(os.Stderr, "  Example: vprox chain upgrade --prop 42 https://rest.akash.network")
		os.Exit(1)
	}

	ctx := context.Background()
	p, err := upgrade.FetchProposal(ctx, rest, *propID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chain upgrade: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Proposal   : #%d — %s\n", p.ID, p.Title)
	fmt.Printf("Status     : %s\n", p.Status)
	if !p.VotingEnd.IsZero() {
		fmt.Printf("Voting ends: %s\n", p.VotingEnd.Format(time.RFC3339))
	}
	if p.UpgradeName != "" {
		fmt.Printf("Upgrade    : %s\n", p.UpgradeName)
		fmt.Printf("Height     : %d\n", p.Height)
	}
	if p.BinaryURL != "" {
		fmt.Printf("Binary URL : %s\n", p.BinaryURL)
	}
}
