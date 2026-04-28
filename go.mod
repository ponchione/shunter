module github.com/ponchione/shunter

go 1.25.5

require (
	github.com/coder/websocket v1.8.14
	github.com/golang-jwt/jwt/v5 v5.3.1
	go.uber.org/goleak v1.3.0
	lukechampine.com/blake3 v1.4.1
)

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/tools v0.40.1-0.20260108161641-ca281cf95054 // indirect
	honnef.co/go/tools v0.7.0 // indirect
)

replace github.com/coder/websocket => github.com/ponchione/websocket v1.8.14-shunter.1

tool honnef.co/go/tools/cmd/staticcheck
