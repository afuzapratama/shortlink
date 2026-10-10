// Package testutil generates tiny synthetic MMDB fixtures, never actual GeoLite data.
package testutil

import (
	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"net"
	"os"
	"path/filepath"
	"shortlink.local/geoip"
)

func Staging(root string, epoch int64, country string) error {
	if err := os.MkdirAll(filepath.Join(root, "staging"), 0700); err != nil {
		return err
	}
	for _, edition := range geoip.Editions {
		tree, err := mmdbwriter.New(mmdbwriter.Options{DatabaseType: edition, BuildEpoch: epoch, IPVersion: 6, RecordSize: 24, IncludeReservedNetworks: true, Description: map[string]string{"en": "SYNTHETIC TEST FIXTURE"}, Languages: []string{"en"}})
		if err != nil {
			return err
		}
		data := mmdbtype.Map{"country": mmdbtype.Map{"iso_code": mmdbtype.String(country)}, "city": mmdbtype.Map{"names": mmdbtype.Map{"en": mmdbtype.String("Test City")}}}
		if edition == "GeoLite2-ASN" {
			data = mmdbtype.Map{"autonomous_system_number": mmdbtype.Uint32(64512), "autonomous_system_organization": mmdbtype.String("TEST NETWORK")}
		}
		for _, cidr := range []string{"1.1.1.0/24", "2001:4860::/32"} {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				return err
			}
			if err = tree.Insert(network, data); err != nil {
				return err
			}
		}
		f, err := os.OpenFile(filepath.Join(root, "staging", edition+".mmdb"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = tree.WriteTo(f)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
