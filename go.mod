module github.com/ponchione/shunter

go 1.25.5

require (
	github.com/coder/websocket v1.8.14
	github.com/golang-jwt/jwt/v5 v5.3.1
	go.uber.org/goleak v1.3.0
	lukechampine.com/blake3 v1.4.1
)

require github.com/klauspost/cpuid/v2 v2.0.9 // indirect

replace github.com/coder/websocket => github.com/ponchione/websocket v1.8.14-shunter.1
