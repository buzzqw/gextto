module github.com/buzzqw/gextto

go 1.26.8

require (
	github.com/cenkalti/log v1.0.0
	github.com/cenkalti/rain/v2 v2.4.2
	github.com/huin/goupnp v1.3.0
	github.com/jackpal/go-nat-pmp v1.1.0
	github.com/jlaffaye/ftp v0.2.4
	golang.org/x/crypto v0.57.0
	golang.org/x/net v0.59.0
	golang.org/x/sys v0.48.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.60.1
)

require (
	github.com/anacrolix/missinggo v1.3.0 // indirect
	github.com/anacrolix/missinggo/perf v1.0.0 // indirect
	github.com/anacrolix/missinggo/v2 v2.5.1 // indirect
	github.com/anacrolix/sync v0.4.0 // indirect
	github.com/anacrolix/utp v0.2.0 // indirect
	github.com/cenkalti/backoff/v7 v7.0.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/gofrs/uuid v4.4.0+incompatible // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/huandu/xstrings v1.3.1 // indirect
	github.com/jackpal/bencode-go v1.0.0 // indirect
	github.com/juju/ratelimit v1.0.2 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/nictuku/dht v0.0.0-20201226073453-fd1c1dd3d66a // indirect
	github.com/nictuku/nettools v0.0.0-20150117095333-8867a2107ad3 // indirect
	github.com/powerman/rpc-codec v1.2.2 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20201227073835-cf1acfcdf475 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/youtube/vitess v3.0.0-rc.3+incompatible // indirect
	github.com/zeebo/bencode v1.0.0 // indirect
	go.etcd.io/bbolt v1.5.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace github.com/cenkalti/rain/v2 => ./third_party/rain

replace github.com/nictuku/dht => ./third_party/dht
