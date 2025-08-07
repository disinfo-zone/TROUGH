.PHONY: build test run clean docker-up docker-down migrate lint

build:
	go build -o trough .

test:
	go test -v ./...

test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

run:
	go run .

clean:
	rm -f trough coverage.out coverage.html
	rm -rf uploads/*

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

docker-build:
	docker-compose up --build -d

migrate:
	docker-compose exec app psql $$DATABASE_URL -f /app/db/schema.sql

lint:
	gofmt -w .
	go vet ./...

dev: docker-up
	sleep 5
	make run

.DEFAULT_GOAL := build