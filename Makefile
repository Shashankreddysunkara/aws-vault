export GO111MODULE=on
VERSION=$(shell git describe --tags --candidates=1 --dirty)
FLAGS=-X main.Version=$(VERSION) -s -w
CERT="Developer ID Application: 99designs Inc (NRM9HVJ62Z)"
SRC=$(shell find . -name '*.go')

.PHONY: all clean release

all: aws-vault-linux-amd64 aws-vault-darwin-amd64 aws-vault-windows-386.exe aws-vault-freebsd-amd64

clean:
	rm -f aws-vault-linux-amd64 aws-vault-darwin-amd64 aws-vault-darwin-amd64.dmg aws-vault-windows-386.exe aws-vault-freebsd-amd64

release: all aws-vault-darwin-amd64.dmg
	@echo "\nTo update homebrew-cask run\n\n    cask-repair -v $(shell echo $(VERSION) | sed 's/v\(.*\)/\1/') aws-vault\n"

aws-vault-linux-amd64: $(SRC)
	GOOS=linux GOARCH=amd64 go build -o $@ -ldflags="$(FLAGS)" .

aws-vault-darwin-amd64: $(SRC)
	GOOS=darwin GOARCH=amd64 go build -o $@ -ldflags="$(FLAGS) -extldflags '-sectcreate __TEXT __info_plist Info.plist'" .

aws-vault-windows-386.exe: $(SRC)
	GOOS=windows GOARCH=386 go build -o $@ -ldflags="$(FLAGS)" .

aws-vault-freebsd-amd64: $(SRC)
	GOOS=freebsd GOARCH=amd64 go build -o $@ -ldflags="$(FLAGS)" .

aws-vault-darwin-amd64.dmg: aws-vault-darwin-amd64
	./bin/create-dmg aws-vault-darwin-amd64 $@
