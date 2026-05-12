module github.com/ponchione/shunter

go 1.25.5

toolchain go1.26.3

require (
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/go-cmp v0.6.0
	github.com/ponchione/websocket v1.8.15-shunter.1
	github.com/prometheus/client_golang v1.21.1
	github.com/prometheus/client_model v0.6.2
	go.uber.org/goleak v1.3.0
	lukechampine.com/blake3 v1.4.1
	pgregory.net/rapid v1.2.0
)

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/tools v0.40.1-0.20260108161641-ca281cf95054 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
	honnef.co/go/tools v0.7.0 // indirect
)

tool honnef.co/go/tools/cmd/staticcheck
