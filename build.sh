#!/bin/bash

mkdir bin >2/dev/nul
GOOS=linux GOARCH=arm go build -o bin/systemd-web-arm systemd-web.go
GOOS=linux GOARCH=arm64 go build -o bin/systemd-web-arm64 systemd-web.go
