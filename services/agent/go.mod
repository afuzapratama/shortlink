module shortlink.local/agent

go 1.27.1

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/maxmind/mmdbwriter v1.2.0 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/oschwald/maxminddb-golang/v2 v2.7.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/ua-parser/uap-go v0.0.0-20260529044130-17c35e68e58c // indirect
	github.com/x-way/crawlerdetect v0.2.32 // indirect
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace shortlink.local/geoip => ../../packages/geoip

replace shortlink.local/policy => ../../packages/policy

replace shortlink.local/artifacts => ../../packages/artifacts

require (
	golang.org/x/term v0.47.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.60.1
	shortlink.local/artifacts v0.0.0
	shortlink.local/geoip v0.0.0
	shortlink.local/policy v0.0.0
	shortlink.local/risk v0.0.0
)

replace shortlink.local/risk => ../../packages/risk
