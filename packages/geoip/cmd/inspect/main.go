// inspect validates a local MMDB set and performs an explicit diagnostic lookup.
// It does not publish files or relax release freshness requirements.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	maxminddb "github.com/oschwald/maxminddb-golang/v2"
	"shortlink.local/geoip"
)

func main() {
	directory := flag.String("dir", "", "local directory containing three GeoLite MMDB files")
	rawIP := flag.String("ip", "8.8.8.8", "explicit diagnostic IP (no network request)")
	flag.Parse()
	ip, err := netip.ParseAddr(*rawIP)
	if err != nil || *directory == "" {
		fmt.Fprintln(os.Stderr, "provide --dir and a valid --ip")
		os.Exit(2)
	}
	results := []map[string]any{}
	for _, edition := range geoip.Editions {
		reader, err := maxminddb.Open(filepath.Join(*directory, edition+".mmdb"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot open", edition)
			os.Exit(1)
		}
		if reader.Metadata.DatabaseType != edition || reader.Verify() != nil {
			reader.Close()
			fmt.Fprintln(os.Stderr, "invalid database", edition)
			os.Exit(1)
		}
		var record any
		if err = reader.Lookup(ip).Decode(&record); err != nil {
			reader.Close()
			fmt.Fprintln(os.Stderr, "lookup failed", edition)
			os.Exit(1)
		}
		results = append(results, map[string]any{"edition": edition, "buildTime": reader.Metadata.BuildTime(), "olderThan29Days": time.Since(reader.Metadata.BuildTime()) > 29*24*time.Hour, "ip": ip.String(), "record": record})
		reader.Close()
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		os.Exit(1)
	}
}
