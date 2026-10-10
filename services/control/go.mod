module shortlink.local/control

go 1.27.1

require (
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/maxmind/mmdbwriter v1.2.0 // indirect
	github.com/oschwald/maxminddb-golang/v2 v2.7.0 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	github.com/ua-parser/uap-go v0.0.0-20260529044130-17c35e68e58c // indirect
	github.com/x-way/crawlerdetect v0.2.32 // indirect
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace shortlink.local/geoip => ../../packages/geoip

replace shortlink.local/policy => ../../packages/policy

replace shortlink.local/artifacts => ../../packages/artifacts

require (
	github.com/jackc/pgx/v5 v5.11.0
	golang.org/x/crypto v0.58.0
	golang.org/x/text v0.43.0
	shortlink.local/artifacts v0.0.0
	shortlink.local/geoip v0.0.0
	shortlink.local/policy v0.0.0
	shortlink.local/risk v0.0.0
)

replace shortlink.local/risk => ../../packages/risk
