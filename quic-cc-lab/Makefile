.PHONY: build test bench proxy vet fmt clean score

build:
	go build -o bin/bench        ./cmd/bench
	go build -o bin/proxy-server ./cmd/proxy-server
	go build -o bin/proxy-client ./cmd/proxy-client

vet:
	go vet ./...

fmt:
	gofmt -l -w internal cmd

# loopback sanity check (no netem): should print non-zero goodput
bench: build
	@./bin/bench -mode server -listen 127.0.0.1:4433 -cc $(CC) & \
	SRV=$$!; sleep 0.5; \
	./bin/bench -mode client -server 127.0.0.1:4433 -cc $(CC) -duration 5s; \
	kill $$SRV 2>/dev/null || true
CC ?= student

clean:
	rm -rf bin testbed/results
