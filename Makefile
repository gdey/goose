.PHONY: dist
dist:
	@mkdir -p ./bin
	@rm -f ./bin/*
	GOOS=darwin  GOARCH=arm64 go build -o ./bin/goose-darwin-arm64   ./cmd/goose
	GOOS=darwin  GOARCH=amd64 go build -o ./bin/goose-darwin-amd64   ./cmd/goose
	GOOS=linux   GOARCH=arm64 go build -o ./bin/goose-linux-arm64    ./cmd/goose
	GOOS=linux   GOARCH=amd64 go build -o ./bin/goose-linux-amd64    ./cmd/goose
	GOOS=linux   GOARCH=386   go build -o ./bin/goose-linux-x386     ./cmd/goose
	GOOS=windows GOARCH=amd64 go build -o ./bin/goose-windows64.exe  ./cmd/goose
	GOOS=windows GOARCH=386   go build -o ./bin/goose-windows386.exe ./cmd/goose

test-packages:
	go test -v $$(go list ./... | grep -v -e /tests -e /bin -e /cmd -e /examples)

test-e2e: test-e2e-postgres test-e2e-mysql

test-e2e-postgres:
	go test -v ./tests/e2e -dialect=postgres

test-e2e-mysql:
	go test -v ./tests/e2e -dialect=mysql

test-clickhouse:
	go test -timeout=10m -count=1 -race -v ./tests/clickhouse -test.short

docker-cleanup:
	docker stop -t=0 $$(docker ps --filter="label=goose_test" -aq)
