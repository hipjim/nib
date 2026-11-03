// Benchcheck reads a benchstat comparison in CSV form and fails when a
// benchmark got noticeably slower.
//
//	benchstat -format csv old.txt new.txt | go run ./scripts/benchcheck -max 10
//
// Only the sec/op tables count. A row counts as slower when benchstat
// found the change significant (it prints ~ otherwise) and the increase
// is more than -max percent. Benchmarks present in only one input are
// ignored, so adding or removing one does not fail the check.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	maxPct := flag.Float64("max", 10, "largest allowed slowdown, in percent")
	flag.Parse()
	in := io.Reader(os.Stdin)
	if flag.NArg() == 1 {
		f, err := os.Open(flag.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer f.Close()
		in = f
	}
	slower, err := check(in, *maxPct)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(slower) > 0 {
		fmt.Printf("\n%d benchmark(s) more than %g%% slower:\n", len(slower), *maxPct)
		for _, s := range slower {
			fmt.Println("  " + s)
		}
		os.Exit(1)
	}
	fmt.Printf("no benchmark more than %g%% slower\n", *maxPct)
}

// check returns the sec/op rows slower by more than maxPct, as "name: +N%".
func check(in io.Reader, maxPct float64) ([]string, error) {
	r := csv.NewReader(in)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var slower []string
	unit := ""
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return slower, nil
		}
		if err != nil {
			return nil, err
		}
		if len(rec) < 2 {
			continue // benchstat's goos/goarch preamble
		}
		if rec[0] == "" {
			unit = rec[1] // a table header: ",sec/op,CI,sec/op,CI,vs base,P"
			continue
		}
		if unit != "sec/op" || rec[0] == "geomean" || len(rec) < 7 {
			continue
		}
		name, old, new, delta := rec[0], rec[1], rec[3], rec[5]
		if old == "" || new == "" {
			continue // only in one input
		}
		fmt.Printf("%-40s %s\n", name, delta)
		if !strings.HasPrefix(delta, "+") {
			continue // faster, or no significant change (~)
		}
		pct, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimPrefix(delta, "+"), "%"), 64)
		if err != nil {
			return nil, fmt.Errorf("%s: cannot read change %q", name, delta)
		}
		if pct > maxPct {
			slower = append(slower, name+": "+delta)
		}
	}
}
