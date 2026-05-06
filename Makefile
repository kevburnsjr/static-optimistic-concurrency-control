build:
	@cd example && go build -ldflags="-s -w" -o example .

run:
	@cd example && docker compose up --build --exit-code-from example

dev: build run

cloc:
	@cloc ./example --exclude-dir=_example,_dist,proto --exclude-ext=pb.go

.PHONY: all test clean
