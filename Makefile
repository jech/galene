all:
	CGO_ENABLED=0 go build -ldflags='-s -w'
