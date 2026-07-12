module github.com/donomii/powerlock/benchmarks/diagnostics

go 1.23.0

require (
	github.com/christophcemper/rwmutexplus v0.0.5
	github.com/donomii/powerlock v0.0.0
	github.com/linkdata/deadlock v0.5.5
	github.com/sasha-s/go-deadlock v0.3.9
)

require github.com/petermattis/goid v0.0.0-20250813065127-a731cc31b4fe // indirect

replace github.com/donomii/powerlock => ../..
